// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Matrix
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageCapture
import androidx.camera.core.ImageCaptureException
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.core.UseCase
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.google.android.gms.tasks.Tasks
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import com.google.mlkit.vision.text.TextRecognition
import com.google.mlkit.vision.text.latin.TextRecognizerOptions
import java.io.ByteArrayOutputStream
import java.util.concurrent.Executors

/** A parcel's reference, as its label prints it. */
private val REFERENCE = Regex("PX-[A-Z]{3}-[0-9]{4}")

/**
 * The courier scans a parcel's label to open it: its QR code, or, where the code is torn or
 * smudged, the reference printed under it, which the camera reads as text.
 */
@Composable
fun LabelScannerScreen(message: String?, onFound: (reference: String) -> Unit, onCancel: () -> Unit) {
    val reader = remember { LabelReader() }
    DisposableEffect(reader) { onDispose { reader.close() } }
    CameraScreen(title = "Scan label", onCancel = onCancel) {
        BackCamera(Modifier.fillMaxSize()) { context ->
            val main = ContextCompat.getMainExecutor(context)
            val analysis = ImageAnalysis.Builder()
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .build()
            analysis.setAnalyzer(reader.executor) { frame ->
                reader.read(frame)?.let { reference -> main.execute { onFound(reference) } }
            }
            listOf(analysis)
        }
        Text(
            message ?: "Point the camera at the parcel's label",
            color = Color.White,
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .padding(bottom = 40.dp)
                .background(Color.Black.copy(alpha = 0.6f), RoundedCornerShape(12.dp))
                .padding(horizontal = 16.dp, vertical = 8.dp)
                .semantics { liveRegion = LiveRegionMode.Polite },
        )
    }
}

/**
 * Reads a label in the camera's frames, on the phone: its QR code with ML Kit's barcode scanning,
 * and the printed reference with its text recognition. ML Kit takes each frame as a bitmap,
 * turned as the camera says.
 */
private class LabelReader {
    val executor = Executors.newSingleThreadExecutor()
    private val codes = BarcodeScanning.getClient(BarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build())
    private val text = TextRecognition.getClient(TextRecognizerOptions.DEFAULT_OPTIONS)
    private var lastFound: String? = null

    /** The reference the frame shows, the first time it shows one; null otherwise. */
    fun read(frame: ImageProxy): String? {
        val image = frame.use { InputImage.fromBitmap(it.toBitmap(), it.imageInfo.rotationDegrees) }
        val reference = runCatching {
            Tasks.await(codes.process(image)).firstNotNullOfOrNull { code -> code.rawValue?.let { REFERENCE.find(it)?.value } }
                ?: REFERENCE.find(Tasks.await(text.process(image)).text)?.value
        }.getOrNull() ?: return null
        if (reference == lastFound) return null
        lastFound = reference
        return reference
    }

    fun close() {
        executor.shutdown()
        codes.close()
        text.close()
    }
}

/**
 * The courier photographs the parcel where they left it, as proof of delivery: the photo comes
 * upright, as the phone is held, and is attached to the delivery as photo-at-the-door.jpg.
 */
@Composable
fun DoorPhotoScreen(onTaken: (jpeg: ByteArray) -> Unit, onCancel: () -> Unit) {
    val capture = remember { ImageCapture.Builder().build() }
    // A tap before the camera is ready takes the photo as soon as it is, as a phone's camera does.
    var bound by remember { mutableStateOf(false) }
    var taking by remember { mutableStateOf(false) }
    val context = LocalContext.current
    val take = {
        capture.takePicture(
            ContextCompat.getMainExecutor(context),
            object : ImageCapture.OnImageCapturedCallback() {
                override fun onCaptureSuccess(image: ImageProxy) {
                    val jpeg = image.use { uprightJpeg(it.toBitmap(), it.imageInfo.rotationDegrees) }
                    onTaken(jpeg)
                }

                override fun onError(exception: ImageCaptureException) {
                    taking = false
                }
            },
        )
    }
    CameraScreen(title = "Photo at the door", onCancel = onCancel) {
        BackCamera(Modifier.fillMaxSize(), onBound = {
            bound = true
            if (taking) take()
        }) { listOf(capture) }
        Button(
            onClick = {
                taking = true
                if (bound) take()
            },
            enabled = !taking,
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .padding(bottom = 40.dp),
        ) {
            Text("Take photo")
        }
    }
}

/** A photo turned upright, as the phone was held, as a JPEG. */
private fun uprightJpeg(photo: Bitmap, rotationDegrees: Int): ByteArray {
    val upright = if (rotationDegrees == 0) {
        photo
    } else {
        Bitmap.createBitmap(photo, 0, 0, photo.width, photo.height, Matrix().apply { postRotate(rotationDegrees.toFloat()) }, true)
    }
    return ByteArrayOutputStream().use { out ->
        upright.compress(Bitmap.CompressFormat.JPEG, 90, out)
        out.toByteArray()
    }
}

/** A screen of the camera: its title and a way back above it, what it shows over the camera. */
@Composable
private fun CameraScreen(title: String, onCancel: () -> Unit, content: @Composable androidx.compose.foundation.layout.BoxScope.() -> Unit) {
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(start = 24.dp, end = 12.dp, top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(title, style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
            TextButton(onClick = onCancel) { Text("Cancel") }
        }
        Box(Modifier.fillMaxSize().background(Color.Black), content = content)
    }
}

/**
 * The phone's back camera, shown, with the use cases the screen adds (an analysis of its frames,
 * a photo): asked for leave to use it first, if the courier has not given it.
 */
@Composable
private fun BackCamera(modifier: Modifier, onBound: () -> Unit = {}, useCases: (android.content.Context) -> List<UseCase>) {
    val context = LocalContext.current
    val owner = LocalLifecycleOwner.current
    var allowed by remember {
        mutableStateOf(ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED)
    }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { allowed = it }
    LaunchedEffect(Unit) { if (!allowed) ask.launch(Manifest.permission.CAMERA) }
    if (!allowed) return
    val providing = remember { ProcessCameraProvider.getInstance(context) }
    // The camera is the screen's while it shows: bound to the activity, it is let go as the screen goes.
    DisposableEffect(providing) { onDispose { providing.addListener({ providing.get().unbindAll() }, ContextCompat.getMainExecutor(context)) } }
    AndroidView(
        modifier = modifier,
        factory = { ctx ->
            val view = PreviewView(ctx).apply { scaleType = PreviewView.ScaleType.FILL_CENTER }
            providing.addListener({
                val provider = providing.get()
                val preview = Preview.Builder().build().also { it.surfaceProvider = view.surfaceProvider }
                provider.unbindAll()
                provider.bindToLifecycle(owner, CameraSelector.DEFAULT_BACK_CAMERA, preview, *useCases(ctx).toTypedArray())
                onBound()
            }, ContextCompat.getMainExecutor(ctx))
            view
        },
    )
}
