// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.graphics.Bitmap
import com.google.android.gms.tasks.Tasks
import com.google.mlkit.common.model.LocalModel
import com.google.mlkit.vision.common.InputImage
import com.google.mlkit.vision.label.ImageLabeling
import com.google.mlkit.vision.label.custom.CustomImageLabelerOptions

/** What the photo check saw: the likeliest class, how likely, and whether it is a parcel's. */
data class Verdict(val label: String, val score: Float, val parcel: Boolean)

/**
 * The photo check: a photo the courier attaches must show the parcel. ML Kit's image labeling
 * runs the app's own image classifier (MobileNet, quantized: photo-check.tflite in its assets),
 * which carries its labels in its metadata. The debug build ships a stub of it
 * (src/debug/assets), which answers the tests' photos as its mappings say.
 */
class PhotoCheck {
    private val labeler = ImageLabeling.getClient(
        CustomImageLabelerOptions.Builder(LocalModel.Builder().setAssetFilePath(MODEL).build())
            .setConfidenceThreshold(0f)
            .setMaxResultCount(1)
            .build(),
    )

    /** Labels the photo: off the main thread, which ML Kit's task waits for. */
    fun check(photo: Bitmap): Verdict {
        val label = Tasks.await(labeler.process(InputImage.fromBitmap(photo, 0))).maxByOrNull { it.confidence }
            ?: return Verdict("nothing", 0f, false)
        return Verdict(label.text, label.confidence, label.text in PARCELS && label.confidence >= CERTAINTY)
    }

    companion object {
        const val MODEL = "photo-check.tflite"

        /** The model's classes that are parcels. */
        private val PARCELS = setOf("carton", "crate", "envelope", "mailbag", "packet")

        /** How likely a parcel's class must be. */
        private const val CERTAINTY = 0.5f
    }
}
