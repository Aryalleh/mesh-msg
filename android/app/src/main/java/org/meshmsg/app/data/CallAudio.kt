package org.meshmsg.app.data

import android.annotation.SuppressLint
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaCodec
import android.media.MediaCodecList
import android.media.MediaFormat
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import android.media.audiofx.NoiseSuppressor
import android.os.Build
import android.util.Log
import org.meshmsg.meshcore.Core
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * The audio of one call (docs/calls.md §4): 16 kHz mono, 20 ms frames.
 * Capture: AudioRecord (VOICE_COMMUNICATION, with the phone's echo canceller
 * and noise suppressor) → MediaCodec encoder → core. Playback: core's jitter
 * buffer → MediaCodec decoder → AudioTrack in voice-call mode, paced by the
 * track. Opus where the phone can encode it (Android 10+), else AMR-WB, which
 * every Android phone with a microphone encodes; both decode everywhere.
 * A lost frame is played as silence.
 */
class CallAudio(private val core: Core, private val id: String, private val codec: Int, private val am: AudioManager) {
    @Volatile private var running = true
    @Volatile var muted = false
    private var oldMode = AudioManager.MODE_NORMAL
    private val threads = mutableListOf<Thread>()

    fun start() {
        oldMode = am.mode
        am.mode = AudioManager.MODE_IN_COMMUNICATION
        threads += Thread(::capture, "call-capture").apply { start() }
        threads += Thread(::playback, "call-playback").apply { start() }
    }

    fun stop() {
        running = false
        threads.forEach { runCatching { it.join(500) } }
        @Suppress("DEPRECATION")
        am.isSpeakerphoneOn = false
        am.mode = oldMode
    }

    fun setSpeaker(on: Boolean) {
        @Suppress("DEPRECATION")
        am.isSpeakerphoneOn = on
    }

    private fun format(): MediaFormat {
        val f = MediaFormat.createAudioFormat(mime(codec), RATE, 1)
        if (codec == OPUS) {
            // Decoder setup: OpusHead, pre-skip and seek pre-roll (ns).
            val head = ByteBuffer.allocate(19).order(ByteOrder.LITTLE_ENDIAN)
            head.put("OpusHead".toByteArray()).put(1.toByte()).put(1.toByte()).putShort(PRE_SKIP.toShort()).putInt(RATE).putShort(0.toShort()).put(0.toByte())
            head.flip()
            f.setByteBuffer("csd-0", head)
            f.setByteBuffer("csd-1", ns(PRE_SKIP * 1_000_000_000L / 48_000))
            f.setByteBuffer("csd-2", ns(80_000_000L))
        }
        return f
    }

    private fun ns(v: Long): ByteBuffer = ByteBuffer.allocate(8).order(ByteOrder.nativeOrder()).putLong(v).also { it.flip() }

