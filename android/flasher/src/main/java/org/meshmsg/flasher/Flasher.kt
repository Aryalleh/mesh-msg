package org.meshmsg.flasher

import org.meshmsg.flasher.esp.EspException
import org.meshmsg.flasher.esp.EspLoader
import org.meshmsg.boardlink.SerialLink
import java.io.Closeable
import java.io.IOException

/** One region of the board image set (assets/board/images.txt). */
class BoardImage(val offset: Int, val name: String, val part: Part, val load: () -> ByteArray) {
    enum class Part { Boot, App, Fs }
}

/**
 * The board images bundled in this APK, written by scripts/build-flasher.sh:
 * images.txt lists "<offset> <file> <boot|app|fs>" per line.
 *   boot.bin      0x0       bootloader + partition table (dio, 80 MHz, 16 MB)
 *   boot_app0.bin 0xe000    OTA data: start the first app slot
 *   firmware.bin  0x10000   the firmware
 *   littlefs.bin  0x810000  file system: web client, and identity + settings
 */
object BoardImages {
    fun parse(manifest: String, load: (String) -> ByteArray): List<BoardImage> =
        manifest.lines().map { it.trim() }.filter { it.isNotEmpty() && !it.startsWith("#") }.map { line ->
            val f = line.split(Regex("\\s+"))
            require(f.size == 3) { "bad images.txt line: $line" }
            val part = when (f[2]) {
                "boot" -> BoardImage.Part.Boot
                "app" -> BoardImage.Part.App
                "fs" -> BoardImage.Part.Fs
                else -> throw IllegalArgumentException("bad part in images.txt: ${f[2]}")
            }
            BoardImage(Integer.decode(f[0]), f[1], part) { load(f[1]) }
        }
}

enum class Mode {
    /** Firmware + file system: the web client, a new identity and device password. */
    Everything,

    /** Bootloader, partition table and firmware: identity, settings and stored messages stay. */
    FirmwareOnly,
}

sealed interface Phase {
    data object Connecting : Phase
    data class Writing(val name: String, val index: Int, val count: Int) : Phase
    data object Restarting : Phase
}

/** The whole job: connect, check the chip, write each region, verify, restart. */
class Flasher(
    private val log: (String) -> Unit,
    private val sleep: (Long) -> Unit = { Thread.sleep(it) },
) {
    fun run(
        open: () -> SerialLink,
        images: List<BoardImage>,
        mode: Mode,
        phase: (Phase) -> Unit,
        progress: (done: Long, total: Long) -> Unit,
    ) {
        val chosen = images.filter { mode == Mode.Everything || it.part != BoardImage.Part.Fs }.sortedBy { it.offset }
        if (chosen.isEmpty()) throw EspException("this flasher carries no board images")
        val data = chosen.map { it to it.load() }
        val total = data.sumOf { it.second.size.toLong() }

        phase(Phase.Connecting)
        var link = open()
        var loader = EspLoader(link, sleep, log)
        var tries = 0
        while (true) {
            try {
                loader.connect()
                break
            } catch (e: IOException) {
                // The board's own USB can drop off the bus while the chip resets: open it again.
                if (e is EspException || ++tries > 3) throw e
                log("USB port gone (${e.message}); opening it again")
                (link as? Closeable)?.close()
                sleep(1500)
                link = open()
                loader = EspLoader(link, sleep, log)
            }
        }
        try {
            log("connected over ${if (link.nativeUsb) "the board's USB" else "a USB-serial chip"}")
            if (!loader.isEsp32S3()) throw EspException("this chip is not an ESP32-S3: the Mava board firmware won't run on it")
            loader.disableWatchdogs()
            if (!link.nativeUsb) loader.changeBaud(FAST_BAUD)
            loader.spiAttach()
            loader.spiSetParams(EspLoader.FLASH_16MB)
            var done = 0L
            data.forEachIndexed { i, (img, bytes) ->
                phase(Phase.Writing(img.name, i + 1, data.size))
                if (img.offset + bytes.size > EspLoader.FLASH_16MB) throw EspException("${img.name} does not fit in 16 MB")
                loader.flash(img.offset, bytes) { n -> progress(done + n, total) }
                done += bytes.size
            }
            phase(Phase.Restarting)
            loader.hardReset()
            log("done: the board restarts with the new firmware")
        } finally {
            (link as? Closeable)?.close()
        }
    }

    companion object {
        const val FAST_BAUD = 460800
    }
}
