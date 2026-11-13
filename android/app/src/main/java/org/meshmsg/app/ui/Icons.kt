package org.meshmsg.app.ui

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.unit.dp

/**
 * The few Material icons that are not in material-icons-core, as vectors
 * (the extended icon library would add megabytes to an APK that people share
 * phone to phone). One family, 24 dp, filled paths — see android/DESIGN.md.
 */
object MeshIcons {
    private fun icon(name: String, path: String, autoMirror: Boolean = false): ImageVector =
        ImageVector.Builder(name = name, defaultWidth = 24.dp, defaultHeight = 24.dp, viewportWidth = 24f, viewportHeight = 24f, autoMirror = autoMirror)
            .addPath(pathData = addPathNodes(path), fill = SolidColor(Color.Black))
            .build()

    /** Queued on this phone. */
    val Schedule by lazy {
        icon("Schedule", "M11.99,2C6.47,2 2,6.48 2,12s4.47,10 9.99,10C17.52,22 22,17.52 22,12S17.52,2 11.99,2zM12,20c-4.42,0 -8,-3.58 -8,-8s3.58,-8 8,-8 8,3.58 8,8 -3.58,8 -8,8zM12.5,7H11v6l5.25,3.15 0.75,-1.23 -4.5,-2.67z")
    }

    /** Delivery receipt came back. */
    val DoneAll by lazy {
        icon("DoneAll", "M18,7l-1.41,-1.41 -6.34,6.34 1.41,1.41L18,7zM22.24,5.59L11.66,16.17 7.48,12l-1.41,1.41L11.66,19l12,-12 -1.42,-1.41zM0.41,13.41L6,19l1.41,-1.41L1.83,12 0.41,13.41z")
    }

    val Chat by lazy {
        icon("Chat", "M20,2H4c-1.1,0 -2,0.9 -2,2v18l4,-4h14c1.1,0 2,-0.9 2,-2V4c0,-1.1 -0.9,-2 -2,-2zM20,16H6l-2,2V4h16v12z", autoMirror = true)
    }

    /** Network tab: paths between nodes. */
    val Hub by lazy {
        icon("Hub", "M17,16l-4,-4V8.82C14.16,8.4 15,7.3 15,6c0,-1.66 -1.34,-3 -3,-3S9,4.34 9,6c0,1.3 0.84,2.4 2,2.82V12l-4,4H3v5h5v-3.05l4,-4.2 4,4.2V21h5v-5h-4z")
    }

    val Bluetooth by lazy {
        icon("Bluetooth", "M17.71,7.71L12,2h-1v7.59L6.41,5 5,6.41 10.59,12 5,17.59 6.41,19 11,14.41V22h1l5.71,-5.71 -4.3,-4.29 4.3,-4.29zM13,5.83l1.88,1.88L13,9.59V5.83zM14.88,16.29L13,18.17v-3.76l1.88,1.88z")
    }

    /** LoRa node. */
    val Antenna by lazy {
        icon("Antenna", "M12,5c-3.87,0 -7,3.13 -7,7h2c0,-2.76 2.24,-5 5,-5s5,2.24 5,5h2c0,-3.87 -3.13,-7 -7,-7zM13,14.29c0.88,-0.39 1.5,-1.26 1.5,-2.29 0,-1.38 -1.12,-2.5 -2.5,-2.5S9.5,10.62 9.5,12c0,1.02 0.62,1.9 1.5,2.29v3.3L7.59,21 9,22.41l3,-3 3,3L16.41,21 13,17.59v-3.3zM12,1C5.93,1 1,5.93 1,12h2c0,-4.97 4.03,-9 9,-9s9,4.03 9,9h2c0,-6.07 -4.93,-11 -11,-11z")
    }

    /** Internet relay. */
    val Globe by lazy {
        icon("Globe", "M12,2C6.48,2 2,6.48 2,12s4.48,10 10,10 10,-4.48 10,-10S17.52,2 12,2zM11,19.93c-3.95,-0.49 -7,-3.85 -7,-7.93 0,-0.62 0.08,-1.21 0.21,-1.79L9,15v1c0,1.1 0.9,2 2,2v1.93zM17.9,17.39c-0.26,-0.81 -1,-1.39 -1.9,-1.39h-1v-3c0,-0.55 -0.45,-1 -1,-1H8v-2h2c0.55,0 1,-0.45 1,-1V7h2c1.1,0 2,-0.9 2,-2v-0.41c2.93,1.19 5,4.06 5,7.41 0,2.08 -0.8,3.97 -2.1,5.39z")
    }

    val Mic by lazy {
        icon("Mic", "M12,14c1.66,0 2.99,-1.34 2.99,-3L15,5c0,-1.66 -1.34,-3 -3,-3S9,3.34 9,5v6c0,1.66 1.34,3 3,3zM17.3,11c0,3 -2.54,5.1 -5.3,5.1S6.7,14 6.7,11H5c0,3.41 2.72,6.23 6,6.72V21h2v-3.28c3.28,-0.48 6,-3.3 6,-6.72h-1.7z")
    }

    val MicOff by lazy {
        icon("MicOff", "M19,11h-1.7c0,0.74 -0.16,1.43 -0.43,2.05l1.23,1.23c0.56,-0.98 0.9,-2.09 0.9,-3.28zM14.98,11.17c0,-0.06 0.02,-0.11 0.02,-0.17L15,5c0,-1.66 -1.34,-3 -3,-3S9,3.34 9,5v0.18l5.98,5.99zM4.27,3L3,4.27l6.01,6.01L9.01,11c0,1.66 1.33,3 2.99,3 0.22,0 0.44,-0.03 0.65,-0.08l1.66,1.66c-0.71,0.33 -1.5,0.52 -2.31,0.52 -2.76,0 -5.3,-2.1 -5.3,-5.1L5,11c0,3.41 2.72,6.23 6,6.72L11,21h2v-3.28c0.91,-0.13 1.77,-0.45 2.54,-0.9L19.73,21 21,19.73 4.27,3z")
    }

