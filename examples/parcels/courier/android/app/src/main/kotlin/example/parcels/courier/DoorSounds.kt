// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import com.google.mediapipe.tasks.audio.audioclassifier.AudioClassifier
import com.google.mediapipe.tasks.audio.core.RunningMode
import com.google.mediapipe.tasks.components.containers.AudioData
import com.google.mediapipe.tasks.core.BaseOptions
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * The sounds at the door: when no one answers, the courier taps "No one home" and rings or knocks,
 * and the phone listens for the doorbell or the knock, which is recorded with the delivery as proof
 * of the attempt. MediaPipe's audio classifier runs YAMNet (door-sounds.tflite in the app's assets,
 * of AudioSet's 521 sounds) on the microphone's sound, a window of a second at a time, half a
 * window apart. The debug build ships a stub of it (src/debug/assets), which answers the tests'
 * recordings as its mappings say.
 */
object DoorSounds {
    const val MODEL = "door-sounds.tflite"

    /** YAMNet's sounds an attempt is, by what the attempt records. */
    private val SOUNDS = mapOf("Doorbell" to "doorbell", "Ding-dong" to "doorbell", "Knock" to "knock")

    /** How sure YAMNet must be of a doorbell or a knock. */
    private const val CERTAINTY = 0.5f

    private const val RATE = 16000

    /** YAMNet's window: 0.975 seconds. */
    private const val WINDOW = 15600
    private const val HOP = WINDOW / 2

    /** Listens until a doorbell or a knock, and says which: "doorbell" or "knock". */
    @SuppressLint("MissingPermission") // The caller has the microphone's.
    suspend fun listen(context: Context): String = withContext(Dispatchers.Default) {
        val classifier = AudioClassifier.createFromOptions(
            context,
            AudioClassifier.AudioClassifierOptions.builder()
                .setBaseOptions(BaseOptions.builder().setModelAssetPath(MODEL).build())
                .setRunningMode(RunningMode.AUDIO_CLIPS)
                .setMaxResults(1)
                .build(),
        )
        val format = AudioFormat.ENCODING_PCM_FLOAT
        val record = AudioRecord(
            MediaRecorder.AudioSource.MIC, RATE, AudioFormat.CHANNEL_IN_MONO, format,
            maxOf(AudioRecord.getMinBufferSize(RATE, AudioFormat.CHANNEL_IN_MONO, format), WINDOW * Float.SIZE_BYTES),
        )
        val window = FloatArray(WINDOW)
        val clip = AudioData.create(AudioData.AudioDataFormat.builder().setNumOfChannels(1).setSampleRate(RATE.toFloat()).build(), WINDOW)
        var heard: String? = null
        try {
            record.startRecording()
            while (heard == null) {
                // The window's latter half, then half a window more of the microphone's sound.
                window.copyInto(window, 0, HOP, WINDOW)
                var read = 0
                while (read < HOP) {
                    val n = record.read(window, WINDOW - HOP + read, HOP - read, AudioRecord.READ_BLOCKING)
                    check(n >= 0) { "the microphone could not be read: $n" }
                    read += n
                }
                ensureActive()
                clip.load(window)
                val top = classifier.classify(clip).classificationResults().firstOrNull()
                    ?.classifications()?.firstOrNull()?.categories()?.firstOrNull()
                if (top != null && top.score() >= CERTAINTY) heard = SOUNDS[top.categoryName()]
            }
        } finally {
            record.stop()
            record.release()
            classifier.close()
        }
        heard
    }
}

/** No one home: the attempt, recorded with what the phone heard at the door. */
@Composable
fun NoOneHome(reference: String, record: suspend (heard: String) -> Boolean) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var listening by remember(reference) { mutableStateOf<Job?>(null) }
    var recorded by remember(reference) { mutableStateOf<String?>(null) }
    var error by remember(reference) { mutableStateOf<String?>(null) }
    val start = {
        error = null
        listening = scope.launch {
            try {
                val heard = DoorSounds.listen(context)
                if (record(heard)) recorded = if (heard == "doorbell") "the doorbell rang" else "a knock at the door"
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                error = "The microphone could not start. Please try again."
            } finally {
                listening = null
            }
        }
    }
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) start() else error = "Allow the microphone to record the attempt."
    }
    TextButton(onClick = {
        val job = listening
        if (job != null) job.cancel()
        else if (context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) start()
        else permission.launch(Manifest.permission.RECORD_AUDIO)
    }) { Text(if (listening != null) "Stop listening" else "No one home") }
    if (listening != null) Text("Listening for the doorbell or a knock…")
    recorded?.let { Text("Attempt recorded: $it") }
    error?.let { Text(it) }
}
