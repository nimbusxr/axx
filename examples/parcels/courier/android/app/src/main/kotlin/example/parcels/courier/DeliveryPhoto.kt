// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.graphics.Bitmap
import com.google.android.gms.tasks.Tasks
import com.google.mlkit.vision.common.InputImage
import com.google.mlkit.vision.label.ImageLabeling
import com.google.mlkit.vision.label.defaults.ImageLabelerOptions
import com.google.mlkit.vision.objects.ObjectDetection
import com.google.mlkit.vision.objects.defaults.ObjectDetectorOptions
import com.google.mlkit.vision.segmentation.Segmentation
import com.google.mlkit.vision.segmentation.selfie.SelfieSegmenterOptions
import com.google.mlkit.vision.text.TextRecognition
import com.google.mlkit.vision.text.latin.TextRecognizerOptions
import java.io.ByteArrayOutputStream

/** What a photo shows besides the parcel: the reference on a label the phone can read, and whether a dog is beside it. */
data class Seen(val reference: String?, val dog: Boolean)

/**
 * What a photo the courier attaches shows besides the parcel (PhotoCheck), as ML Kit's own models
 * see it on the phone: a page, like the recipient's signed note, and a dog beside the parcel (its
 * image labeling), the reference on a parcel's label (its text recognition), where the parcel is
 * (its object detection, of a photo's prominent object), and people (its selfie segmentation),
 * whom the app blurs out before the photo leaves the phone.
 */
class DeliveryPhoto {
    private val text = TextRecognition.getClient(TextRecognizerOptions.DEFAULT_OPTIONS)
    private val labels = ImageLabeling.getClient(ImageLabelerOptions.DEFAULT_OPTIONS)
    private val objects = ObjectDetection.getClient(
        ObjectDetectorOptions.Builder().setDetectorMode(ObjectDetectorOptions.SINGLE_IMAGE_MODE).build(),
    )
    private val people = Segmentation.getClient(
        SelfieSegmenterOptions.Builder()
            .setDetectorMode(SelfieSegmenterOptions.SINGLE_IMAGE_MODE)
            .enableRawSizeMask()
            .build(),
    )

    /** Whether the photo is of a page, like the recipient's signed note: off the main thread. */
    fun isDocument(photo: Bitmap): Boolean =
        Tasks.await(labels.process(InputImage.fromBitmap(photo, 0))).any { it.text in PAGES && it.confidence >= CERTAINTY }

    /** Looks at the photo: off the main thread, which ML Kit's tasks wait for. */
    fun look(photo: Bitmap): Seen {
        val image = InputImage.fromBitmap(photo, 0)
        val read = Tasks.await(text.process(image)).text
        val dog = Tasks.await(labels.process(image)).any { it.text == "Dog" && it.confidence >= CERTAINTY }
        return Seen(REFERENCE.find(read)?.value, dog)
    }

    /** Whether the parcel, the photo's prominent object, runs off the photo's edge: off the main thread. */
    fun cutOff(photo: Bitmap): Boolean {
        val box = Tasks.await(objects.process(InputImage.fromBitmap(photo, 0))).firstOrNull()?.boundingBox ?: return false
        val margin = photo.width / EDGE
        return box.left <= margin || box.top <= margin ||
            box.right >= photo.width - margin || box.bottom >= photo.height - margin
    }

    /** The photo as a JPEG, its people blurred out; null when nobody is in it. */
    fun blurringPeople(photo: Bitmap): ByteArray? {
        val mask = Tasks.await(people.process(InputImage.fromBitmap(photo, 0)))
        val confidence = mask.buffer.asFloatBuffer()
        val mw = mask.width
        val mh = mask.height
        val w = photo.width
        val h = photo.height
        // A blur of the photo: the photo at a sixteenth of its size, stretched back.
        val blur = Bitmap.createScaledBitmap(
            Bitmap.createScaledBitmap(photo, maxOf(1, w / 16), maxOf(1, h / 16), true), w, h, true,
        )
        val pixels = IntArray(w * h).also { photo.getPixels(it, 0, w, 0, 0, w, h) }
        val blurred = IntArray(w * h).also { blur.getPixels(it, 0, w, 0, 0, w, h) }
        var person = 0
        // The mask is smaller than the photo: stretched over it.
        for (y in 0 until h) {
            for (x in 0 until w) {
                if (confidence.get(y * mh / h * mw + x * mw / w) >= CERTAINTY) {
                    pixels[y * w + x] = blurred[y * w + x]
                    person++
                }
            }
        }
        if (person < w * h / SOMEONE) return null
        val out = Bitmap.createBitmap(pixels, w, h, Bitmap.Config.ARGB_8888)
        return ByteArrayOutputStream().also { out.compress(Bitmap.CompressFormat.JPEG, 90, it) }.toByteArray()
    }

    companion object {
        /** A parcel's reference, as its label has it: PX-MOB-9481. */
        private val REFERENCE = Regex("""PX(-[A-Z0-9]+)+""")

        /** What ML Kit's image labeling calls a photo of a page. */
        private val PAGES = setOf("Paper", "Receipt")

        /** How sure ML Kit must be of a dog or a page, or that a pixel is a person's. */
        private const val CERTAINTY = 0.5f

        /** How near the photo's edge a box runs off it: within one pixel in this many of its width. */
        private const val EDGE = 100

        /** The share of the photo a person takes up, at least: one pixel in this many. */
        private const val SOMEONE = 200
    }
}
