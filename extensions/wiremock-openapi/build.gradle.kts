// SPDX-License-Identifier: Apache-2.0

import com.github.jengelman.gradle.plugins.shadow.relocation.SimpleRelocator

// WireMock extension that validates requests and stubbed responses against an OpenAPI
// specification. Standalone build: see settings.gradle.kts.
plugins {
    java
    id("com.gradleup.shadow") version "9.6.1"
}

group = "us.nimbusxr.axx"
description =
    "WireMock extension that validates requests and stubbed responses against an OpenAPI specification."

// Keep in sync with WIREMOCK_VERSION in the Dockerfile: the image runs the same WireMock
// version the extension compiles and tests against.
val wiremockVersion = "3.13.2"

java {
    // openapi-request-validator 3.x requires Java 21.
    toolchain {
        languageVersion = JavaLanguageVersion.of(21)
    }
}

repositories {
    mavenCentral()
}

dependencies {
    // WireMock itself is on the runtime classpath (wiremock-standalone), so it is not bundled.
    // Compiling against the standalone jar keeps the extension binary-compatible with it.
    compileOnly("org.wiremock:wiremock-standalone:$wiremockVersion")
    implementation("com.atlassian.oai:openapi-request-validator-core:3.0.0")

    testImplementation("org.wiremock:wiremock-standalone:$wiremockVersion")
    testImplementation(platform("org.junit:junit-bom:6.1.3"))
    testImplementation("org.junit.jupiter:junit-jupiter")
    testImplementation("org.assertj:assertj-core:3.27.7")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
    testRuntimeOnly("org.slf4j:slf4j-simple:2.0.20")
}

tasks.withType<JavaCompile>().configureEach {
    options.encoding = "UTF-8"
    options.compilerArgs.addAll(listOf("-Xlint:deprecation", "-Xlint:unchecked"))
}

// The fat jar that goes on WireMock's classpath: the extension plus the validator and its
// dependencies. WireMock is excluded (compileOnly).
tasks.shadowJar {
    archiveBaseName = "axx-wiremock-openapi"
    archiveClassifier = "all"

    // Merge META-INF/services files; for every other duplicate the first copy wins.
    duplicatesStrategy = DuplicatesStrategy.INCLUDE
    mergeServiceFiles()
    filesNotMatching("META-INF/services/**") {
        duplicatesStrategy = DuplicatesStrategy.EXCLUDE
    }
    failOnDuplicateEntries = true

    // wiremock-standalone relocates its own networknt json-schema-validator (1.x) classes but
    // leaves that library's message bundle at the classpath root as jsv-messages*.properties.
    // The validator uses networknt 2.x, which loads a bundle of the same name with different
    // placeholders, so whichever jar comes first on the classpath wins and messages degrade to
    // "id: required property '{1}' not found". Renaming our copy of the bundle (its resource
    // files and the base-name constant) makes the messages independent of classpath order.
    relocate(SimpleRelocator("^jsv-messages", "axx-wiremock-openapi-jsv-messages", rawString = true))

    manifest {
        attributes(
            "Implementation-Title" to "axx-wiremock-openapi",
            "Implementation-Version" to project.version,
            "Implementation-Vendor" to "NimbusXR",
        )
    }
}

tasks.jar {
    archiveBaseName = "axx-wiremock-openapi"
}

tasks.test {
    useJUnitPlatform()

    // Test the jar that ships, not the loose classes: wiremock-standalone first and the fat jar
    // after it, the same order as the Docker image's classpath.
    val shadowJar = tasks.shadowJar
    classpath =
        sourceSets.test.get().output +
        (configurations.testRuntimeClasspath.get() - configurations.runtimeClasspath.get()) +
        files(shadowJar)
}
