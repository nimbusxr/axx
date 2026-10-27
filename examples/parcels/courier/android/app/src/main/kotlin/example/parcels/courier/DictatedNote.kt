// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.launch

/** The phone's speech recognizer transcribes a courier's note in every build. */
@Composable
fun DictatedNote(reference: String, existing: String, save: suspend (String) -> Boolean) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var listening by remember(reference) { mutableStateOf(false) }
    var text by remember(reference) { mutableStateOf(existing) }
    var saved by remember(reference) { mutableStateOf(false) }
    var error by remember(reference) { mutableStateOf<String?>(null) }
    val recognizer = remember(reference) { SpeechRecognizer.createSpeechRecognizer(context) }
    DisposableEffect(recognizer) {
        recognizer.setRecognitionListener(object : RecognitionListener {
            override fun onReadyForSpeech(params: Bundle?) = Unit
            override fun onBeginningOfSpeech() = Unit
            override fun onRmsChanged(rmsdB: Float) = Unit
            override fun onBufferReceived(buffer: ByteArray?) = Unit
            override fun onEndOfSpeech() = Unit
            override fun onEvent(eventType: Int, params: Bundle?) = Unit
            override fun onPartialResults(results: Bundle?) {
                results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)?.firstOrNull()?.let { text = it }
            }
            override fun onResults(results: Bundle?) {
                listening = false
                val note = results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)?.firstOrNull()?.trim().orEmpty()
                text = note
                if (note.isNotEmpty()) scope.launch { saved = save(note) }
            }
            override fun onError(code: Int) {
                listening = false
                error = "The note could not be transcribed. Please try again."
            }
        })
        onDispose { recognizer.destroy() }
    }
    val start = {
        if (!SpeechRecognizer.isRecognitionAvailable(context)) {
            error = "Speech recognition is unavailable. Try again later."
        } else {
            error = null
            saved = false
            listening = true
            recognizer.startListening(Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
                putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, true)
            })
        }
    }
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) start() else error = "Allow the microphone to dictate a note."
    }
    TextButton(onClick = {
        if (listening) recognizer.stopListening()
        else if (context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) start()
        else permission.launch(Manifest.permission.RECORD_AUDIO)
    }) { Text(if (listening) "Finish dictation" else "Dictate note") }
    if (listening) Text("Listening…")
    if (text.isNotEmpty()) Text("Courier note: $text")
    if (saved) Text("Note saved")
    error?.let { Text(it) }
}
