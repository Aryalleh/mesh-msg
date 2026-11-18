package org.meshmsg.screen

import android.annotation.SuppressLint
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import android.media.audiofx.NoiseSuppressor
import kotlin.concurrent.thread

/**
 * AUDIO over the host link (firmware/lib/hwsim/src/audiolink.h), a pre-release
 * hardware simulator: 16 kHz mono signed 16-bit little-endian PCM in 10 ms
 * frames (160 samples), the board's call audio and its sounds.
 *   command: stop 0 | start 1 flags | mic 2 seq(u16 BE) pcm
 *   event:   format 0 rate(u16 BE) frame(u16 BE) | speaker 1 seq(u16 BE) pcm
 */
object AudioLink {
    const val STOP = 0
    const val START = 1
    const val MIC = 2
    const val FORMAT = 0
    const val SPEAKER = 1
    const val FLAG_SPEAKER = 1
    const val FLAG_MIC = 2
    const val RATE = 16000
    const val FRAME = 160
    const val HEADER = 3

    fun start(speaker: Boolean, mic: Boolean): ByteArray =
        if (!speaker && !mic) byteArrayOf(STOP.toByte())
        else byteArrayOf(START.toByte(), ((if (speaker) FLAG_SPEAKER else 0) or (if (mic) FLAG_MIC else 0)).toByte())

    /** A microphone frame: s16 little-endian PCM, at most [FRAME] samples. */
    fun mic(seq: Int, pcm: ByteArray, len: Int = pcm.size): ByteArray {
        require(len % 2 == 0 && len in 2..FRAME * 2) { "bad frame" }
        val out = ByteArray(HEADER + len)
        out[0] = MIC.toByte()
        out[1] = (seq shr 8).toByte()
        out[2] = seq.toByte()
        System.arraycopy(pcm, 0, out, HEADER, len)
        return out
    }

    /** The PCM bytes of a speaker event, or null if the data is something else. */
    fun speaker(d: ByteArray): ByteArray? =
        if (d.size > HEADER && (d[0].toInt() and 0xFF) == SPEAKER && (d.size - HEADER) % 2 == 0) d.copyOfRange(HEADER, d.size) else null

    /** Sample rate and frame size from a format event, or null. */
    fun format(d: ByteArray): Pair<Int, Int>? {
        if (d.size < 5 || (d[0].toInt() and 0xFF) != FORMAT) return null
        val u16 = { i: Int -> ((d[i].toInt() and 0xFF) shl 8) or (d[i + 1].toInt() and 0xFF) }
        return u16(1) to u16(3)
    }
}

/**
 * The phone as the board's speaker (AudioTrack) and microphone (AudioRecord).
 * [sendMic] gets ready AUDIO mic arguments, 100 times a second. Both are set
 * up for voice: the board's calls play on this speaker and take this
 * microphone, so the phone's echo canceller keeps the far side from hearing
 * itself.
 */
class BoardAudio(private val sendMic: (ByteArray) -> Unit) {
    private var track: AudioTrack? = null
    @Volatile private var recording = false
    private var recorder: Thread? = null
    @Volatile var framesIn = 0L
        private set
    @Volatile var framesOut = 0L
        private set

    @Synchronized
    fun speaker(on: Boolean) {
        if (on == (track != null)) return
        if (!on) {
            track?.run { runCatching { stop() }; release() }
            track = null
            return
        }
        val min = AudioTrack.getMinBufferSize(AudioLink.RATE, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT)
        track = AudioTrack.Builder()
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .setAudioFormat(
                AudioFormat.Builder().setSampleRate(AudioLink.RATE).setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT).build(),
            )
            .setBufferSizeInBytes(maxOf(min, AudioLink.FRAME * 2 * 16)) // ~160 ms against USB jitter
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
            .also { it.play() }
    }

    /** A speaker frame from the board. */
    @Synchronized
    fun play(pcm: ByteArray) {
        val t = track ?: return
        t.write(pcm, 0, pcm.size, AudioTrack.WRITE_NON_BLOCKING)
        framesIn++
    }

    /** Needs the RECORD_AUDIO permission (asked by the activity first). */
    @SuppressLint("MissingPermission")
    @Synchronized
    fun mic(on: Boolean) {
        if (on == recording) return
        recording = on
        if (!on) {
            recorder?.join(500)
            recorder = null
            return
        }
        recorder = thread(name = "board-mic") {
            val frameBytes = AudioLink.FRAME * 2
            val min = AudioRecord.getMinBufferSize(AudioLink.RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
            // VOICE_COMMUNICATION: the platform's echo canceller and noise
            // suppression where the phone has them.
            val rec = runCatching {
                AudioRecord(MediaRecorder.AudioSource.VOICE_COMMUNICATION, AudioLink.RATE, AudioFormat.CHANNEL_IN_MONO,
                    AudioFormat.ENCODING_PCM_16BIT, maxOf(min, frameBytes * 8))
            }.getOrNull()
            if (rec == null || rec.state != AudioRecord.STATE_INITIALIZED) {
                rec?.release()
                recording = false
                return@thread
            }
            val aec = if (AcousticEchoCanceler.isAvailable()) runCatching { AcousticEchoCanceler.create(rec.audioSessionId)?.apply { enabled = true } }.getOrNull() else null
            val ns = if (NoiseSuppressor.isAvailable()) runCatching { NoiseSuppressor.create(rec.audioSessionId)?.apply { enabled = true } }.getOrNull() else null
            val buf = ByteArray(frameBytes)
            var seq = 0
            rec.startRecording()
            try {
                while (recording) {
                    var got = 0
                    while (got < frameBytes && recording) {
                        val n = rec.read(buf, got, frameBytes - got)
                        if (n <= 0) break
                        got += n
                    }
                    if (got == frameBytes) {
                        runCatching { sendMic(AudioLink.mic(seq, buf)) }
                        seq = (seq + 1) and 0xFFFF
                        framesOut++
                    }
                }
            } finally {
                runCatching { rec.stop() }
                aec?.release()
                ns?.release()
                rec.release()
            }
        }
    }

    fun close() {
        mic(false)
        speaker(false)
    }
}
