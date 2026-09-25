// SPDX-License-Identifier: Apache-2.0

// Standalone build: the IntelliJ Platform Gradle Plugin downloads a full IDE distribution, too
// heavy to share a build with anything else.
plugins {
    id("org.gradle.toolchains.foojay-resolver-convention") version "1.0.0"
}

rootProject.name = "axx-intellij"
