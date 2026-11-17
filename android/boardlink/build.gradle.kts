plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

// The board over USB, shared by the flasher and Mava Screen: USB serial
// drivers (ESP32-S3 USB, CP210x, CH340) and the node host link framing.
android {
    namespace = "org.meshmsg.boardlink"
    compileSdk = 35
    defaultConfig { minSdk = 26 }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    lint {
        abortOnError = true
        warningsAsErrors = false
    }
}

dependencies {
    testImplementation(libs.junit)
}
