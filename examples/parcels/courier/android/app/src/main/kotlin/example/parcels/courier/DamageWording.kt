// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.content.Context
import java.io.File
import java.io.IOException
import java.net.URL
import java.security.MessageDigest

/**
 * The damage a courier reports, written out for the damage report from their short note of it
 * ("corner crushed, wet"), by a small language model of the app's own (Gemma 3 270M)
 * that runs on the phone with MNN's LLM engine (MNN-LLM, the app's native code: src/main/cpp).
 * The model, a folder of files, is too large for the APK: the app downloads it into its own files
 * (files/models/damage-wording) the first time it needs it, each file checked against its
 * SHA-256. A model already there is the one it runs: the tests put a stub of it there before the
 * app starts, which answers their notes as its mappings say.
 */
class DamageWording(private val context: Context) {
    private val model: Long by lazy {
        val folder = File(context.filesDir, FOLDER)
        if (!File(folder, "config.json").exists()) download(folder)
        System.loadLibrary("damagewording")
        load(File(folder, "config.json").absolutePath).also { check(it != 0L) { "the damage wording's model does not load" } }
    }

    /** Downloads the model's files into [folder], each checked against its SHA-256: a partial or other file is not kept. */
    private fun download(folder: File) {
        folder.mkdirs()
        // config.json last: a folder of it is a model whole.
        for ((name, sha256) in FILES.entries.sortedBy { it.key == "config.json" }) {
            val to = File(folder, name)
            if (to.exists()) continue
            val part = File(to.path + ".part")
            val digest = MessageDigest.getInstance("SHA-256")
            URL(MODEL_URL + name).openStream().use { input ->
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
            if (sum != sha256) {
                part.delete()
                throw IOException("the damage wording's model's $name is not the one expected: SHA-256 $sum")
            }
            check(part.renameTo(to)) { "cannot keep the damage wording's model's $name" }
        }
    }

    /** The damage, written out from the courier's note of it: one model call at a time. */
    @Synchronized
    fun describe(note: String): String =
        String(respond(model, INSTRUCTIONS.toByteArray(), note.toByteArray()), Charsets.UTF_8).trim()

    private companion object {
        const val FOLDER = "models/damage-wording"
        const val INSTRUCTIONS =
            "A courier noted the damage to a parcel. Write it out in one plain sentence for the damage report."

        // Gemma 3 270M, instruction-tuned, as MNN publishes it for its LLM engine, at a commit, and
        // its files' SHA-256 sums.
        const val MODEL_URL = "https://huggingface.co/taobao-mnn/gemma-3-270m-it-MNN/resolve/9acdd6d40cb9e35c3d91cda2668d85d070b1d988/"
        val FILES = mapOf(
            "config.json" to "daf8aa56d276968f3642396baf81cf3163602cfb5a6d3b989935b7a24f242c7a",
            "llm_config.json" to "0a47a667bf518d76ccb498a49457691a4e219869160c52cee93697b28f653586",
            "tokenizer.mtok" to "6bffcbf66f8719677895ff20a579e00425c48939bde54e504784cd2187fa0d08",
            "llm.mnn" to "3dc85a4bf5dc866c160549a244c1306738baea35b5e57345730122f061688f14",
            "llm.mnn.weight" to "ff36f0357d29a18b8825b6f02beec99ded1549deee2ee384be15b13ce635c613",
        )

        @JvmStatic
        external fun load(config: String): Long

        @JvmStatic
        external fun respond(model: Long, instructions: ByteArray, note: ByteArray): ByteArray
    }
}
