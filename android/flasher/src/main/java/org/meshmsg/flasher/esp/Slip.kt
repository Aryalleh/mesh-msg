package org.meshmsg.flasher.esp

import java.io.ByteArrayOutputStream

/** SLIP framing of Espressif's serial bootloader protocol. */
object Slip {
    const val END = 0xC0
    const val ESC = 0xDB
    const val ESC_END = 0xDC
    const val ESC_ESC = 0xDD

    fun encode(packet: ByteArray): ByteArray {
        val out = ByteArrayOutputStream(packet.size + packet.size / 32 + 2)
        out.write(END)
        for (b in packet) {
            when (b.toInt() and 0xFF) {
                END -> { out.write(ESC); out.write(ESC_END) }
                ESC -> { out.write(ESC); out.write(ESC_ESC) }
                else -> out.write(b.toInt())
            }
        }
        out.write(END)
        return out.toByteArray()
    }
}

/**
 * Splits a byte stream at SLIP END bytes. Everything between two ENDs is a
 * frame, so text the chip prints at boot comes out as junk frames, which the
 * loader drops because they don't parse as responses.
 */
class SlipDecoder {
    private val cur = ByteArrayOutputStream()
    private var esc = false
    private val frames = ArrayDeque<ByteArray>()

    fun feed(buf: ByteArray, n: Int) {
        for (i in 0 until n) {
            val b = buf[i].toInt() and 0xFF
            if (esc) {
                esc = false
                cur.write(
                    when (b) {
                        Slip.ESC_END -> Slip.END
                        Slip.ESC_ESC -> Slip.ESC
                        else -> b
                    },
                )
                continue
            }
            when (b) {
                Slip.END -> {
                    if (cur.size() > 0) frames.addLast(cur.toByteArray())
                    cur.reset()
                }
                Slip.ESC -> esc = true
                else -> cur.write(b)
            }
        }
    }

    fun poll(): ByteArray? = frames.removeFirstOrNull()

    fun clear() {
        frames.clear()
        cur.reset()
        esc = false
    }
}
