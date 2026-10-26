// SPDX-License-Identifier: Apache-2.0

import java.net.URI
import java.security.MessageDigest
import javax.inject.Inject

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

/**
 * A model a release build ships, as its publisher publishes it, checked against its SHA-256. A
 * debug build ships a stub of it instead (src/debug/assets), which axx builds from the
 * acceptance project's models.
 */
abstract class ReleaseModel : DefaultTask() {
    @get:Input
    abstract val url: Property<String>

    @get:Input
    abstract val sha256: Property<String>

    @get:Input
    abstract val asset: Property<String>

    @get:OutputDirectory
    abstract val outputDir: DirectoryProperty

    @TaskAction
    fun fetch() {
        val model = URI(url.get()).toURL().readBytes()
        val sum = MessageDigest.getInstance("SHA-256").digest(model).joinToString("") { "%02x".format(it) }
        check(sum == sha256.get()) { "the model from ${url.get()} is not the one expected: SHA-256 $sum" }
        outputDir.get().file(asset.get()).asFile.writeBytes(model)
    }
}

/**
 * A model of the company's own a release build ships, from the path the build is given
 * (-P<property>=<path>). A debug build ships a stub of it instead (src/debug/assets).
 */
abstract class CompanyModel : DefaultTask() {
    @get:Input
    abstract val property: Property<String>

    @get:InputFile
    @get:Optional
    abstract val model: RegularFileProperty

    @get:Input
    abstract val asset: Property<String>

    @get:OutputDirectory
    abstract val outputDir: DirectoryProperty

    @TaskAction
    fun copy() {
        val file = model.orNull?.asFile
            ?: throw GradleException("a release build needs the company's model: -P${property.get()}=<${asset.get()}>")
        file.copyTo(outputDir.get().file(asset.get()).asFile, overwrite = true)
    }
}

/**
 * The source of a native library the app builds into its own native code, as its publisher
 * publishes it (a tarball of one folder), checked against its SHA-256.
 */
abstract class NativeSource : DefaultTask() {
    @get:Input
    abstract val url: Property<String>

    @get:Input
    abstract val sha256: Property<String>

    @get:OutputDirectory
    abstract val outputDir: DirectoryProperty

    @get:Inject
    abstract val archives: ArchiveOperations

    @get:Inject
    abstract val files: FileSystemOperations

    @TaskAction
    fun fetch() {
        val tarball = temporaryDir.resolve("source.tar.gz")
        URI(url.get()).toURL().openStream().use { input -> tarball.outputStream().use { input.copyTo(it) } }
        val sum = MessageDigest.getInstance("SHA-256").digest(tarball.readBytes()).joinToString("") { "%02x".format(it) }
        check(sum == sha256.get()) { "the source from ${url.get()} is not the one expected: SHA-256 $sum" }
        files.sync {
            from(archives.tarTree(tarball)) {
                // The tarball's folder's files, at the top.
                eachFile { relativePath = RelativePath(true, *relativePath.segments.drop(1).toTypedArray()) }
                // Not its Go files (flatbuffers' tests, in MNN's): they would be packages of the Go
                // module this app is in (axx's), which its builds and tests would build.
                exclude("**/*.go")
                includeEmptyDirs = false
            }
            into(outputDir)
        }
    }
}

// MNN, whose LLM engine runs the damage wording's model (src/main/cpp), as Alibaba publishes its
// source: its releases have no x86_64 build for Android, the emulators'.
val mnnSource = tasks.register<NativeSource>("mnnSource") {
    url.set("https://github.com/alibaba/MNN/archive/refs/tags/3.6.1.tar.gz")
    sha256.set("4b6065c4e2674318f5bf1dc75836ce4d30c17bfe598c4a1b11b7d0b2092b06e6")
    outputDir.set(layout.buildDirectory.dir("generated/mnn"))
}
tasks.configureEach { if (name.startsWith("configureCMake")) dependsOn(mnnSource) }

// The photo check's model: MobileNet v1, quantized, with its labels in its metadata, as
// TensorFlow publishes it (models/photo-check-android).
val photoCheckModel = tasks.register<ReleaseModel>("photoCheckModel") {
    url.set("https://storage.googleapis.com/download.tensorflow.org/models/tflite/task_library/image_classification/android/mobilenet_v1_1.0_224_quantized_1_metadata_1.tflite")
    sha256.set("d3949e8a3556c79739cb675e0be7476503bcce76938031c6a1048e13e0cb7d8b")
    asset.set("photo-check.tflite")
    outputDir.set(layout.buildDirectory.dir("generated/photo-check/assets"))
}

