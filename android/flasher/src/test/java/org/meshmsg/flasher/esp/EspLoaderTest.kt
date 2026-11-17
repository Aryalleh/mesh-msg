package org.meshmsg.flasher.esp

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.meshmsg.boardlink.SerialLink
import org.meshmsg.flasher.BoardImage
import org.meshmsg.flasher.BoardImages
import org.meshmsg.flasher.Flasher
import org.meshmsg.flasher.Mode
import org.meshmsg.flasher.Phase
import kotlin.random.Random

class EspLoaderTest {
    private val noSleep: (Long) -> Unit = {}

    @Test
    fun slipRoundTripWithEscapesAndBootText() {
        val pkt = byteArrayOf(1, 0xC0.toByte(), 2, 0xDB.toByte(), 0xDC.toByte(), 3)
        val stream = "ESP-ROM:esp32s3\r\n".toByteArray() + Slip.encode(pkt) + Slip.encode(byteArrayOf(9))
        val d = SlipDecoder()
        // Fed in odd-sized pieces, as USB reads arrive.
        stream.toList().chunked(5).forEach { c -> d.feed(c.toByteArray(), c.size) }
        val frames = generateSequence { d.poll() }.toList()
        assertEquals(3, frames.size) // boot text comes out as a junk frame
        assertArrayEquals(pkt, frames[1])
        assertArrayEquals(byteArrayOf(9), frames[2])
    }

    private fun images(rnd: Random): Map<String, ByteArray> {
        // Random bytes (incompressible, full of SLIP specials) and long 0xFF runs, like a file system image.
        val fs = ByteArray(1_200_000) { 0xFF.toByte() }
        rnd.nextBytes(fs, 0, 200_000)
        return mapOf(
            "boot.bin" to rnd.nextBytes(0x8C00),
            "boot_app0.bin" to ByteArray(0x2000) { 0xFF.toByte() }.also { it[0] = 1 },
            "firmware.bin" to rnd.nextBytes(300_001),
            "littlefs.bin" to fs,
        )
    }

    private val manifest = """
        # offset file part
        0x0 boot.bin boot
        0xe000 boot_app0.bin boot
        0x10000 firmware.bin app
        0x810000 littlefs.bin fs
    """.trimIndent()

    private fun flashWith(rom: FakeRom, mode: Mode, files: Map<String, ByteArray>): List<Phase> {
        val phases = mutableListOf<Phase>()
        var last = 0L
        val list = BoardImages.parse(manifest) { files.getValue(it) }
        Flasher(log = {}, sleep = noSleep).run({ rom }, list, mode, { phases += it }) { done, total ->
            assertTrue(done in last..total)
            last = done
        }
        return phases
    }

    @Test
    fun writesEverythingAndVerifiesOverNativeUsb() {
        val files = images(Random(1))
        val rom = FakeRom(nativeUsb = true)
        val phases = flashWith(rom, Mode.Everything, files)
        for ((name, off) in listOf("boot.bin" to 0, "boot_app0.bin" to 0xe000, "firmware.bin" to 0x10000, "littlefs.bin" to 0x810000)) {
            val img = files.getValue(name)
            assertArrayEquals(name, img, rom.flash.copyOfRange(off, off + img.size))
        }
        assertEquals(16 * 1024 * 1024, rom.flashSizeSet)
        assertEquals(115200, rom.currentBaud) // no baud change on the chip's own USB
        assertEquals(Phase.Connecting, phases.first())
        assertEquals(Phase.Restarting, phases.last())
        assertEquals(4, phases.count { it is Phase.Writing })
        assertEquals(false to false, rom.lines.last()) // released after the hard reset
        // Watchdogs off before writing (USB-Serial/JTAG), force-download cleared before the reset.
        assertTrue(EspLoader.RTC_CNTL_WDTCONFIG0_REG to 0 in rom.regWrites)
        assertEquals(EspLoader.RTC_CNTL_OPTION1_REG, rom.regWrites.last().first)
    }

    @Test
    fun firmwareOnlyKeepsTheFileSystem() {
        val files = images(Random(2))
        val rom = FakeRom(nativeUsb = true)
        flashWith(rom, Mode.FirmwareOnly, files)
        assertTrue(rom.flash.copyOfRange(0x810000, 0x810000 + 4096).all { it == 0x5A.toByte() })
        val fw = files.getValue("firmware.bin")
        assertArrayEquals(fw, rom.flash.copyOfRange(0x10000, 0x10000 + fw.size))
    }

    @Test
    fun usbSerialChipSwitchesToFastBaud() {
        val rom = FakeRom(nativeUsb = false)
        flashWith(rom, Mode.FirmwareOnly, images(Random(3)))
        assertEquals(Flasher.FAST_BAUD, rom.currentBaud)
        assertTrue(EspLoader.CHANGE_BAUDRATE in rom.ops)
        assertTrue(rom.regWrites.none { it.first == EspLoader.RTC_CNTL_WDTCONFIG0_REG }) // a UART: watchdogs are reset
    }

    @Test
    fun badFlashFailsVerification() {
        val rom = FakeRom().apply { corruptWrites = true }
        try {
            flashWith(rom, Mode.FirmwareOnly, images(Random(4)))
            fail("expected a verify error")
        } catch (e: EspException) {
            assertTrue(e.message, e.message!!.startsWith("verify failed"))
        }
    }

    @Test
    fun silentBoardGivesTheBootButtonHint() {
        val rom = object : SerialLink by FakeRom() {
            override fun setLines(dtr: Boolean, rts: Boolean) {} // never enters download mode
        }
        val img = listOf(BoardImage(0x10000, "firmware.bin", BoardImage.Part.App) { ByteArray(10) })
        try {
            Flasher(log = {}, sleep = noSleep).run({ rom }, img, Mode.Everything, {}) { _, _ -> }
            fail("expected no answer")
        } catch (e: EspException) {
            assertTrue(e.message!!.contains("BOOT"))
        }
    }

    @Test
    fun checksumAndCh340Baud() {
        assertEquals(0xEF, EspLoader.checksum(ByteArray(0)))
        assertEquals(0xEF xor 0x01 xor 0x02, EspLoader.checksum(byteArrayOf(1, 2)))
        // 1532620800 / 115200 = 13304 → 0x10000 - 13304 = 0xcc08; divisor 3 | 0x80.
        assertEquals(Pair(0xCC83, 0x08), org.meshmsg.boardlink.UsbSerial.ch34xBaud(115200))
    }
}
