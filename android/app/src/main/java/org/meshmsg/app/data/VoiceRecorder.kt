package org.meshmsg.app.data

import android.content.Context
import android.media.MediaRecorder
import android.os.Build
import java.io.File

/** Records a voice message: AAC in .m4a, mono 16 kHz at 24 kbit/s (about 180 KB a minute). */
class VoiceRecorder private constructor(private val rec: MediaRecorder, val file: File) {
    val startedMs: Long = System.currentTimeMillis()

    /** Stops and returns the file, or null if nothing usable was recorded. */
    fun finish(): File? {
        val ok = runCatching { rec.stop() }.isSuccess
        rec.release()
        if (!ok || file.length() == 0L || System.currentTimeMillis() - startedMs < 700) {
            file.delete()
            return null
        }
        return file
    }

    fun cancel() {
        runCatching { rec.stop() }
        rec.release()
        file.delete()
    }

    companion object {
        const val MIME = "audio/mp4"
        const val MAX_MS = 5 * 60 * 1000

        fun start(ctx: Context): VoiceRecorder {
            val file = MediaFiles.newOutgoing(ctx, "voice", "m4a")
            @Suppress("DEPRECATION")
            val rec = if (Build.VERSION.SDK_INT >= 31) MediaRecorder(ctx) else MediaRecorder()
            try {
                rec.setAudioSource(MediaRecorder.AudioSource.MIC)
                rec.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
                rec.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
                rec.setAudioChannels(1)
                rec.setAudioSamplingRate(16_000)
                rec.setAudioEncodingBitRate(24_000)
                rec.setMaxDuration(MAX_MS)
                rec.setOutputFile(file.path)
                rec.prepare()
                rec.start()
            } catch (e: Exception) {
                rec.release()
                file.delete()
                throw e
            }
            return VoiceRecorder(rec, file)
        }
    }
}
