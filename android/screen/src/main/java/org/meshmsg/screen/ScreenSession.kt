package org.meshmsg.screen

import org.meshmsg.boardlink.HostLink
import org.meshmsg.boardlink.SerialLink
import java.io.ByteArrayOutputStream
import java.io.IOException

/**
 * The phone's copy of the board's screen, rebuilt from SCREEN events
 * (firmware/lib/meshui/src/mirror.h): an info event gives the size, tile
 * events carry 16×8 areas, raw or run-length RGB565 (little-endian).
 */
class ScreenFrame {
    var width = 0
        private set
    var height = 0
        private set

    /** ARGB_8888 pixels, row by row (empty until the info event). */
    var pixels = IntArray(0)
        private set

    /** Grows with every change, so the UI knows when to redraw. */
    @Volatile var version = 0L
        private set

    /** Applies one SCREEN event's data; false if it isn't one. */
    @Synchronized
    fun apply(d: ByteArray): Boolean {
        if (d.isEmpty()) return false
        return when (d[0].toInt()) {
            INFO -> info(d)
            TILE -> tile(d)
            else -> false
        }
    }

    private fun u16(d: ByteArray, i: Int) = ((d[i].toInt() and 0xFF) shl 8) or (d[i + 1].toInt() and 0xFF)

    private fun info(d: ByteArray): Boolean {
        if (d.size < 7) return false
        val w = u16(d, 1)
        val h = u16(d, 3)
        if (w <= 0 || h <= 0 || w > 2048 || h > 2048) return false
        if (w != width || h != height) {
            width = w
            height = h
            pixels = IntArray(w * h) { 0xFF000000.toInt() }
        }
        version++
        return true
    }

    private fun tile(d: ByteArray): Boolean {
        if (d.size < 8 || width == 0) return false
        val x = u16(d, 1)
        val y = u16(d, 3)
        val w = d[5].toInt() and 0xFF
        val h = d[6].toInt() and 0xFF
        if (w == 0 || h == 0 || x + w > width || y + h > height) return false
        val n = w * h
        val m = d.size - 8
        when (d[7].toInt()) {
            RAW -> {
                if (m != n * 2) return false
                for (i in 0 until n) {
                    val c = (d[8 + 2 * i].toInt() and 0xFF) or ((d[9 + 2 * i].toInt() and 0xFF) shl 8)
                    pixels[(y + i / w) * width + x + i % w] = argb(c)
                }
            }
            RLE -> {
                if (m % 3 != 0) return false
                var i = 0
                var k = 8
                while (k < d.size) {
                    val cnt = d[k].toInt() and 0xFF
                    val c = argb((d[k + 1].toInt() and 0xFF) or ((d[k + 2].toInt() and 0xFF) shl 8))
                    if (cnt == 0 || i + cnt > n) return false
                    repeat(cnt) {
                        pixels[(y + i / w) * width + x + i % w] = c
                        i++
                    }
                    k += 3
                }
                if (i != n) return false
            }
            else -> return false
        }
        version++
        return true
    }

    @Synchronized
    fun copyTo(out: IntArray) {
        pixels.copyInto(out, 0, 0, minOf(out.size, pixels.size))
    }

    companion object {
        const val INFO = 0
        const val TILE = 1
        const val RAW = 0
        const val RLE = 1

        fun argb(c: Int): Int {
            val r = (c shr 11) and 0x1F
            val g = (c shr 5) and 0x3F
            val b = c and 0x1F
            return (0xFF shl 24) or (((r shl 3) or (r shr 2)) shl 16) or (((g shl 2) or (g shr 4)) shl 8) or ((b shl 3) or (b shr 2))
        }
    }
}

/** keys::Action of the firmware (lib/meshkeys/src/keys.h), in its order. */
enum class Key(val code: Int) {
    Text(1), Backspace(2), Enter(3), Up(4), Down(5), Left(6), Right(7), Ok(8),
    Call(9), End(10), Menu(11), Back(12), VolUp(13), VolDown(14), Symbols(15),
}

/**
 * One connection to the board's pre-release hardware simulators
 * (firmware/src/hwsim): the screen (SCREEN, into [frame]) unless [picture] is
 * off, touches, keys and typing (INPUT), and the board's speaker and
 * microphone (AUDIO, [onAudio]). [run] blocks until the port fails or
 * [close] is called.
 */
