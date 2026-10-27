# Parcels Courier (iOS)

The handheld a courier of the parcels service delivers with, the iOS twin of the Android app
(`../android`). The courier signs in with their courier ID and PIN, sees the parcels out for
delivery with them today, and marks each one delivered with the name of whoever signed for it.
The service's courier endpoints (`../../app/courierapp.go`) back it.

## Build

```sh
xcodebuild -project Courier.xcodeproj -target Courier -configuration Debug \
  -sdk iphonesimulator ONLY_ACTIVE_ARCH=NO SYMROOT=build
```

The app lands in `build/Debug-iphonesimulator/Courier.app`, for the iOS Simulator (iOS 17 or
later). The build needs Xcode, and nothing to sign with: a simulator build is signed ad hoc
("Sign to Run Locally"), which gives the app the entitlements the simulator's keychain asks for.
Do not add `CODE_SIGNING_ALLOWED=NO`: without those entitlements the keychain refuses the app
(`errSecMissingEntitlement`), and the courier is signed out at every launch.

```sh
xcrun simctl install booted build/Debug-iphonesimulator/Courier.app
xcrun simctl launch booted example.parcels.courier
```

## The photo check

A photo the courier attaches must show the parcel: an image classifier (Apple's MobileNetV2)
checks it on the phone, with Core ML through Vision, before the app uploads it
(`PhotoCheck.swift`). The "Photo check model" build phase compiles the model into the app, as
Xcode compiles a model: in a Release build, the model from Apple, checked against its SHA-256;
in a Debug build, which the tests install, a stub of it (`Courier/Models/PhotoCheck.mlmodel`,
65 KB to the model's 24.7 MB): a model of the same inputs, outputs and class labels, whose
answers are the mappings of the acceptance project's `models/photo-check-ios`. `go generate
./...`, in axx's repository, builds it again. (The phase writes a folder, the compiled model,
which Xcode's script sandbox does not let a phase write: the target runs its scripts without
it.)

The photo must also show the whole parcel, not one cut off at the photo's edge: an object
detector of the company's own finds the parcel (`ParcelFinder.swift`), a Create ML object
detector of one label, `parcel` (a detector's pipeline: its network of candidates, then their
non-maximum suppression, which Vision runs and gives as `VNRecognizedObjectObservation`s, of the
photo stretched to the model's input). A parcel whose box reaches within 1% of the photo's edge
is cut off, and the app asks the courier to step back. The "Parcel finder model" build phase
compiles it as the photo check's is: in a Release build, the company's model, whose path the
company's build gives (`PARCEL_FINDER_MODEL`: it is published nowhere, so a Release build without
it stops, named); in a Debug build, a stub of it (`Courier/Models/ParcelFinder.mlmodel`, 15 KB
to the model's 32 MB), whose answers are the mappings of the acceptance project's
`models/parcel-finder-ios`: each photo's parcel, by its box.

The app also looks at the photo with Apple's Vision framework (`DeliveryPhoto.swift`). A photo
of a page (`VNClassifyImageRequest`: a document, handwriting, a receipt), like the recipient's
signed note, is a document, and needs no parcel. In a photo of the parcel, a parcel's label it can
read (`VNRecognizeTextRequest`) must be the delivery's, a dog
(`VNRecognizeAnimalsRequest`) must not be beside the parcel, and the people in it
(`VNDetectHumanRectanglesRequest`) are blurred out, where person segmentation
(`VNGeneratePersonSegmentationRequest`) has them, before the photo leaves the phone. Vision's
models are iOS's, in no file of the app's: the tests put stubs of their APIs in the app's
`axx-models` folder (`acceptance/features/courier-app-documents/models`, of the acceptance
project's `models/photo-*-ios`), which the `models-ios` pack answers Vision's requests from.

## The delivery note

A recipient's long delivery note (over 80 characters) is summarized in a line for the courier
(`DeliveryNotes.swift`); a short one is shown as the recipient wrote it. Apple's on-device
language model summarizes it (Foundation Models' `SystemLanguageModel`), on an iPhone with Apple
Intelligence. Its model is iOS's: the tests put a stub of its API in the app's `axx-models`
folder (`acceptance/features/courier-app/models/delivery-note.axxmodel`, of the acceptance
project's `models/delivery-note-ios`), which the `models-ios` pack answers the app's sessions
from, through iOS 27's public provider API.

On an iPhone without Apple Intelligence, a model of the app's own summarizes it:
SmolLM2-135M-Instruct, which a Release build bundles (`NotesModel.gguf`, 105 MB, from Hugging
Face, checked against its SHA-256) and runs with llama.cpp (its XCFramework, fetched and checked
alike), behind a Foundation Models model of the app's own (`CourierNotesModel.swift`, of the
`LanguageModel` protocol), which the app's sessions take as they take the system's. The "Notes
model runtime" and "Notes model" build phases fetch them and put them in the app. A Debug build
leaves them out, and gives its sessions a stub of the app's model instead
(`Courier/Models/CourierNotesModelStub.swift`, a Swift type of the protocol, 4 KB), whose
answers are the mappings of the acceptance project's `models/delivery-note-own-ios`; its
scenario puts a stub of an iPhone without Apple Intelligence in the app's `axx-models` folder
(`no-apple-intelligence.axxmodel`, of `models/no-apple-intelligence-ios`), so that the app takes
its own model. llama.cpp's framework is built for iPhones, not the simulator: a Release build is
an iPhone's.

The summaries need Xcode 27 and iOS 27; before them, or should the model fail, the courier reads
the note as it is.

## A dictated note

The courier can dictate a note about a delivery (`DictatedNote.swift`): the app feeds the
microphone (`AVAudioEngine`) to Apple's speech recognizer (`SFSpeechRecognizer`), shows the
transcript, and saves it with the parcel as the courier's note. Speech's models are iOS's: the
tests put a stub of its API in the app's `axx-models` folder (`dictated-note-ios.axxmodel`, of the
acceptance project's `models/dictated-note-ios`), and the microphone step plays a recording into
the app's microphone while it listens. Both builds run the same code.

## No one home

When no one answers the door, the courier taps "No one home" and rings or knocks: the app
listens (`AttemptListener.swift`) and records the attempt with the delivery, with what it heard,
as proof that the courier came. A sound classifier of the company's own hears it: a Create ML
sound classifier of three labels, `doorbell`, `knock` and `background`, which Sound Analysis
runs (`SNClassifySoundRequest`) on the microphone's sound, a window of 0.975 seconds at a time,
half a window apart. The first doorbell or knock it is 80% sure of is the attempt's. The "Door
sounds model" build phase compiles it as the parcel finder's is: in a Release build, the
company's model, whose path the company's build gives (`DOOR_SOUNDS_MODEL`); in a Debug build, a
stub of it (`Courier/Models/DoorSounds.mlmodel`), whose answers are the mappings of the
acceptance project's `models/door-sounds-ios`: the scenarios' recordings of a doorbell and a
knock (`acceptance/features/courier-app/recordings`, cut from recordings on Wikimedia Commons
that their authors gave to the public domain, CC0: "Electronic doorbell" and "Knock on door") to
what they are, and anything else to `background`. The stub is the bigger (282 KB to the model's
9 KB): Create ML's model is Core ML's audio feature print, whose network is iOS's and not in the
model, then a classifier of it; the stub, which matches a sound by its spectrum, computes its
own.

Once the attempt is recorded, Apple's server model (Private Cloud Compute, iOS 27's
`PrivateCloudComputeLanguageModel`) drafts the card the courier leaves for the recipient
(`MissedCard.swift`), which the app shows for the courier to write out; without it, the app drafts
none. The server model is Apple's: its scenario puts a stub of its API in the app's `axx-models`
folder (`missed-card.axxmodel`, of the acceptance project's `models/missed-card-ios`), which the
`models-ios` pack answers the app's session from.

## The parcels service

The app calls the parcels service at `http://127.0.0.1:8400`: the simulator shares the Mac's
network. The launch argument `-API_URL` or the environment variable `API_URL` points it
somewhere else for that launch:

```sh
xcrun simctl launch booted example.parcels.courier -API_URL http://127.0.0.1:8401
SIMCTL_CHILD_API_URL=http://127.0.0.1:8401 xcrun simctl launch booted example.parcels.courier
```

Plain HTTP is allowed to local addresses only (`NSAllowsLocalNetworking`).

## Signed in

The courier's sign-in is kept in the keychain, as a generic password of the service
`example.parcels.courier`. Like every keychain item it outlives the app: reinstalled, the app
finds the courier still signed in. Resetting the simulator's keychain signs the courier out:

```sh
xcrun simctl keychain booted reset
```

## Deep link

`parcels-courier://deliveries/{reference}` opens that delivery, after signing in if the courier
has not:

```sh
xcrun simctl openurl booted parcels-courier://deliveries/PX-MOB-9401
```

iOS may first ask `Open in “Parcels Courier”?`. A parcel that is not out for delivery with the
signed-in courier shows "PX-MOB-9401 is not out for delivery with you."

## Notifications

Once the courier sees today's deliveries, the app asks for leave to post notifications, once
while it runs, unless the courier has already allowed or refused them. A delivery marked
delivered is then announced ("PX-MOB-9401 delivered", "Signed by …"), also while the app is in
the foreground.

## Measure a parcel

A delivery's "Measure parcel" measures the parcel with the camera, before the courier leaves it
in a parcel locker: the courier taps one end of the parcel, then the other, and the app shows how
far apart they are (ARKit world tracking, in RealityKit's `ARView`). A simulator has no camera,
and its ARKit no world tracking: the app says it cannot measure, unless axx plays the camera from
a scene (`../../acceptance/features/courier-app-measure`).

## Logs

The app logs what it does under the subsystem `example.parcels.courier`:

```sh
xcrun simctl spawn booted log stream --predicate 'subsystem == "example.parcels.courier"'
```
