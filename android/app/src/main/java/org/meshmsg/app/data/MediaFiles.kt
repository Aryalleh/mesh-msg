package org.meshmsg.app.data

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.ExifInterface
import android.net.Uri
import android.provider.OpenableColumns
import androidx.core.content.FileProvider
import java.io.File

/** Files the app sends and opens: copies from pickers, photo shrinking, share URIs. */
object MediaFiles {
    /** Largest file the core takes. */
    const val MAX_BYTES = 64L shl 20
    /** Photos are shrunk to this many pixels on the long side. */
    private const val PHOTO_MAX_PX = 1600

    data class Picked(val file: File, val mime: String, val name: String)

    fun outgoingDir(ctx: Context): File = File(ctx.cacheDir, "outgoing").apply { mkdirs() }

    /** A new file for the camera or the voice recorder to write into. */
    fun newOutgoing(ctx: Context, prefix: String, ext: String): File =
        File(outgoingDir(ctx), "$prefix-${System.currentTimeMillis()}.$ext")

    fun uriFor(ctx: Context, file: File): Uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".files", file)

    /** Copies what a picker returned into the app's cache; photos are shrunk. */
    fun copyIn(ctx: Context, uri: Uri): Picked {
        val cr = ctx.contentResolver
        var name = "file"
        var size = -1L
        cr.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                c.getString(0)?.let { name = it }
                if (!c.isNull(1)) size = c.getLong(1)
            }
        }
        if (size > MAX_BYTES) throw IllegalArgumentException("File larger than 64 MB")
        val mime = cr.getType(uri) ?: "application/octet-stream"
        val out = File(outgoingDir(ctx), "in-${System.currentTimeMillis()}")
        cr.openInputStream(uri)?.use { input ->
            out.outputStream().use { o ->
                val buf = ByteArray(64 shl 10)
                var total = 0L
                while (true) {
                    val n = input.read(buf)
                    if (n < 0) break
                    total += n
                    if (total > MAX_BYTES) throw IllegalArgumentException("File larger than 64 MB")
                    o.write(buf, 0, n)
                }
            }
        } ?: throw IllegalArgumentException("Can't read this file")
        if (mime == "image/jpeg" || mime == "image/png" || mime == "image/webp" || mime == "image/heic" || mime == "image/heif") {
            shrinkPhoto(out)?.let { return Picked(it, "image/jpeg", name.substringBeforeLast('.') + ".jpg") }
        }
        return Picked(out, mime, name)
    }

    /** Re-encodes a photo as a ≤1600 px JPEG, upright; null if it can't (then the original goes). */
    private fun shrinkPhoto(f: File): File? = runCatching { shrink(f) }.getOrNull()

    private fun shrink(f: File): File? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(f.path, bounds)
        if (bounds.outWidth <= 0) return null
        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= PHOTO_MAX_PX) sample *= 2
        val bmp = BitmapFactory.decodeFile(f.path, BitmapFactory.Options().apply { inSampleSize = sample }) ?: return null
        val scale = PHOTO_MAX_PX.toFloat() / maxOf(bmp.width, bmp.height)
        val m = Matrix()
        if (scale < 1f) m.postScale(scale, scale)
        val rot = when (ExifInterface(f.path).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)) {
            ExifInterface.ORIENTATION_ROTATE_90 -> 90f
            ExifInterface.ORIENTATION_ROTATE_180 -> 180f
            ExifInterface.ORIENTATION_ROTATE_270 -> 270f
            else -> 0f
        }
        m.postRotate(rot)
        val out = Bitmap.createBitmap(bmp, 0, 0, bmp.width, bmp.height, m, true)
        val dst = File(f.path + ".jpg")
        dst.outputStream().use { out.compress(Bitmap.CompressFormat.JPEG, 82, it) }
        if (dst.length() >= f.length()) { dst.delete(); return null }
        f.delete()
        return dst
    }

    /** A small bitmap of a photo for the chat bubble. */
    fun thumbnail(path: String, maxPx: Int = 720): Bitmap? = runCatching {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(path, bounds)
        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= maxPx) sample *= 2
        BitmapFactory.decodeFile(path, BitmapFactory.Options().apply { inSampleSize = sample })
    }.getOrNull()

    /** Opens a received file in another app (viewer, player). */
    fun viewIntent(ctx: Context, path: String, mime: String): Intent =
        Intent(Intent.ACTION_VIEW).setDataAndType(uriFor(ctx, File(path)), mime.ifBlank { "*/*" })
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
}
