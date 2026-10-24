package us.nimbusxr.axx.sdkprobe

import android.app.Activity
import android.os.Bundle
import android.graphics.BitmapFactory
import com.google.mlkit.vision.common.InputImage
import com.google.mlkit.vision.label.ImageLabeling
import com.google.mlkit.vision.label.defaults.ImageLabelerOptions
import com.google.mlkit.vision.text.TextRecognition
import com.google.mlkit.vision.text.latin.TextRecognizerOptions
import com.google.mlkit.vision.objects.ObjectDetection
import com.google.mlkit.vision.objects.defaults.ObjectDetectorOptions
import com.google.mlkit.vision.segmentation.Segmentation
import com.google.mlkit.vision.segmentation.selfie.SelfieSegmenterOptions
import com.google.mlkit.genai.prompt.Generation
import kotlinx.coroutines.*
import kotlinx.coroutines.tasks.await
import kotlinx.coroutines.flow.toList
import java.io.File

// Ordinary SDK calls in an independent app; no axx dependency or provider branch.
class Probe : Activity() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        scope.launch {
            val result = runCatching {
                val kind = intent.getStringExtra("kind") ?: "labels"
                if (kind == "nano" || kind == "nano-stream") {
                    val model = Generation.getClient()
                    val status = model.checkStatus()
                    val prompt = intent.getStringExtra("prompt") ?: "A parcel is waiting"
                    val answer = if (kind == "nano-stream") {
                        model.generateContentStream(prompt).toList().joinToString("") { it.candidates.first().text }
                    } else model.generateContent(prompt).candidates.first().text
                    model.close()
                    "status=$status $answer"
                } else {
                    val bitmap = BitmapFactory.decodeFile(File(filesDir, "photo.png").path)
                    val image = InputImage.fromBitmap(bitmap, 0)
                    when (kind) {
                        "labels" -> {
                            val detector = ImageLabeling.getClient(ImageLabelerOptions.DEFAULT_OPTIONS)
                            val labels = detector.process(image).await()
                            detector.close()
                            labels.joinToString(";") { "${it.text}:${it.confidence}:${it.index}" }
                        }
                        "text" -> {
                            val detector = TextRecognition.getClient(TextRecognizerOptions.DEFAULT_OPTIONS)
                            val text = detector.process(image).await()
                            detector.close()
                            "${text.text}|${text.textBlocks.first().lines.first().elements.first().text}"
                        }
                        "objects" -> {
                            val detector = ObjectDetection.getClient(ObjectDetectorOptions.Builder()
                                .setDetectorMode(ObjectDetectorOptions.SINGLE_IMAGE_MODE).enableClassification().build())
                            val objects = detector.process(image).await()
                            detector.close()
                            objects.joinToString(";") { "${it.boundingBox}:${it.labels.first().text}:${it.trackingId}" }
                        }
                        "mask" -> {
                            val detector = Segmentation.getClient(SelfieSegmenterOptions.Builder()
                                .setDetectorMode(SelfieSegmenterOptions.SINGLE_IMAGE_MODE).enableRawSizeMask().build())
                            val mask = detector.process(image).await()
                            detector.close()
                            "${mask.width}x${mask.height}:${mask.buffer.float}"
                        }
                        "mask-loop" -> {
                            val detector = Segmentation.getClient(SelfieSegmenterOptions.Builder()
                                .setDetectorMode(SelfieSegmenterOptions.SINGLE_IMAGE_MODE).enableRawSizeMask().build())
                            val mask = detector.process(image).await()
                            detector.close()
                            // An app reads a mask's size for each of its pixels, off the main thread.
                            val pixels = withContext(Dispatchers.Default) {
                                var n = 0L
                                repeat(2_000_000) { n += mask.width * mask.height }
                                n
                            }
                            "loop:$pixels"
                        }
                        else -> error("Unknown probe $kind")
                    }
                }
            }.fold({ it }, { "ERROR: ${it.stackTraceToString()}" })
            File(filesDir, "answer.txt").writeText(result)
        }
    }
    override fun onDestroy() { scope.cancel(); super.onDestroy() }
}
