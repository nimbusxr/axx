// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.content.Context
import android.graphics.Bitmap
import kotlin.math.exp
import org.pytorch.IValue
import org.pytorch.LiteModuleLoader
import org.pytorch.Module
import org.pytorch.torchvision.TensorImageUtils

/**
 * The damage check: a photo the courier reports damage with must show it. The company's own
 * damage classifier (PyTorch, saved for PyTorch Mobile: damage-check.ptl in the app's assets)
 * runs on the phone with PyTorch Android Lite, of the photo stretched to its 224 by 224 pixels.
 * The debug build ships a stub of it (src/debug/assets), which answers the tests' photos as its
 * mappings say.
 */
class DamageCheck(context: Context) {
    private val module: Module = LiteModuleLoader.loadModuleFromAsset(context.assets, MODEL)

    /** Whether the photo shows a damaged parcel: off the main thread. */
    fun damaged(photo: Bitmap): Boolean {
        val input = TensorImageUtils.bitmapToFloat32Tensor(
            Bitmap.createScaledBitmap(photo, SIZE, SIZE, true),
            TensorImageUtils.TORCHVISION_NORM_MEAN_RGB,
            TensorImageUtils.TORCHVISION_NORM_STD_RGB,
        )
        // Its logits of intact and damaged.
        val logits = module.forward(IValue.from(input)).toTensor().dataAsFloatArray
        return 1 / (1 + exp(logits[0] - logits[1])) >= CERTAINTY
    }

    companion object {
        const val MODEL = "damage-check.ptl"
        private const val SIZE = 224

        /** How likely damage must be. */
        private const val CERTAINTY = 0.5f
    }
}
