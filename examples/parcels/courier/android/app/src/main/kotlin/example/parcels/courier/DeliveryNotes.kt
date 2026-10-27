// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.content.Context
import com.google.ai.edge.litertlm.Backend
import com.google.ai.edge.litertlm.Content
import com.google.ai.edge.litertlm.Contents
import com.google.ai.edge.litertlm.ConversationConfig
import com.google.ai.edge.litertlm.Engine
import com.google.ai.edge.litertlm.EngineConfig
import com.google.mlkit.genai.common.FeatureStatus
import com.google.mlkit.genai.prompt.Generation
import kotlinx.coroutines.CancellationException
import java.io.File
import java.io.IOException
import java.net.URL
import java.security.MessageDigest

/**
 * The recipient's delivery note, as the courier reads it at the door: a short one as it is, a
 * long one summarized in a line. A phone with Gemini Nano (Android's AICore) summarizes it with
 * that, through ML Kit's Prompt API; any other, with a small language model of the app's own
 * (SmolLM2-135M-Instruct) that runs on the phone with LiteRT-LM. That model is too large for the
 * APK: the app downloads it into its own files (files/models) the first time it needs it, and
 * checks it against its SHA-256. A model already there is the one it runs: the tests put a stub
 * of it there before the app starts, which answers their notes as its mappings say.
 */
class DeliveryNotes(private val context: Context) {
    private val engine: Engine by lazy {
        val model = File(context.filesDir, MODEL)
        if (!model.exists()) download(model)
        Engine(EngineConfig(modelPath = model.absolutePath, backend = Backend.CPU(), cacheDir = context.cacheDir.absolutePath))
            .also { it.initialize() }
    }

    /** Downloads the model to [to], checked against its SHA-256: a partial or other file is not kept. */
    private fun download(to: File) {
        to.parentFile?.mkdirs()
        val part = File(to.path + ".part")
        val digest = MessageDigest.getInstance("SHA-256")
        URL(BuildConfig.DELIVERY_NOTE_MODEL_URL).openStream().use { input ->
            part.outputStream().use { output ->
                val buffer = ByteArray(1 shl 16)
                while (true) {
                    val n = input.read(buffer)
                    if (n < 0) break
                    digest.update(buffer, 0, n)
                    output.write(buffer, 0, n)
                }
            }
        }
        val sum = digest.digest().joinToString("") { "%02x".format(it) }
        if (sum != BuildConfig.DELIVERY_NOTE_MODEL_SHA256) {
            part.delete()
            throw IOException("the delivery notes' model is not the one expected: SHA-256 $sum")
        }
        check(part.renameTo(to)) { "cannot keep the delivery notes' model" }
    }

    /** The note for the courier: summarized when it is long, off the main thread. */
    suspend fun forCourier(note: String): String {
        if (note.length <= SHORT) return note
        return nano(note) ?: ownModel(note)
    }

    /** Gemini Nano's summary: null on a phone without it, or with it not downloaded yet. */
    private suspend fun nano(note: String): String? {
        val model = Generation.getClient()
        return try {
            if (model.checkStatus() != FeatureStatus.AVAILABLE) return null
            model.generateContent("$INSTRUCTION\n\n$note").candidates.firstOrNull()?.text?.trim()?.ifEmpty { null }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            null
        } finally {
            model.close()
        }
    }

    private fun ownModel(note: String): String {
        return engine.createConversation(ConversationConfig(systemInstruction = Contents.of(INSTRUCTION))).use { conversation ->
            val reply = conversation.sendMessage(note)
            reply.contents.contents.filterIsInstance<Content.Text>().joinToString("") { it.text }.trim()
        }
    }

    companion object {
        /** The model, in the app's files. */
        const val MODEL = "models/delivery-note.litertlm"

        /** The longest note the courier reads as it is. */
        const val SHORT = 80

        const val INSTRUCTION =
            "You summarize a recipient's delivery note for a parcel courier in one short line: " +
                "where to leave the parcel, and what to look out for."
    }
}
