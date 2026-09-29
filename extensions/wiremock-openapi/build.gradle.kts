// SPDX-License-Identifier: Apache-2.0

import com.github.jengelman.gradle.plugins.shadow.relocation.SimpleRelocator
import java.net.URI
import java.security.MessageDigest

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
    // The GraphQL mock: operations run against the mock's schema, and federation's _service and
    // _entities for subgraphs.
    implementation("com.graphql-java:graphql-java:26.0")
    implementation("com.apollographql.federation:federation-graphql-java-support:7.0.0")

    testImplementation("org.wiremock:wiremock-standalone:$wiremockVersion")
    // The providers' official SDKs read the model mock's answers in the tests, which point them at
    // WireMock: nothing reaches a provider.
    testImplementation("com.openai:openai-java:4.70.0")
    testImplementation("com.anthropic:anthropic-java:2.66.0")
    testImplementation("com.anthropic:anthropic-java-bedrock:2.66.0")
    testImplementation("com.anthropic:anthropic-java-vertex:2.66.0")
    testImplementation("com.google.genai:google-genai:1.73.0")
    testImplementation("software.amazon.awssdk:bedrockruntime:2.55.7")
    testImplementation("software.amazon.awssdk:netty-nio-client:2.55.7")
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

// The published contracts the model mock's answers are validated against in the tests, pinned by
// commit and checksum: OpenAI's OpenAPI document (openai/openai-openapi, in JSON: its YAML is
// longer than the YAML parser reads) and Ollama's (ollama/ollama, docs/openapi.yaml).
abstract class FetchContracts : DefaultTask() {
    /** A file's name, to its URL and its sha256, separated by a space. */
    @get:Input
    abstract val sources: MapProperty<String, String>

    @get:OutputDirectory
    abstract val into: DirectoryProperty

    @TaskAction
    fun fetch() {
        for ((name, source) in sources.get()) {
            val (url, sha256) = source.split(" ")
            val file = into.file(name).get().asFile
            if (file.exists() && digest(file.readBytes()) == sha256) {
                continue
            }
            val bytes = URI(url).toURL().openStream().use { it.readBytes() }
            check(digest(bytes) == sha256) { "$url does not have the sha256 $sha256: update the pin" }
            file.writeBytes(bytes)
        }
    }

    private fun digest(b: ByteArray) =
        MessageDigest.getInstance("SHA-256").digest(b).joinToString("") { "%02x".format(it) }
}

val fetchContracts by tasks.registering(FetchContracts::class) {
    sources.put(
        "openai.json",
        "https://raw.githubusercontent.com/openai/openai-openapi/b6059fc737ac846e8ba64ad65f4f2c8bc0d15854/openapi.json " +
            "b784832a852028d51a19092a600caf60a4924db4aa6b53b18c129fab4c82f351",
    )
    sources.put(
        "ollama.yaml",
        "https://raw.githubusercontent.com/ollama/ollama/a8aaf9fcfad23dc8d6f07b8109b1ba5ed310d76c/docs/openapi.yaml " +
            "988261d67db0389c9e6913ea6c10a6a7bf3c3e0ed76350898eefcbf7a0e2a8bc",
    )
    into = layout.buildDirectory.dir("contracts")
}

tasks.test {
    useJUnitPlatform()
    dependsOn(fetchContracts)
    val contracts = fetchContracts.flatMap { it.into }
    jvmArgumentProviders.add(CommandLineArgumentProvider { listOf("-Daxx.contracts=${contracts.get().asFile}") })

    // Test the jar that ships, not the loose classes: wiremock-standalone first and the fat jar
    // after it, the same order as the Docker image's classpath.
    val shadowJar = tasks.shadowJar
    classpath =
        sourceSets.test.get().output +
        (configurations.testRuntimeClasspath.get() - configurations.runtimeClasspath.get()) +
        files(shadowJar)
}
