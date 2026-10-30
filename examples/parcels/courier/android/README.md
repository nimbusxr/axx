# Parcels Courier (Android)

The handheld a courier of the parcels service delivers with. The courier signs in with their
courier ID and PIN, sees the parcels out for delivery with them today, and marks each one
delivered with the name of whoever signed for it. The service's courier endpoints
(`../../app/courierapp.go`) back it.

## Build

```sh
./gradlew assembleDebug
```

The APK lands in `app/build/outputs/apk/debug/app-debug.apk`. The build needs a JDK (17 or
later) and the Android SDK (`ANDROID_HOME`, or `sdk.dir` in `local.properties`), with the NDK and
CMake the app's native code is built with (`ndkVersion` and `externalNativeBuild` in
`app/build.gradle.kts`).

## The photo check

A photo the courier attaches must show the parcel: an image classifier (MobileNet, quantized)
checks it on the phone, with ML Kit's image labeling (its custom model: the app's own), before
the app uploads it (`PhotoCheck.kt`), and ML Kit reads its labels from the model's metadata. A
release build fetches the model from TensorFlow, checked against its SHA-256. The debug build,
which the tests install, ships a stub of it instead (`app/src/debug/assets/photo-check.tflite`,
38 KB to the model's 4.3 MB): a model of the same inputs, outputs and metadata, whose answers
are the mappings of the acceptance project's `models/photo-check-android`. `go generate ./...`,
in axx's repository, builds it again.

The app also looks at the photo with ML Kit's own models (`DeliveryPhoto.kt`). A photo of a page
(image labeling: paper, a receipt), like the recipient's signed note, is a document, and needs
no parcel. In a photo of the parcel, a parcel's label it can read (text recognition) must be the
delivery's, a dog (image labeling) must not be beside the parcel, the parcel must not run off
the photo's edge (object detection, of the photo's prominent object: a box that reaches within
1% of the photo's edge is cut off), and the people in it (selfie segmentation) are blurred out
before the photo leaves the phone. These models come with ML Kit, in no file of the app's: the
tests put stubs of their APIs in the app's `axx-models` folder
(`acceptance/features/courier-app-documents/models`, of the acceptance project's
`models/photo-*-android`), which the `models-android` pack answers ML Kit's clients from.

## The damage check

A parcel that comes to the courier damaged is reported with a photo of the damage ("Report
damage"): the app attaches it to the delivery as `damage-<the photo's name>` once the company's
own damage classifier sees damage in it (`DamageCheck.kt`). The classifier is a PyTorch model
saved for PyTorch Mobile (`.ptl`), which PyTorch Android Lite runs on the phone, of the photo
stretched to 224 by 224 pixels and normalized as torchvision's models' photos are. It gives the
logits of `intact` and `damaged`, and damage it is 50% sure of is damage. A release build ships
the company's model, from the path its build gives:

```sh
./gradlew assembleRelease -PdamageCheckModel=<damage-check.ptl>
```

The debug build ships a stub of it instead (`app/src/debug/assets/damage-check.ptl`, 14 KB),
whose answers are the mappings of the acceptance project's `models/damage-check-android`: the
scenarios' photo of a crushed parcel
(`acceptance/features/courier-app-documents/documents/crushed-parcel.jpg`) to `damaged`, and
any other photo to `intact`. The stub uses a few operators the company's model does not
(`axx models build -v` names them); PyTorch Android Lite's runtime has them all.

## The damage wording

The courier can also describe the damage in a few words ("Describe damage": "corner crushed,
wet"), and the app writes them out in a sentence for the damage report, which it attaches to the
delivery as `damage-description.txt` (`DamageWording.kt`). It does so with a small language
model (Gemma 3 270M, instruction-tuned, as MNN publishes it) that MNN's LLM engine runs on the
phone, from the app's native code (`app/src/main/cpp`). MNN's releases for Android are of ARM's
ABIs alone, and emulators are x86_64: the Gradle build fetches MNN's source (3.6.1), checked
against its SHA-256, and builds it with the app's native code, for `arm64-v8a` and `x86_64`.

The model, a folder of files (178 MB), is too large for the APK: the app downloads it from
Hugging Face into its own files (`files/models/damage-wording`) the first time it needs it, each
file checked against its SHA-256, and runs the one it finds there. The tests put a stub of it
there before the app starts (`acceptance/features/courier-app-documents/models/damage-wording`,
21 MB, half of it the real tokenizer: the stub's graph and embeddings, the real `config.json`
and tokenizer, and an `llm_config.json` of the real one's chat template), whose answers are the
mappings of the acceptance project's `models/damage-wording-android`, with a folder of the app's
and the files pack's copy step.

## The delivery notes

A recipient can leave the courier a note, when the shop registers the parcel. The app shows a
short one as it is, and summarizes a long one in a line (`DeliveryNotes.kt`): with Gemini Nano,
on a phone that has it (Android's AICore, through ML Kit's Prompt API), and on any other with a
small language model (SmolLM2-135M-Instruct) it runs on the phone with LiteRT-LM. An emulator
has no Gemini Nano: a test that wants it puts a stub of its API in the app's `axx-models` folder
(`acceptance/features/courier-app/models/delivery-note-nano.axxmodel`, of the acceptance
project's `models/delivery-note-nano`), which the `models-android` pack answers the Prompt API
from. The app's own model (`.litertlm`, 143 MB) is too large for the APK: the app downloads it
from LiteRT's community on Hugging Face into its own files (`files/models`) the first time it
needs it, checked against its SHA-256, and runs the one it finds there. The tests put a stub of
it there before the app starts (`acceptance/features/courier-app/models/delivery-note.litertlm`,
709 KB: a model of the real one's tokenizer, chat template and signatures, whose answers are the
mappings of the acceptance project's `models/delivery-note-android`), with a folder of the app's
and the files pack's copy step.

## A dictated note

The courier can dictate a note about a delivery with Android's `SpeechRecognizer`
(`DictatedNote.kt`): the app shows the transcript and saves it with the parcel as the courier's
note, apart from the recipient's. The speech recognizer is Android's: the tests put a stub of
its API in the app's `axx-models` folder (`dictated-note-android.axxmodel`, of the acceptance
project's `models/dictated-note-android`), and the microphone step plays a recording into the
app's microphone while it listens. Both builds run the same code.

## No one home

When no one answers the door, the courier taps "No one home" and rings or knocks: the app
listens (`DoorSounds.kt`) and records the attempt with the delivery, with what it heard, as
proof that the courier came. MediaPipe's audio classifier runs YAMNet (of AudioSet's 521 sounds,
among them `Doorbell`, `Ding-dong` and `Knock`) on the microphone's sound (`AudioRecord`, at 16
kHz), a window of 0.975 seconds at a time, half a window apart; the first doorbell or knock it
is 50% sure of is the attempt's. A release build fetches YAMNet as MediaPipe publishes it,
checked against its SHA-256; the debug build ships a stub of it
(`app/src/debug/assets/door-sounds.tflite`, 298 KB to the model's 4 MB), whose answers are the
mappings of the acceptance project's `models/door-sounds-android`: the scenarios' recordings of
a doorbell and a knock (`acceptance/features/courier-app/recordings`, cut from recordings on
Wikimedia Commons that their authors gave to the public domain, CC0: "Electronic doorbell" and
"Knock on door") to `Doorbell` and `Knock`, and anything else to `Silence`.

## The parcels service

The app calls the parcels service at `http://localhost:8400`, the address it has on the Mac, as
the iOS app does. The emulator reaches the Mac's port as its own once adb forwards it, as the
scenarios' `host ports` row does:

```sh
adb reverse tcp:8400 tcp:8400
```

A launch intent's `api_url` extra points the app somewhere else until its process ends:

```sh
adb shell am start -n example.parcels.courier/.MainActivity --es api_url http://10.0.2.2:8401
```

Plain HTTP is allowed to `10.0.2.2` and `localhost` only.

## Deep link

`parcels-courier://deliveries/{reference}` opens that delivery, after signing in if the courier
has not:

```sh
adb shell am start -a android.intent.action.VIEW -d parcels-courier://deliveries/PX-MOB-9401
```

A parcel that is not out for delivery with the signed-in courier shows
"PX-MOB-9401 is not out for delivery with you."

## Notifications

A delivery marked delivered is announced in the "Deliveries" notification channel. The app
never asks for the notification permission; grant it to see them:

```sh
adb shell pm grant example.parcels.courier android.permission.POST_NOTIFICATIONS
```

## The camera

The courier scans a parcel's label to open it ("Scan label", `Camera.kt`): the app reads its QR
code with ML Kit's barcode scanning, or, where the code is torn off, the reference printed under
it with ML Kit's text recognition, in CameraX's frames. At the door, the courier photographs the
parcel where they left it ("Photo at the door"), and the app attaches the photo, upright, to the
delivery as `photo-at-the-door.jpg`. Before leaving a parcel in a parcel locker, the courier
measures it ("Measure parcel", `MeasureParcel.kt`): ARCore finds the two ends the courier taps,
and the app shows how far apart they are. ARCore is optional (`com.google.ar.core` in the
manifest): a phone without it measures nothing, and the rest of the app works.

An emulator's camera sees a room of its own, and an emulator has no ARCore: the tests say what
the camera sees (`acceptance/features/courier-app-camera`, `courier-app-measure`), a photo of a
label, a video of one, or a scene of a parcel on a porch, and axx plays the emulator's camera
from it, and the debug build's ARCore from the scene. Both builds run the same code.
