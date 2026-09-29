// SPDX-License-Identifier: Apache-2.0

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "example.parcels.courier"
    compileSdk = 36

    defaultConfig {
        applicationId = "example.parcels.courier"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "1.0"
        // The parcels service as the emulator sees the host. A launch
        // intent's api_url extra overrides it.
        buildConfigField("String", "API_URL", "\"http://10.0.2.2:8400\"")
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation(platform("androidx.compose:compose-bom:2026.06.01"))
    implementation("androidx.compose.material3:material3")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.10.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.11.0")
}
