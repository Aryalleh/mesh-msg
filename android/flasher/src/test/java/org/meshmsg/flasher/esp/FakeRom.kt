package org.meshmsg.flasher.esp

import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.MessageDigest
import java.util.zip.Inflater

/**
 * An ESP32-S3 ROM bootloader on the other end of the cable, as far as the
 * flasher uses it: answers only after a reset into download mode, prints boot
 * text, inflates FLASH_DEFL_DATA into a 16 MB flash and answers MD5 from it.
 */
class FakeRom(override val nativeUsb: Boolean = true) : SerialLink {
    val flash = ByteArray(16 * 1024 * 1024) { 0x5A }
    val lines = mutableListOf<Pair<Boolean, Boolean>>()
    var currentBaud = 115200
    var inDownloadMode = false
    var corruptWrites = false
    var flashSizeSet = 0
    val ops = mutableListOf<Int>()
    val regWrites = mutableListOf<Pair<Int, Int>>()

    private val out = ArrayDeque<Byte>()
    private val dec = SlipDecoder()
    private var writePos = 0
    private var expectSeq = 0
    private var inflater: Inflater? = null

    override fun setLines(dtr: Boolean, rts: Boolean) {
        lines += dtr to rts
        // Reset with the boot pin held: USB-Serial/JTAG reaches (DTR=0, RTS=1) after
        // (DTR=1, RTS=0); the auto-reset circuit releases EN while IO0 is low.
        val n = lines.size
        if (n >= 2) {
            val prev = lines[n - 2]
            val enteredDownload = if (nativeUsb) prev == (true to true) && (dtr to rts) == (false to true)
            else prev == (true to true) && (dtr to rts) == (true to false)
            if (enteredDownload) {
                inDownloadMode = true
                "ESP-ROM:esp32s3-20210327\r\nrst:0x1 (POWERON),boot:0x0 (DOWNLOAD(USB/UART0))\r\nwaiting for download\r\n"
                    .toByteArray().forEach { out.addLast(it) }
            }
        }
    }

    override fun setBaud(baud: Int) {
        currentBaud = baud
    }

    override fun read(buf: ByteArray, timeoutMs: Int): Int {
        var n = 0
        while (n < buf.size && out.isNotEmpty()) buf[n++] = out.removeFirst()
        return n
    }

    override fun write(data: ByteArray) {
        dec.feed(data, data.size)
        while (true) {
            val f = dec.poll() ?: break
            handle(f)
        }
    }

    private fun handle(f: ByteArray) {
        val b = ByteBuffer.wrap(f).order(ByteOrder.LITTLE_ENDIAN)
        require(b.get().toInt() == 0) { "not a request" }
        val op = b.get().toInt() and 0xFF
        val len = b.short.toInt() and 0xFFFF
        val chk = b.int
        val data = f.copyOfRange(8, 8 + len)
        require(data.size == len) { "short request" }
        if (!inDownloadMode) return // the app is running: no answers
        ops += op
        val p = ByteBuffer.wrap(data).order(ByteOrder.LITTLE_ENDIAN)
        when (op) {
            EspLoader.SYNC -> {
                require(data.contentEquals(EspLoader.SYNC_DATA))
                repeat(4) { reply(op) }
            }
            EspLoader.READ_REG -> reply(
                op,
                value = when (p.int) {
                    EspLoader.CHIP_MAGIC_REG -> EspLoader.ESP32S3_MAGIC
                    EspLoader.UARTDEV_BUF_NO -> if (nativeUsb) EspLoader.UARTDEV_USB_JTAG_SERIAL else 0
                    else -> 0
                },
            )
            EspLoader.WRITE_REG -> {
                require(len == 16)
                regWrites += p.int to p.int
                reply(op)
            }
            EspLoader.SPI_ATTACH -> { require(len == 8); reply(op) }
            EspLoader.SPI_SET_PARAMS -> { p.int; flashSizeSet = p.int; reply(op) }
            EspLoader.CHANGE_BAUDRATE -> { require(!nativeUsb); reply(op) }
            EspLoader.FLASH_DEFL_BEGIN -> {
                require(len == 20) { "S3 ROM takes 5 words" }
                val erase = p.int; p.int; require(p.int == EspLoader.FLASH_WRITE_SIZE)
                val offset = p.int
                require(offset % 4096 == 0)
                flash.fill(0xFF.toByte(), offset, offset + erase)
                writePos = offset
                expectSeq = 0
                inflater = Inflater()
                reply(op)
            }
            EspLoader.FLASH_DEFL_DATA -> {
                val size = p.int
                val seq = p.int
                val block = data.copyOfRange(16, 16 + size)
                if (seq != expectSeq || EspLoader.checksum(block) != chk) return reply(op, status = 1, error = 0x07)
                expectSeq++
                val inf = inflater!!
                inf.setInput(block)
                val buf = ByteArray(64 * 1024)
                while (true) {
                    val n = inf.inflate(buf)
                    if (n == 0) break
                    if (corruptWrites) buf[0] = (buf[0] + 1).toByte()
                    System.arraycopy(buf, 0, flash, writePos, n)
                    writePos += n
                }
                reply(op)
            }
            EspLoader.SPI_FLASH_MD5 -> {
                val off = p.int
                val size = p.int
                val md = MessageDigest.getInstance("MD5")
                md.update(flash, off, size)
                reply(op, body = md.digest().joinToString("") { "%02x".format(it) }.toByteArray())
            }
            else -> reply(op, status = 1, error = 0x05)
        }
    }

    private fun reply(op: Int, value: Int = 0, body: ByteArray = ByteArray(0), status: Int = 0, error: Int = 0) {
        val payload = body + byteArrayOf(status.toByte(), error.toByte(), 0, 0)
        val r = ByteBuffer.allocate(8 + payload.size).order(ByteOrder.LITTLE_ENDIAN)
            .put(1).put(op.toByte()).putShort(payload.size.toShort()).putInt(value).put(payload).array()
        val enc = ByteArrayOutputStream()
        enc.write(Slip.encode(r))
        enc.toByteArray().forEach { out.addLast(it) }
    }
}
