# Writes parcel-check.keras, a small model Keras saves (a Sequential
# classifier of 32×32 pictures), for keras_test.go: python make_keras.py,
# with Keras 3.
import os

os.environ["KERAS_BACKEND"] = "tensorflow"
import keras
from keras import layers

keras.utils.set_random_seed(7)
model = keras.Sequential(
    [
        keras.Input(shape=(32, 32, 3), name="pixels"),
        layers.Rescaling(1.0 / 255),
        layers.Conv2D(4, 3, activation="relu"),
        layers.GlobalAveragePooling2D(),
        layers.Dense(4, activation="softmax", name="scores"),
    ],
    name="parcel_check",
)
model.save("parcel-check.keras")