    @SuppressLint("MissingPermission") // asked before a call starts
    private fun capture() {
        val min = AudioRecord.getMinBufferSize(RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val rec = runCatching {
            AudioRecord(MediaRecorder.AudioSource.VOICE_COMMUNICATION, RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT, maxOf(min, FRAME_BYTES * 8))
        }.getOrNull() ?: return
        if (rec.state != AudioRecord.STATE_INITIALIZED) { rec.release(); return }
        val aec = if (AcousticEchoCanceler.isAvailable()) AcousticEchoCanceler.create(rec.audioSessionId)?.apply { enabled = true } else null
        val ns = if (NoiseSuppressor.isAvailable()) NoiseSuppressor.create(rec.audioSessionId)?.apply { enabled = true } else null
        val enc = MediaCodec.createEncoderByType(mime(codec))
        val f = MediaFormat.createAudioFormat(mime(codec), RATE, 1)
        f.setInteger(MediaFormat.KEY_BIT_RATE, if (codec == OPUS) 20_000 else 12_650)
        f.setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, FRAME_BYTES)
        try {
            enc.configure(f, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
            enc.start()
            rec.startRecording()
            val pcm = ByteArray(FRAME_BYTES)
            val info = MediaCodec.BufferInfo()
            var pts = 0L
            while (running) {
                var got = 0
                while (got < FRAME_BYTES && running) {
                    val n = rec.read(pcm, got, FRAME_BYTES - got)
                    if (n <= 0) break
                    got += n
                }
                if (got < FRAME_BYTES) continue
                if (muted) java.util.Arrays.fill(pcm, 0.toByte()) // keep the codec's clock, send silence
                val i = enc.dequeueInputBuffer(10_000)
                if (i >= 0) {
                    enc.getInputBuffer(i)!!.apply { clear(); put(pcm) }
                    enc.queueInputBuffer(i, 0, FRAME_BYTES, pts, 0)
                    pts += 20_000
                }
                while (true) {
                    val o = enc.dequeueOutputBuffer(info, 0)
                    if (o < 0) break
                    if (info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG == 0 && info.size > 0 && !muted) {
                        val b = ByteArray(info.size)
                        enc.getOutputBuffer(o)!!.apply { position(info.offset); get(b) }
                        runCatching { core.callAudio(id, b) }
                    }
                    enc.releaseOutputBuffer(o, false)
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "capture: $e")
        } finally {
            runCatching { rec.stop() }
            rec.release()
            aec?.release()
            ns?.release()
            runCatching { enc.stop() }
            enc.release()
        }
    }

    private fun newTrack(rate: Int, channels: Int): AudioTrack {
        val mask = if (channels >= 2) AudioFormat.CHANNEL_OUT_STEREO else AudioFormat.CHANNEL_OUT_MONO
        val min = AudioTrack.getMinBufferSize(rate, mask, AudioFormat.ENCODING_PCM_16BIT)
        val frame = rate / 50 * 2 * maxOf(1, minOf(channels, 2))
        return AudioTrack.Builder()
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .setAudioFormat(AudioFormat.Builder().setSampleRate(rate).setChannelMask(mask).setEncoding(AudioFormat.ENCODING_PCM_16BIT).build())
            .setBufferSizeInBytes(maxOf(min, frame * 4))
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
    }

    private fun playback() {
        // The decoder decides the PCM it produces: Android's Opus decoder always
        // outputs 48 kHz, whatever rate was encoded. The track must follow the
        // decoder's output format, or speech plays slow and low.
        var outRate = RATE
        var outChannels = 1
        var track = newTrack(outRate, outChannels)
        val dec = MediaCodec.createDecoderByType(mime(codec))
        try {
            dec.configure(format(), null, null, 0)
            dec.start()
            track.play()
            var silence = ByteArray(outRate / 50 * 2 * outChannels)
            val info = MediaCodec.BufferInfo()
            var pts = 0L
            while (running) {
                val r = core.pullCallAudio(id)
                var played = false
                if (r.isNotEmpty() && r[0].toInt() == 0 && r.size > 1) {
                    val i = dec.dequeueInputBuffer(10_000)
                    if (i >= 0) {
                        dec.getInputBuffer(i)!!.apply { clear(); put(r, 1, r.size - 1) }
                        dec.queueInputBuffer(i, 0, r.size - 1, pts, 0)
                        pts += 20_000
                    }
                }
                // Play everything the decoder has ready (it may hand out a frame
                // late, then two at once); wait a little only for the first.
                val deadline = System.nanoTime() + 15_000_000
                while (running) {
                    val o = dec.dequeueOutputBuffer(info, if (played) 0 else 5_000)
                    if (o == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                        val of = dec.outputFormat
                        val rate = if (of.containsKey(MediaFormat.KEY_SAMPLE_RATE)) of.getInteger(MediaFormat.KEY_SAMPLE_RATE) else outRate
                        val ch = if (of.containsKey(MediaFormat.KEY_CHANNEL_COUNT)) minOf(2, maxOf(1, of.getInteger(MediaFormat.KEY_CHANNEL_COUNT))) else outChannels
                        Log.i(TAG, "decoder output: $rate Hz, $ch ch")
                        if (rate != outRate || ch != outChannels) {
                            runCatching { track.stop() }
                            track.release()
                            outRate = rate
                            outChannels = ch
                            track = newTrack(outRate, outChannels)
                            track.play()
                            silence = ByteArray(outRate / 50 * 2 * outChannels)
                        }
                        continue
                    }
                    if (o < 0) {
                        if (played || System.nanoTime() >= deadline) break
                        continue
                    }
                    if (info.size > 0) {
                        val b = ByteArray(info.size)
                        dec.getOutputBuffer(o)!!.apply { position(info.offset); get(b) }
                        track.write(b, 0, b.size) // blocks: paces the loop
                        played = true
                    }
                    dec.releaseOutputBuffer(o, false)
                }
                if (!played) track.write(silence, 0, silence.size)
            }
        } catch (e: Exception) {
            Log.w(TAG, "playback: $e")
        } finally {
            runCatching { track.stop() }
            track.release()
            runCatching { dec.stop() }
            dec.release()
        }
    }

    companion object {
        private const val TAG = "CallAudio"
        const val OPUS = 1
        const val AMR_WB = 2
        private const val RATE = 16_000
        private const val FRAME_BYTES = RATE / 50 * 2 // 20 ms of 16-bit mono
        private const val PRE_SKIP = 312

        fun mime(codec: Int) = if (codec == OPUS) MediaFormat.MIMETYPE_AUDIO_OPUS else MediaFormat.MIMETYPE_AUDIO_AMR_WB

        private fun canEncode(mime: String): Boolean = runCatching {
            MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos.any { it.isEncoder && it.supportedTypes.any { t -> t.equals(mime, true) } }
        }.getOrDefault(false)

        /** The codecs this phone can encode and decode, best first ("1,2" or "2"). */
        fun codecs(): String =
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && canEncode(MediaFormat.MIMETYPE_AUDIO_OPUS)) "$OPUS,$AMR_WB" else "$AMR_WB"
    }
}
