plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.roborazzi)
}

// The Go core (mobile/ → meshcore.aar) is built by scripts/build-android-core.sh.
val coreAar = file("libs/meshcore.aar")
if (!coreAar.exists()) {
    logger.warn("android/app/libs/meshcore.aar is missing: run scripts/build-android-core.sh first")
}

// Robolectric normally downloads its Android runtime with its own HTTP client,
// which fails behind proxies; Gradle fetches it instead and tests run offline.
val robolectricRuntime: Configuration by configurations.creating
val robolectricJars = layout.buildDirectory.dir("robolectric-jars")
val fetchRobolectricRuntime by tasks.registering(Copy::class) {
    from(robolectricRuntime)
    into(robolectricJars)
}

android {
    namespace = "org.meshmsg.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "org.meshmsg.app"
        minSdk = 26
        targetSdk = 35
        // Set by CI from the release tag (v0.1.0-alpha4 → 0.1.0-alpha4) and the
        // workflow run number, so every build installs as an update over the last.
        versionCode = (findProperty("versionCode") as String?)?.toInt() ?: 1
        versionName = (findProperty("versionName") as String?) ?: "0.1.0-dev"
        // Only what the UI is translated into.
        androidResources.localeFilters += listOf("en", "fa")
    }

    // A shared, public debug key (android/debug.keystore) so test APKs from CI
    // install over each other. It is NOT a release key: anyone can sign with it.
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
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // Unsigned: sign with your own key (docs/android.md, reproducible builds are M6).
        }
    }
    // One APK per CPU type plus a universal one: an arm64-only APK is about a
    // third of the size, which matters when APKs are passed phone to phone.
    splits {
        abi {
            isEnable = true
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = true
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { compose = true }
    packaging {
        resources.excludes += setOf("META-INF/{AL2.0,LGPL2.1}", "META-INF/*.kotlin_module")
    }
    testOptions {
        unitTests {
            isIncludeAndroidResources = true
            all {
                it.systemProperty("robolectric.graphicsMode", "NATIVE")
                it.systemProperty("robolectric.pixelCopyRenderMode", "hardware")
                it.maxHeapSize = "2g"
                it.dependsOn(fetchRobolectricRuntime)
                it.systemProperty("robolectric.offline", "true")
                it.systemProperty("robolectric.dependency.dir", robolectricJars.get().asFile.absolutePath)
            }
        }
    }
    lint {
        abortOnError = true
        warningsAsErrors = false
        disable += setOf("MissingTranslation") // checked the other way round: fa must cover en
    }
}

dependencies {
    implementation(files("libs/meshcore.aar"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.service)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.kotlinx.coroutines.android)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons.core)
    implementation(libs.zxing.core)
    debugImplementation(libs.compose.ui.tooling)
    debugImplementation(libs.compose.ui.test.manifest)

    // Android 14 (API 34) runtime for Robolectric: every test uses @Config(sdk = [34]).
    robolectricRuntime("org.robolectric:android-all-instrumented:14-robolectric-10818077-i7") { isTransitive = false }

    testImplementation(libs.junit)
    testImplementation(libs.robolectric)
    testImplementation(libs.roborazzi)
    testImplementation(libs.roborazzi.compose)
    testImplementation(libs.roborazzi.junit.rule)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation(libs.compose.ui.test.junit4)
}
