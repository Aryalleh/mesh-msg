package org.meshmsg.flasher.esp

import java.io.IOException
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.MessageDigest
import java.util.zip.Deflater
import org.meshmsg.boardlink.SerialLink

class EspException(message: String) : IOException(message)

/**
 * Espressif's ROM serial bootloader protocol, the subset esptool uses to write
 * flash on an ESP32-S3 without its RAM stub: sync, attach the SPI flash, write
 * compressed (FLASH_DEFL_*), check each region with the ROM's MD5, reset.
 * Same offsets and flash settings as the Linux flasher (tools/flasher/mava-flash).
 */
class EspLoader(
    private val port: SerialLink,
    private val sleep: (Long) -> Unit = { Thread.sleep(it) },
    private val log: (String) -> Unit = {},
) {
    private val slip = SlipDecoder()
    private val rx = ByteArray(16 * 1024)

    class Response(val value: Int, val data: ByteArray)

    /** Resets the chip into download mode and syncs. Also finds a board already in it. */
    fun connect(attempts: Int = 8) {
        for (i in 0 until attempts) {
            // Even attempts reset the chip; odd ones only listen (BOOT held while resetting).
            if (i % 2 == 0) {
                log("reset into download mode (attempt ${i / 2 + 1})")
                resetIntoBootloader(slow = i >= 4)
            }
            drain(if (i % 2 == 0) 300 else 50)
            repeat(5) { if (trySync()) return }
        }
        throw EspException("the board does not answer: hold BOOT, press and release RESET, release BOOT, and try again")
    }

    private fun trySync(): Boolean =
        try {
            command(SYNC, SYNC_DATA, timeoutMs = 120)
            drain(60) // the ROM answers a sync several times
            true
        } catch (e: EspException) {
            false
        }

    fun resetIntoBootloader(slow: Boolean = false) {
        // Line states follow esptool's reset sequences step by step, including
        // the intermediate ones (it sets DTR and RTS one at a time).
        if (port.nativeUsb) {
            // USB-Serial/JTAG: its own logic, DTR = boot pin, RTS = reset.
            port.setLines(dtr = false, rts = false); sleep(100)
            port.setLines(dtr = true, rts = false); sleep(100)
            port.setLines(dtr = true, rts = true)
            port.setLines(dtr = false, rts = true); sleep(100)
            port.setLines(dtr = false, rts = false)
        } else {
            // Dev board auto-reset circuit: RTS → EN, DTR → IO0, both inverted.
            port.setLines(dtr = false, rts = true); sleep(100)
            port.setLines(dtr = true, rts = true)
            port.setLines(dtr = true, rts = false); sleep(if (slow) 400 else 50)
            port.setLines(dtr = false, rts = false)
        }
    }

    fun hardReset() {
        // Clear "force download boot" first, or the chip can come back in download mode
        // (esptool does the same; arduino-esp32 issue 6762).
        try {
            writeReg(RTC_CNTL_OPTION1_REG, 0, mask = 0x1)
        } catch (e: IOException) {
            log("could not clear force-download (${e.message}); resetting anyway")
        }
        port.setLines(dtr = false, rts = true)
        sleep(if (port.nativeUsb) 200 else 100)
        port.setLines(dtr = false, rts = false)
        if (port.nativeUsb) sleep(200)
    }

    fun readReg(addr: Int): Int = command(READ_REG, le(addr)).value

    fun writeReg(addr: Int, value: Int, mask: Int = -1) {
        command(WRITE_REG, le(addr, value, mask, 0))
    }

    /** The ROM talks over the chip's own USB-Serial/JTAG (not a UART). */
    fun usesUsbJtagSerial(): Boolean = readReg(UARTDEV_BUF_NO) and 0xFF == UARTDEV_USB_JTAG_SERIAL

    /**
     * Over USB-Serial/JTAG the RTC and super watchdogs keep running in download
     * mode and can reset the chip in the middle of a write: switch them off, as
     * esptool does.
     */
    fun disableWatchdogs() {
        if (!usesUsbJtagSerial()) return
        writeReg(RTC_CNTL_WDTWPROTECT_REG, RTC_CNTL_WDT_WKEY)
        writeReg(RTC_CNTL_WDTCONFIG0_REG, 0)
        writeReg(RTC_CNTL_WDTWPROTECT_REG, 0)
        writeReg(RTC_CNTL_SWD_WPROTECT_REG, RTC_CNTL_SWD_WKEY)
        writeReg(RTC_CNTL_SWD_CONF_REG, readReg(RTC_CNTL_SWD_CONF_REG) or RTC_CNTL_SWD_AUTO_FEED_EN)
        writeReg(RTC_CNTL_SWD_WPROTECT_REG, 0)
        log("watchdogs off for flashing (USB-Serial/JTAG)")
    }

    /** ESP32-S3: CHIP_DETECT_MAGIC_REG reads 9. */
    fun isEsp32S3(): Boolean = readReg(CHIP_MAGIC_REG) == ESP32S3_MAGIC

    fun spiAttach() {
        command(SPI_ATTACH, ByteArray(8))
    }

    fun spiSetParams(flashSize: Int) {
        command(SPI_SET_PARAMS, le(0, flashSize, 64 * 1024, 4 * 1024, 256, 0xFFFF))
    }

    /** USB-serial chips only: 115200 is slow for 10 MB. */
    fun changeBaud(baud: Int) {
        command(CHANGE_BAUDRATE, le(baud, 0))
        port.setBaud(baud)
        sleep(50)
        drain(50)
    }

    /**
     * Writes [image] at [offset] compressed, then checks it against the ROM's
     * MD5 of the flash. [progress] gets the bytes of [image] written so far.
     */
    fun flash(offset: Int, image: ByteArray, progress: (Int) -> Unit = {}) {
        val comp = deflate(image)
        val blocks = (comp.size + FLASH_WRITE_SIZE - 1) / FLASH_WRITE_SIZE
        val eraseSize = (image.size + FLASH_WRITE_SIZE - 1) / FLASH_WRITE_SIZE * FLASH_WRITE_SIZE
        log("write 0x%06x: %d bytes (%d compressed)".format(offset, image.size, comp.size))
        // The ROM erases the region before it answers.
        command(FLASH_DEFL_BEGIN, le(eraseSize, blocks, FLASH_WRITE_SIZE, offset, 0), timeoutMs = perMb(ERASE_MS_PER_MB, eraseSize))
        val ratio = image.size.toDouble() / comp.size.coerceAtLeast(1)
        for (seq in 0 until blocks) {
            val from = seq * FLASH_WRITE_SIZE
            val to = minOf(from + FLASH_WRITE_SIZE, comp.size)
            val block = comp.copyOfRange(from, to)
            val payload = ByteBuffer.allocate(16 + block.size).order(ByteOrder.LITTLE_ENDIAN)
                .putInt(block.size).putInt(seq).putInt(0).putInt(0).put(block).array()
            command(FLASH_DEFL_DATA, payload, checksum(block), perMb(WRITE_MS_PER_MB, (block.size * ratio).toInt()))
            progress(((seq + 1).toLong() * image.size / blocks).toInt())
        }
        // The ROM acknowledges a block before writing it: the MD5 below waits
        // for the last one, then proves what is in flash.
        val want = md5Hex(image)
        val got = flashMd5(offset, image.size)
        if (!got.equals(want, ignoreCase = true)) throw EspException("verify failed at 0x%06x: flash %s, image %s".format(offset, got, want))
        log("verified 0x%06x (md5 %s)".format(offset, want))
    }

    fun flashMd5(offset: Int, size: Int): String {
        val r = command(SPI_FLASH_MD5, le(offset, size, 0, 0), timeoutMs = perMb(MD5_MS_PER_MB, size))
        if (r.data.size < 16) throw EspException("short MD5 answer")
        // The ROM sends 32 hex characters, a stub 16 raw bytes.
        return if (r.data.size >= 32) String(r.data, 0, 32, Charsets.US_ASCII) else r.data.copyOf(16).toHex()
    }

    fun command(op: Int, data: ByteArray, checksum: Int = 0, timeoutMs: Int = DEFAULT_TIMEOUT_MS): Response {
        val pkt = ByteBuffer.allocate(8 + data.size).order(ByteOrder.LITTLE_ENDIAN)
            .put(0).put(op.toByte()).putShort(data.size.toShort()).putInt(checksum).put(data).array()
        port.write(Slip.encode(pkt))
        val deadline = System.nanoTime() + timeoutMs * 1_000_000L
        while (true) {
            val f = nextFrame(deadline) ?: throw EspException("no answer to command 0x%02x".format(op))
            if (f.size < 8 || f[0].toInt() != 1 || (f[1].toInt() and 0xFF) != op) continue
            val size = (f[2].toInt() and 0xFF) or ((f[3].toInt() and 0xFF) shl 8)
            if (size < 2 || f.size < 8 + size) continue
            val value = ByteBuffer.wrap(f, 4, 4).order(ByteOrder.LITTLE_ENDIAN).int
            val body = f.copyOfRange(8, 8 + size)
            // The ROM ends each answer with 4 status bytes (a stub with 2).
            val st = if (size >= 4) size - 4 else size - 2
            if (body[st].toInt() != 0) {
                throw EspException("command 0x%02x failed: %s".format(op, romError(body[st + 1].toInt() and 0xFF)))
            }
            return Response(value, body.copyOfRange(0, st))
        }
    }

    private fun nextFrame(deadline: Long): ByteArray? {
        while (true) {
            slip.poll()?.let { return it }
            val left = (deadline - System.nanoTime()) / 1_000_000L
            if (left <= 0) return null
            val n = port.read(rx, left.coerceAtMost(100).toInt())
            if (n > 0) slip.feed(rx, n)
        }
    }

    /** Reads and drops whatever arrives within [ms] (boot text, extra sync answers). */
    fun drain(ms: Int) {
        val deadline = System.nanoTime() + ms * 1_000_000L
        while (true) {
            val left = (deadline - System.nanoTime()) / 1_000_000L
            if (left <= 0) break
            port.read(rx, left.coerceAtMost(50).toInt())
        }
        slip.clear()
    }

    companion object {
        const val SYNC = 0x08
        const val WRITE_REG = 0x09
        const val READ_REG = 0x0A
        const val SPI_SET_PARAMS = 0x0B
        const val SPI_ATTACH = 0x0D
        const val CHANGE_BAUDRATE = 0x0F
        const val FLASH_DEFL_BEGIN = 0x10
        const val FLASH_DEFL_DATA = 0x11
        const val FLASH_DEFL_END = 0x12
        const val SPI_FLASH_MD5 = 0x13

        const val FLASH_WRITE_SIZE = 0x400 // the ROM's block size
        const val CHIP_MAGIC_REG = 0x40001000
        const val ESP32S3_MAGIC = 9
        const val FLASH_16MB = 16 * 1024 * 1024

        // ESP32-S3 registers (esptool targets/esp32s3.py).
        const val UARTDEV_BUF_NO = 0x3FCEF14C // ROM variable: the port in use
        const val UARTDEV_USB_JTAG_SERIAL = 4
        const val RTC_CNTL_WDTCONFIG0_REG = 0x60008098
        const val RTC_CNTL_WDTWPROTECT_REG = 0x600080B0
        const val RTC_CNTL_WDT_WKEY = 0x50D83AA1
        const val RTC_CNTL_SWD_CONF_REG = 0x600080B4
        const val RTC_CNTL_SWD_WPROTECT_REG = 0x600080B8
        const val RTC_CNTL_SWD_WKEY = 0x8F1D312A.toInt()
        const val RTC_CNTL_SWD_AUTO_FEED_EN = 1 shl 31
        const val RTC_CNTL_OPTION1_REG = 0x6000812C

        private const val DEFAULT_TIMEOUT_MS = 3000
        private const val ERASE_MS_PER_MB = 30_000
        private const val WRITE_MS_PER_MB = 40_000
        private const val MD5_MS_PER_MB = 8_000

        val SYNC_DATA = byteArrayOf(0x07, 0x07, 0x12, 0x20) + ByteArray(32) { 0x55 }

        fun checksum(data: ByteArray): Int {
            var c = 0xEF
            for (b in data) c = c xor (b.toInt() and 0xFF)
            return c
        }

        fun deflate(data: ByteArray): ByteArray {
            val d = Deflater(Deflater.BEST_COMPRESSION) // zlib format, as esptool's zlib.compress
            d.setInput(data)
            d.finish()
            val out = java.io.ByteArrayOutputStream(data.size / 4 + 64)
            val buf = ByteArray(64 * 1024)
            while (!d.finished()) out.write(buf, 0, d.deflate(buf))
            d.end()
            return out.toByteArray()
        }

        fun md5Hex(data: ByteArray): String = MessageDigest.getInstance("MD5").digest(data).toHex()

        private fun ByteArray.toHex() = joinToString("") { "%02x".format(it) }

        private fun perMb(msPerMb: Int, bytes: Int): Int = maxOf(DEFAULT_TIMEOUT_MS, (msPerMb.toLong() * bytes / (1024 * 1024)).toInt())

        private fun le(vararg v: Int): ByteArray {
            val b = ByteBuffer.allocate(4 * v.size).order(ByteOrder.LITTLE_ENDIAN)
            v.forEach { b.putInt(it) }
            return b.array()
        }

        fun romError(code: Int): String = when (code) {
            0x05 -> "invalid message"
            0x06 -> "failed to act on message"
            0x07 -> "invalid CRC"
            0x08 -> "flash write error"
            0x09 -> "flash read error"
            0x0A -> "flash read length error"
            0x0B -> "deflate error"
            else -> "error 0x%02x".format(code)
        }
    }
}
