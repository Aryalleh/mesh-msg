plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

// Mava board flasher for Android (docs/flashing.md): writes the board images over
// USB-OTG. The images come from the board build (scripts/build-flasher.sh writes
// dist/board-images/); pass -PboardImages=<dir>. Without them the app builds and
// tells the user this copy carries no firmware.
val boardImages = (findProperty("boardImages") as String?)?.let { file(it) }
    ?: rootProject.file("../dist/board-images")
val boardAssets = layout.buildDirectory.dir("generated/boardAssets")
val copyBoardImages by tasks.registering(Sync::class) {
    from(boardImages) { include("images.txt", "variants.txt", "VERSION", "*.bin") }
    into(boardAssets.map { it.dir("board") })
}

android {
    namespace = "org.meshmsg.flasher"
    compileSdk = 35

    defaultConfig {
        applicationId = "org.meshmsg.flasher"
        minSdk = 26
        targetSdk = 35
        versionCode = (findProperty("versionCode") as String?)?.toInt() ?: 1
        versionName = (findProperty("versionName") as String?) ?: "0.1.0-dev"
        androidResources.localeFilters += listOf("en", "fa")
    }
    sourceSets["main"].assets.srcDir(boardAssets)

    // The same public debug key as the app (android/debug.keystore): CI builds
    // install over each other.
    signingConfigs {
        getByName("debug") {
            storeFile = rootProject.file("debug.keystore")
            storePassword = "android"
            keyAlias = "androiddebugkey"
            keyPassword = "android"
        }
    }
    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { compose = true }
    lint {
        abortOnError = true
        warningsAsErrors = false
        disable += setOf("MissingTranslation")
    }
}

tasks.named("preBuild") { dependsOn(copyBoardImages) }

dependencies {
    implementation(project(":boardlink"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.kotlinx.coroutines.android)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)

    testImplementation(libs.junit)
}
