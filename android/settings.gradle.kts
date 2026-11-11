pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        // Google's mirror of Maven Central first: Central itself rate-limits
        // (HTTP 429) large downloads from shared addresses.
        maven("https://maven-central.storage-download.googleapis.com/maven2/") { name = "MavenCentralMirror" }
        mavenCentral()
    }
}
rootProject.name = "mesh-msg"
include(":app")
