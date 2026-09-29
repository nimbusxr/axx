// SPDX-License-Identifier: Apache-2.0

// Standalone build: the parcels couriers' Android app, not part of any root build.
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
        mavenCentral()
    }
}

rootProject.name = "parcels-courier"
include(":app")
