package org.meshmsg.boardlink

/** A serial port: the ESP32-S3's own USB, or a USB-serial chip (CP210x, CH34x). */
interface SerialLink {
    /** The ESP32-S3's USB-Serial/JTAG (vendor 303a): its own reset logic. */
    val nativeUsb: Boolean
    fun write(data: ByteArray)
    /** Reads what is there, waiting up to [timeoutMs]; 0 = nothing arrived. */
    fun read(buf: ByteArray, timeoutMs: Int): Int
    fun setLines(dtr: Boolean, rts: Boolean)
    fun setBaud(baud: Int)
}
