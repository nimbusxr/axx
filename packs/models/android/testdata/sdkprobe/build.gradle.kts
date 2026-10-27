plugins { id("com.android.application") version "9.4.1" }

android {
    namespace = "us.nimbusxr.axx.sdkprobe"
    compileSdk = 37
    defaultConfig {
        applicationId = "us.nimbusxr.axx.sdkprobe"
        minSdk = 26
        targetSdk = 37
        versionCode = 1
        versionName = "1"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}
dependencies {
    implementation("com.google.mlkit:image-labeling:17.0.9")
    implementation("com.google.mlkit:text-recognition:16.0.1")
    implementation("com.google.mlkit:object-detection:17.0.2")
    implementation("com.google.mlkit:segmentation-selfie:16.0.0-beta6")
    implementation("com.google.mlkit:genai-prompt:1.0.0-beta5")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.11.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-play-services:1.11.0")
}
