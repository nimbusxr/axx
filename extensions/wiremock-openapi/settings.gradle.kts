// SPDX-License-Identifier: Apache-2.0

// The plugins, and the libraries they use from Maven Central itself: the plugin portal's copy of
// Maven Central fails at times, and a build must not fail with it.
pluginManagement {
    repositories {
        gradlePluginPortal()
        mavenCentral()
    }
}

// Standalone build: this component is not part of any root build.
plugins {
    id("org.gradle.toolchains.foojay-resolver-convention") version "1.0.0"
}

rootProject.name = "axx-wiremock-openapi"