class ScreenSession(
    private val port: SerialLink,
    val frame: ScreenFrame,
    private var picture: Boolean = true,
    private val log: (String) -> Unit = {},
) {
    @Volatile private var closed = false
    @Volatile var started: Int? = null // the board's ACK status for SCREEN (or the first command), once it answered
        private set
    @Volatile var audioAck: Int? = null // the board's ACK status for AUDIO start
        private set
    private var req = 0
    private val writeLock = Any()
    @Volatile private var lastSent = 0L

    /** AUDIO event data (speaker frames, format), on the reading thread. */
    var onAudio: (ByteArray) -> Unit = {}

    private fun send(op: Int, args: ByteArray) {
        synchronized(writeLock) {
            req = (req % 255) + 1
            port.write(HostLink.encode(HostLink.KIND_COMMAND, byteArrayOf(op.toByte(), req.toByte()) + args))
            lastSent = System.currentTimeMillis()
        }
    }

    /** The picture on or off; off keeps keys, keyboard and audio (the board's own display works). */
    fun picture(on: Boolean) {
        if (closed || on == picture) return
        picture = on
        runCatching { send(OP_SCREEN, byteArrayOf(if (on) 1 else 0)) }
    }

    /** The phone as the board's speaker and / or microphone (both off: stop). */
    fun audio(speaker: Boolean, mic: Boolean) {
        if (closed) return
        audioAck = null
        runCatching { send(OP_AUDIO, AudioLink.start(speaker, mic)) }
    }

    /** A microphone frame (AudioLink.mic arguments). */
    fun micFrame(args: ByteArray) {
        if (closed) return
        send(OP_AUDIO, args)
    }

    fun touch(x: Int, y: Int, pressed: Boolean) {
        if (closed) return
        runCatching {
            send(OP_INPUT, byteArrayOf(0, (x shr 8).toByte(), x.toByte(), (y shr 8).toByte(), y.toByte(), if (pressed) 1 else 0))
        }
    }

    fun key(k: Key, cp: Int = 0) {
        if (closed) return
        runCatching {
            send(OP_INPUT, byteArrayOf(1, k.code.toByte(), (cp shr 24).toByte(), (cp shr 16).toByte(), (cp shr 8).toByte(), cp.toByte()))
        }
    }

    /** Board options: id 1 = the touch keyboard (false hides it: the phone types). */
    fun option(id: Int, on: Boolean) {
        if (closed) return
        runCatching { send(OP_INPUT, byteArrayOf(2, id.toByte(), if (on) 1 else 0, 0, 0, 0)) }
    }

    /** Types text on the board, one Text key per code point. */
    fun type(text: String) {
        var i = 0
        while (i < text.length) {
            val cp = text.codePointAt(i)
            if (cp == '\n'.code) key(Key.Enter) else if (cp >= 0x20) key(Key.Text, cp)
            i += Character.charCount(cp)
        }
    }

    /** Sent right after SCREEN 1 (before any frame), e.g. to hide the touch keyboard. */
    var onStart: (ScreenSession) -> Unit = {}

    fun run() {
        // Without the picture GET_STATUS opens the session: the board answers
        // every port, and its answer tells that the firmware runs.
        if (picture) send(OP_SCREEN, byteArrayOf(1)) else send(OP_GET_STATUS, byteArrayOf())
        onStart(this)
        val buf = ByteArray(16 * 1024)
        val pending = ByteArrayOutputStream()
        while (!closed) {
            // Something every 20 s: the board goes back to its text log after
            // 90 s without a word from the host (keys only, nothing typed).
            if (System.currentTimeMillis() - lastSent > 20_000) runCatching { send(OP_GET_STATUS, byteArrayOf()) }
            val n = try {
                port.read(buf, 200)
            } catch (e: IOException) {
                if (closed) return
                throw e
            }
            for (i in 0 until n) {
                val b = buf[i]
                if (b.toInt() != 0) {
                    if (pending.size() < 4096) pending.write(b.toInt())
                    continue
                }
                val f = HostLink.decode(pending.toByteArray())
                pending.reset()
                if (f == null || f.first != HostLink.KIND_EVENT || f.second.size < 2) continue
                val p = f.second
                when (p[0].toInt() and 0xFF) {
                    EV_SCREEN -> frame.apply(p.copyOfRange(2, p.size))
                    EV_AUDIO -> onAudio(p.copyOfRange(2, p.size))
                    EV_STATUS -> if (started == null) started = HostLink.ACK_OK // keys-only session: the board is there
                    HostLink.EV_ACK -> if (p.size >= 4) when (p[2].toInt() and 0xFF) {
                        OP_SCREEN -> {
                            started = p[3].toInt() and 0xFF
                            log("board answered SCREEN: ${p[3].toInt() and 0xFF}")
                        }
                        OP_AUDIO -> if (audioAck == null) audioAck = p[3].toInt() and 0xFF
                    }
                }
            }
        }
    }

    fun close() {
        if (closed) return
        runCatching { send(OP_AUDIO, AudioLink.start(speaker = false, mic = false)) }
        runCatching { send(OP_SCREEN, byteArrayOf(0)) }
        closed = true
    }

    companion object {
        const val OP_GET_STATUS = 0x02
        const val OP_SCREEN = 0x0B
        const val OP_INPUT = 0x0C
        const val OP_AUDIO = 0x0D
        const val EV_STATUS = 0x81
        const val EV_SCREEN = 0x86
        const val EV_AUDIO = 0x87
    }
}