    val CallEnd by lazy {
        icon("CallEnd", "M12,9c-1.6,0 -3.15,0.25 -4.6,0.72v3.1c0,0.39 -0.23,0.74 -0.56,0.9 -0.98,0.49 -1.87,1.12 -2.66,1.85 -0.18,0.18 -0.43,0.28 -0.7,0.28 -0.28,0 -0.53,-0.11 -0.71,-0.29L0.29,13.08c-0.18,-0.17 -0.29,-0.42 -0.29,-0.7 0,-0.28 0.11,-0.53 0.29,-0.71C3.34,8.78 7.46,7 12,7s8.66,1.78 11.71,4.67c0.18,0.18 0.29,0.43 0.29,0.71 0,0.28 -0.11,0.53 -0.29,0.71l-2.48,2.48c-0.18,0.18 -0.43,0.29 -0.71,0.29 -0.27,0 -0.52,-0.11 -0.7,-0.28 -0.79,-0.74 -1.69,-1.36 -2.67,-1.85 -0.33,-0.16 -0.56,-0.5 -0.56,-0.9v-3.1C15.15,9.25 13.6,9 12,9z")
    }

    val VolumeUp by lazy {
        icon("VolumeUp", "M3,9v6h4l5,5V4L7,9H3zM16.5,12c0,-1.77 -1.02,-3.29 -2.5,-4.03v8.05c1.48,-0.73 2.5,-2.25 2.5,-4.02zM14,3.23v2.06c2.89,0.86 5,3.54 5,6.71s-2.11,5.85 -5,6.71v2.06c4.01,-0.91 7,-4.49 7,-8.77s-2.99,-7.86 -7,-8.77z", autoMirror = true)
    }

    val Pause by lazy { icon("Pause", "M6,19h4V5H6v14zM14,5v14h4V5h-4z") }

    val AttachFile by lazy {
        icon("AttachFile", "M16.5,6v11.5c0,2.21 -1.79,4 -4,4s-4,-1.79 -4,-4V5c0,-1.38 1.12,-2.5 2.5,-2.5s2.5,1.12 2.5,2.5v10.5c0,0.55 -0.45,1 -1,1s-1,-0.45 -1,-1V6H10v9.5c0,1.38 1.12,2.5 2.5,2.5s2.5,-1.12 2.5,-2.5V5c0,-2.21 -1.79,-4 -4,-4S7,2.79 7,5v12.5c0,3.04 2.46,5.5 5.5,5.5s5.5,-2.46 5.5,-5.5V6h-1.5z")
    }

    val FileIcon by lazy {
        icon("File", "M6,2c-1.1,0 -1.99,0.9 -1.99,2L4,20c0,1.1 0.89,2 1.99,2H18c1.1,0 2,-0.9 2,-2V8l-6,-6H6zM13,9V3.5L18.5,9H13z")
    }

    val SwitchCamera by lazy {
        icon("SwitchCamera", "M20,4h-3.17L15,2H9L7.17,4H4c-1.1,0 -2,0.9 -2,2v12c0,1.1 0.9,2 2,2h16c1.1,0 2,-0.9 2,-2V6c0,-1.1 -0.9,-2 -2,-2zM15,15.5V13H9v2.5L5.5,12 9,8.5V11h6V8.5l3.5,3.5 -3.5,3.5z")
    }

    /** Round video message. */
    val RoundVideo by lazy {
        icon("RoundVideo", "M12,2C6.48,2 2,6.48 2,12s4.48,10 10,10 10,-4.48 10,-10S17.52,2 12,2zM12,20c-4.41,0 -8,-3.59 -8,-8s3.59,-8 8,-8 8,3.59 8,8 -3.59,8 -8,8zM10,8.5v7l5.5,-3.5z")
    }

    val Download by lazy { icon("Download", "M19,9h-4V3H9v6H5l7,7 7,-7zM5,18v2h14v-2H5z") }

    val VideocamOff by lazy {
        icon("VideocamOff", "M21,6.5l-4,4V7c0,-0.55 -0.45,-1 -1,-1H9.82L21,17.18V6.5zM3.27,2L2,3.27 4.73,6H4c-0.55,0 -1,0.45 -1,1v10c0,0.55 0.45,1 1,1h12c0.21,0 0.39,-0.08 0.54,-0.18L19.73,21 21,19.73 3.27,2z")
    }

    val Videocam by lazy {
        icon("Videocam", "M17,10.5V7c0,-0.55 -0.45,-1 -1,-1H4c-0.55,0 -1,0.45 -1,1v10c0,0.55 0.45,1 1,1h12c0.55,0 1,-0.45 1,-1v-3.5l4,4v-11l-4,4z")
    }

    val Copy by lazy {
        icon("Copy", "M16,1H4c-1.1,0 -2,0.9 -2,2v14h2V3h12V1zM19,5H8c-1.1,0 -2,0.9 -2,2v14c0,1.1 0.9,2 2,2h11c1.1,0 2,-0.9 2,-2V7c0,-1.1 -0.9,-2 -2,-2zM19,21H8V7h11v14z")
    }
}
