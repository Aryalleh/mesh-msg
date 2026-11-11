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

    val Copy by lazy {
        icon("Copy", "M16,1H4c-1.1,0 -2,0.9 -2,2v14h2V3h12V1zM19,5H8c-1.1,0 -2,0.9 -2,2v14c0,1.1 0.9,2 2,2h11c1.1,0 2,-0.9 2,-2V7c0,-1.1 -0.9,-2 -2,-2zM19,21H8V7h11v14z")
    }
}