// The sounds at the door's model: YAMNet, of AudioSet's 521 sounds, with its labels in its
// metadata, as MediaPipe publishes it for its audio classifier (models/door-sounds-android).
val doorSoundsModel = tasks.register<ReleaseModel>("doorSoundsModel") {
    url.set("https://storage.googleapis.com/mediapipe-models/audio_classifier/yamnet/float32/1/yamnet.tflite")
    sha256.set("4d8b4a53282dc83ef04e3e7dbc4fbc98082e34e44ed798e16c3a0cdd4c584faf")
    asset.set("door-sounds.tflite")
    outputDir.set(layout.buildDirectory.dir("generated/door-sounds/assets"))
}

// The damage check's model: the company's own damage classifier, a PyTorch model saved for
// PyTorch Mobile (models/damage-check-android).
val damageCheckModel = tasks.register<CompanyModel>("damageCheckModel") {
    property.set("damageCheckModel")
    model.set(providers.gradleProperty("damageCheckModel").map { layout.projectDirectory.file(it) })
    asset.set("damage-check.ptl")
    outputDir.set(layout.buildDirectory.dir("generated/damage-check/assets"))
}

androidComponents {
    onVariants(selector().withBuildType("release")) { variant ->
        variant.sources.assets?.addGeneratedSourceDirectory(photoCheckModel, ReleaseModel::outputDir)
        variant.sources.assets?.addGeneratedSourceDirectory(doorSoundsModel, ReleaseModel::outputDir)
        variant.sources.assets?.addGeneratedSourceDirectory(damageCheckModel, CompanyModel::outputDir)
    }
}

android {
    namespace = "example.parcels.courier"
    compileSdk = 37
    ndkVersion = "28.2.13676358"

    defaultConfig {
        applicationId = "example.parcels.courier"
        minSdk = 26
        targetSdk = 37
        versionCode = 1
        versionName = "1.0"
        // The parcels service, at the address it has on the Mac: the tests give
        // the emulator the Mac's port as its own (host ports), as
        // `adb reverse tcp:8400 tcp:8400` does. A launch intent's api_url extra
        // overrides it.
        buildConfigField("String", "API_URL", "\"http://localhost:8400\"")
        // The delivery notes' language model (143 MB), which the app downloads into its own files
        // the first time it needs it, and checks against its SHA-256: SmolLM2-135M-Instruct, as
        // LiteRT's community publishes it for LiteRT-LM. A test puts its stub there first.
        buildConfigField("String", "DELIVERY_NOTE_MODEL_URL", "\"https://huggingface.co/litert-community/SmolLM2-135M-Instruct/resolve/8111e0a65fda719f0a6855e8e1a8ec8c3f9ccb22/SmolLM2_135M_Instruct.litertlm\"")
        buildConfigField("String", "DELIVERY_NOTE_MODEL_SHA256", "\"ccdc5c85735743f081b7d44ca309cab569f76c0f2f0e8e163449a63721969c37\"")
        externalNativeBuild {
            cmake {
                arguments += "-DMNN_SOURCE=${mnnSource.get().outputDir.get().asFile.path}"
                arguments += "-DANDROID_STL=c++_shared"
            }
        }
        // Phones' and emulators' ABIs, which the app's native code is built for (-Pabis=x86_64:
        // an x86-64 emulator's alone, as CI's are).
        ndk {
            abiFilters += providers.gradleProperty("abis").orElse("arm64-v8a,x86_64").get().split(",")
        }
    }

    externalNativeBuild {
        cmake {
            path = file("src/main/cpp/CMakeLists.txt")
            version = "3.22.1"
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    // ML Kit and MediaPipe map their models from the APK, which keeps them uncompressed.
    androidResources {
        noCompress += "tflite"
    }
}

dependencies {
    implementation(platform("androidx.compose:compose-bom:2026.06.01"))
    implementation("androidx.compose.material3:material3")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.10.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.11.0")
    implementation("androidx.work:work-runtime-ktx:2.10.0")
    implementation("com.google.mlkit:image-labeling-custom:17.0.3")
    implementation("com.google.mlkit:image-labeling:17.0.9")
    implementation("com.google.mlkit:text-recognition:16.0.1")
    implementation("com.google.mlkit:object-detection:17.0.2")
    implementation("com.google.mlkit:segmentation-selfie:16.0.0-beta6")
    implementation("com.google.mlkit:genai-prompt:1.0.0-beta4")
    implementation("com.google.mediapipe:tasks-audio:1.1.0")
    implementation("com.google.ai.edge.litertlm:litertlm-android:0.18.0")
    implementation("org.pytorch:pytorch_android_lite:2.1.0")
    implementation("org.pytorch:pytorch_android_torchvision_lite:2.1.0")
}
