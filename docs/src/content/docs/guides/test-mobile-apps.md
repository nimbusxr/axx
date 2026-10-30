---
title: Test mobile apps
description: "Use your Android and iOS apps the way people do, on emulators and simulators axx runs: tap and fill their controls by the names people see, open them with deep links, check what they show; each scenario with a device to itself and a clean app."
---

Many acceptance criteria describe what a person does in an app: a courier signs in, taps a parcel and marks it delivered. The `mobile-android` and `mobile-ios` packs do exactly that, on Android emulators and iOS simulators, driven by [Appium](https://appium.io), and a scenario can use every other pack too: it checks what your service did with the delivery, and what the parcel's tracking page shows.

```gherkin
Scenario: A courier delivers a parcel, and its recipient sees it delivered
  Given a seeds/courier-app-deliver.yaml db seed
  When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9411" link
  And the "Courier ID" field in the courier app is filled with "CR-LEJ-14"
  And the "PIN" field in the courier app is filled with "${env:COURIER_PIN}"
  And the "Sign in" button is tapped in the courier app
  Then the courier app shows "Eisenbahnstr. 41, 04315 Leipzig"
  When the "Signed by" field in the courier app is filled with "C. Busch"
  And the "Mark delivered" button is tapped in the courier app
  And the "Confirm" button is tapped in the courier app
  Then the courier app shows "Delivered"
  And the courier app shows a notification "PX-MOB-9411 delivered"
  When the "/track/PX-MOB-9411" page is opened
  Then within 20s the page shows "Delivered on"
```

The steps are the same on Android and on iOS: only the app's registration says which it is. Add the pack for each platform you test with `axx pack add mobile-android` or `axx pack add mobile-ios` (both build on `mobile-core`, the steps of phones, and on `app-core`, the steps every app has, on a phone or a desktop; [Choose packs](/guides/use-packs/)).

## What it needs

**For Android:**

- **The Android SDK**, with `ANDROID_HOME` pointing at it: its emulator, and adb. Android Studio installs it, or its command-line tools. An adb server axx starts has its mDNS discovery off (`ADB_MDNS=0`, unless you set `ADB_MDNS`): axx needs none, and on some networks it makes adb's server abort.
- **An emulator's device (AVD)**, like the one Android Studio's Device Manager makes, or one `avdmanager` makes:

  ```sh
  sdkmanager --install "system-images;android-35;google_apis;arm64-v8a"   # x86_64 on Intel and AMD
  avdmanager create avd --name parcels-pixel --package "system-images;android-35;google_apis;arm64-v8a" --device pixel_8
  ```
- **For an app's ARCore** ([below](#what-the-camera-sees-and-ar)): the SDK's NDK and build-tools, and a JDK, which axx builds its agent with, once.

**For iOS:**

- **A Mac with Xcode**, and an iOS runtime for its simulators (Xcode's Settings, Components). axx makes the simulators itself.
- **The app built for the simulator**: the `.app` that `xcodebuild -sdk iphonesimulator` builds, or it zipped.

`axx doctor` checks them: the Android SDK and its devices (and KVM on Linux), Xcode and its iOS runtimes.

**Nothing else:** axx downloads what drives the apps the first time a run needs it, pinned, as it downloads a browser driver for web apps. For Android, that is the UiAutomator2 server, which axx installs on each device and starts itself, and keeps running while the device is in use: a scenario's session starts in the time its app takes to reset. For iOS, it is WebDriverAgent, the app on the simulator that drives yours, as the [Appium](https://appium.io) project builds it, which axx starts on each simulator itself: nothing is built with Xcode, and nothing needs signing.

## Register the app

```gherkin
Given the courier android app with the following properties:
  | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
  | device      | parcels-pixel                                                |
  | permissions | POST_NOTIFICATIONS                                           |
  | timezone    | Europe/Berlin                                                |
  | host ports  | 8400                                                         |
```

```gherkin
Given the courier ios app with the following properties:
  | app      | ../courier/ios/build/Debug-iphonesimulator/Courier.app |
  | device   | iPhone 16                                              |
  | timezone | Europe/Berlin                                          |
```

| Property | What it is |
| --- | --- |
| `apk` (Android), `app` (iOS) | The app, a file of the project: it is the app the scenario tests. On iOS, a simulator build (`.app`), or it zipped. |
| `package`, `activity` (Android), `bundle id` (iOS) | The app's identity: with no `apk` or `app`, an app the device has. On iOS, the `app`'s own bundle identifier by default. |
| `device` | Android: an emulator's device (AVD) axx starts, or a device adb lists, by its serial. iOS: a device type axx makes simulators of, like `iPhone 16` or `iPhone 16, iOS 18.1` (the newest iOS Xcode has, by default); a simulator you set up, by its name, which axx clones; or a simulator's UDID. |
| `permissions` | What the app may use from the start, and nothing else. Android: permissions like `POST_NOTIFICATIONS`. iOS: services like `location, photos` (`xcrun simctl help privacy` lists them). |
| `locale`, `timezone`, `location` | The language and region (`de-DE`), the time zone (`Europe/Berlin`), and where the device says it is (`51.3397, 12.3731`). On Android the language is the app's own (Android 13 and later), and the device's stays as it is. |
| `photos`, `documents` | Files the phone has before the app starts, for the app to pick in the system's pickers, like `photos/begin.jpg` or `documents/safe-place-authorization.pdf`: found as a step finds a file, next to the feature file first. Photos go in the photo library; documents where the file picker shows them (see [Pick a document](#pick-a-document)). |
| `host ports` (Android) | Ports of the machine axx runs on that the app reaches as `localhost` on the device, like `8400, 5500`: the app calls `http://localhost:8400` as on a developer's device, and gets the service there. Each scenario has only its own registration's. |
| `appium`, `capability.<name>` | An Appium server of your own or a device farm's, which runs the device, and the capabilities it takes. On Android without `appium`, the capabilities go to the UiAutomator2 server's session. |

The app starts when a step launches it or opens a link in it. Every step names the app, so a scenario can drive two, and a web app too.

## Each scenario has a device, and a clean app

A scenario leases a device for its whole run: no other scenario uses it meanwhile. The packs run devices as scenarios need them, up to `devices` of each at once (1 by default: those scenarios then take turns while the others run).

```yaml title="axx.yaml"
packs:
  mobile-android:
    devices: 2   # two emulators: two Android scenarios at once
  mobile-ios:
    devices: 2   # two simulators: two iOS scenarios at once
```

- **Android** starts emulators of the device read-only, so that several run at once and nothing a scenario changes outlives it. A device with a boot snapshot (boot it once, and close it) starts in seconds; without one, each emulator boots cold.
- **iOS** gives each device it runs a clone, deleted when the run ends: of the simulator you set up, or, for a device type, of axx's own simulator of it. axx makes that one and boots it once to set it up, the same on every Mac, and keeps it in its cache (`~/Library/Caches/axx/mobile/simulators`, apart from Xcode's). No scenario ever runs on it, and its clones boot in seconds. WebDriverAgent, the app that drives yours, starts once on each simulator, for all the scenarios that run on it.

Before its app starts, a scenario's app is reset, and what it sees of the device is set:

- **Android:** its data cleared (and with it its sign-in, its permissions and its notifications), the permissions its registration names granted, the notification shade and any dialog of the system's closed, the ports it reaches on this machine set, the photos and documents a scenario before put on the device taken out (and the registration's put in), the system's file picker started afresh, and the device's language, time zone and location set: an emulator goes back to where it starts when the registration names none. On the emulators axx starts, other apps that hang or crash show no dialog over yours, and Chrome opens a page at once, without its first-run screens.
- **iOS:** the app installed afresh (its data and notifications go with the old one), the simulator's keychain reset (a sign-in kept there outlives the app), its permissions reset and those its registration names granted, the documents a scenario before put On My iPhone taken out (and the registration's put in), and the location set. It starts in the registration's language, region and time zone.

There is no switch that skips it: a scenario is one journey, and that journey is its own. Its data stays unique as everywhere in axx: each scenario above has a courier and parcels of its own.

### Keep simulators warm

A run makes its iOS simulators and deletes them at its end. With `keep`, it keeps them booted for the next run instead, which then starts at once: in the parcels example, its three iOS scenarios ran in 41 seconds instead of 68.

```yaml title="axx.local.yaml"
packs:
  mobile-ios:
    keep: true
```

The kept simulators are in Xcode's own set, named like `axx iPhone 17, iOS 27.0 (1)`, where Device Hub lists them. A run uses one only while no other run does, and each scenario still has its simulator to itself, its app reset. A watched run keeps them. To get rid of them, delete them in Device Hub, or with `xcrun simctl delete`.

## Run one platform

A profile per platform runs the scenarios of one app, with no tags on the features: `run.uses` picks the scenarios by the packs their steps use ([Select by what scenarios use](/guides/tags-and-filtering/#select-by-what-scenarios-use)).

```yaml title="axx.yaml"
profiles:
  ios:     {run: {uses: [mobile-ios]}}
  android: {run: {uses: [mobile-android]}}
```

`axx run --profile ios` runs the iOS app's scenarios, and `--profile ios,watch` runs them watched.

## Watch it

`axx run --watch` shows what the scenarios do as they do it: an iOS simulator in Device Hub (the Simulator app before Xcode 27), an Android emulator in its window, a browser in its own, one scenario at a time, slowed down with `--slowdown 500ms` ([Watch a run](/guides/watch-runs/)). Without `--watch`, the devices run without a window, and a Device Hub you have open stays open.

## Launch it, or open it with a link

```gherkin
When the courier app is launched
When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9411" link
When the courier app is sent to the background
When the courier app is brought back
When the courier app is restarted
```

A deep link jumps straight to the screen a scenario tests, instead of tapping through the ones before it. A restart keeps what the app stored, like a sign-in; the next scenario's reset does not.

## An app's files

An app's files are a folder of the files pack whose `owner` is the app: `./` is its data on the device (on iOS its data container, on Android its data folder, which Android opens to a debuggable build only) ([Check files](/guides/check-files/)). A scenario reads what the app saved there, and puts files in place before the app first reads them: the folder starts the app's device, installs the app and resets it, as launching does, without launching it. A scenario about a returning user starts so, with the app's settings as that user's app has them:

```gherkin
Given the settings folder with the following properties:
  | owner | app:courier         |
  | path  | Library/Preferences |
And the example.parcels.courier.plist file in the settings folder is a copy of the fixtures/signed-in-before.plist file
When the courier app is launched
```

## Tap and fill

```gherkin
When the "Sign in" button is tapped in the courier app
When the "PX-MOB-9401" list item is tapped in the courier app
When the "PIN" field in the courier app is filled with "${env:COURIER_PIN}"
When the courier app is swiped down
When the "PX-MOB-9412" list item is scrolled into view in the courier app
When the courier app's back button is pressed
```

Controls are found by the names people see: a button by its text, a field by its label, a list item by one of its texts (its reference, say), an image by its description. The kinds are `button`, `field`, `checkbox`, `switch`, `tab`, `list item`, `image`, `text` and `element` (anything). Jetpack Compose and Android's views both work, and SwiftUI and UIKit on iOS. When no name tells a control apart, `id=` (its resource ID on Android, its accessibility identifier on iOS) or `xpath=` finds it. `${env:..}` values are secrets: masked in logs and failures.

`swiped down` pulls a list to refresh; `up`, `left` and `right` swipe too. The back button is Android's; on iOS, tap the back button the screen shows, by its text.

`scrolled into view` scrolls the list or text that holds the control until it shows, as a person does. A control far down a long text, like the end of a terms of service, is reached in one move on iOS, which grabs the scroll bar and drags it there, and in a few flings on Android.

## Check what it shows

```gherkin
Then the courier app shows "Hello, Hanna Wolf"
Then the courier app does not show "PX-MOB-9401"
Then the "Mark delivered" button is disabled in the courier app
Then the "Courier ID" field in the courier app has the value "CR-LEJ-12"
Then the courier app shows a notification "PX-MOB-9411 delivered"
Then the courier app looks like the "deliveries" screenshot
```

Checks wait for the app, 10 seconds or `within {duration}`, as apps take their time. A failed step attaches what the app showed, and the scenario's failure says which device and which resets it had. A notification is looked for where people look: Android's notification shade, iOS's Notification Center.

**Screenshots** are `<name>.<device>.png` in a `screenshots` folder next to the feature file, like `deliveries.android-parcels-pixel.png` or `deliveries.ios-iPhone-16-iOS-18-1.png`: each device has its own. The device's status and navigation bars, its clock among them, are left out. The first time, the step takes the screenshot and fails: look at it, keep it, and run again. `packs.mobile-core.screenshots` in `axx.yaml` sets the `folder`, the `tolerance` and `update: true`, which takes them again.

## Tap a notification

```gherkin
When the "PX-MOB-9402 delivered" notification is tapped in the courier app
When the "Next delivery" action of the "PX-MOB-9401 delivered" notification is tapped in the courier app
```

A notification is tapped where people find it: the step opens Android's notification shade or iOS's Notification Center, opens the group it is folded into (on iOS, one already seen waits below the screen, and is swiped into view), and taps it, and the app opens where the notification leads. An action is one of the buttons a notification offers: Android shows them under it, and iOS when a person holds the notification.

On a simulator, iOS answers a tap on a notification as on an iPhone without Face ID: it slides the notification aside to show Open, which the step taps too. And it shows a notification's actions in Notification Center only on a phone it can tell is in a hand, one with a proximity sensor (or Face ID unlock, which takes a passcode a simulator cannot have). So axx gives the simulator's SpringBoard the proximity sensor an iPhone has: a small library axx builds with Xcode the first time, and loads into the simulators it boots. In your app, it does only what [background tasks](#run-a-background-task-now) need. A simulator that runs already (one of yours, or one kept warm) gets it as SpringBoard starts again, before the scenario.

## Run a background task now

```gherkin
When the "round-reminder" background task is run in the courier app
Then within 30s the courier app shows a notification "2 deliveries left today"
```

An app schedules work for the phone to run later: a sync, a reminder, a check. The phone chooses when, every 15 minutes at the soonest on Android, and whenever it suits it on iOS. The step has the phone run one of the app's tasks now, as it does when the task's time comes. It is a nudge, not a stand-in: the app's own code runs, in the app, and does what it does then. Only the moment moves. The app must have scheduled the task. The step waits for it to be done, so a check after it finds what it did: on iOS, until the app says it is done (`setTaskCompleted`), in the 30 seconds iOS gives a task; on Android, until the work ends, as WorkManager's database has it.

The task is named as the app names it:

- **Android:** its WorkManager work's unique name (`enqueueUniquePeriodicWork("round-reminder", ...)`), or one of its tags. WorkManager tags each piece of work with its worker's class, so `RoundReminderWorker` names it too. axx asks Android's job scheduler to run the work's job now, and Android runs it only if the conditions the app set for it, a network say, are met. WorkManager runs periodic work no sooner than its period allows, so axx first tells it the period has passed, in its own database on the phone. That takes the phone's `sqlite3`, which emulators with a Google APIs image have, and a debuggable build, as an app's files do.
- **iOS:** its identifier, one of `BGTaskSchedulerPermittedIdentifiers` in its `Info.plist`. iOS runs the task as Xcode's debugger command does on an iPhone: the handler the app registered gets the task the app scheduled, once for each time it scheduled it. A simulator refuses to schedule any task, so axx's library, which the simulators it boots load, keeps the app's requests as an iPhone does. An app in the background is suspended, and SpringBoard wakes it for the task, as iOS does, and holds it awake until the task is done.

A name the app does not have fails, and the failure lists the tasks it has.

## Pick a document

An app that takes a file from the phone, like a courier's app attaching a recipient's signed authorization to a delivery, opens the system's file picker. The `documents` property puts the files on the phone before the app starts, and the scenario picks one as a person does, with the tap steps:

```gherkin
Given the courier android app with the following properties:
  | apk       | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
  | device    | Pixel_9                                                      |
  | documents | documents/safe-place-PX-MOB-9481.pdf                         |
And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9481" link
When the "Attach document" button is tapped in the courier app
And the "Show roots" button is tapped in the courier app
And the "Downloads" text is tapped in the courier app
And the "safe-place-PX-MOB-9481.pdf" text is tapped in the courier app
Then the courier app shows "safe-place-PX-MOB-9481.pdf attached"
```

- **Android** puts them in the device's Downloads, indexed as a download is. The picker opens at Recent: *Show roots*, then *Downloads*, then the file by its name. On an emulator axx starts, each scenario's reset empties Downloads (what an app saved there too) and starts the picker afresh, as it keeps where each app last picked a file; on a device of yours, it takes out only the documents axx put there.
- **iOS** puts them On My iPhone, in the simulator's Files. In the picker, *Browse*, then *On My iPhone*, then the file as a list item, by its name without its extension, as Files shows it (below). On a simulator axx makes, each scenario's reset empties On My iPhone (what an app saved there too); on a simulator of yours, it takes out only the documents axx put there.

```gherkin
When the "Attach document" button is tapped in the courier app
And the "Browse" button is tapped in the courier app
And the "On My iPhone" text is tapped in the courier app
And the "safe-place-PX-IOS-9591" list item is tapped in the courier app
```

The phone has its documents in one folder, by their names: two files of one name are refused. A device of an Appium server's, like a device farm's, takes files its own way.

## Dialogs the system shows

```gherkin
Then the courier app's dialog shows "send you notifications"
When the courier app's dialog is dismissed
```

A dialog the system shows over the app, like a request for a permission it asked for, is accepted (allowed) or dismissed. iOS lets no one grant notifications ahead: an app that uses them asks, and the scenario answers. Accepted, a request with a choice of how to allow allows for good: *While using the app* for a location (not *Only this time*), *Allow Full Access* for photos. A dialog of the app's own is part of its screen: tap its buttons, like `the "Confirm" button is tapped in the courier app`.

## Move the phone along a route

The `location` property puts the phone in one place. An app that follows the phone as it moves, like a courier's app sharing where the van is, needs it to travel, with its GPS reporting where it is, how fast it goes and which way:

```gherkin
When the courier app's phone travels at 30 km/h through:
  | 51.3397 | 12.3731 |
  | 51.3397 | 12.3760 |
```

The table's rows are the points the route goes through, each a latitude and a longitude; the phone keeps one speed (`mph`, `km/h` or `m/s`) from the first to the last, and stays at the last. A route recorded on the road, or drawn in a mapping tool, is a GPX file: its track, else its route, else its waypoints. The phone keeps to the file's own times, stops included, or goes faster, or keeps one speed throughout:

```gherkin
When the courier app's phone travels the routes/grimmaische-strasse.gpx route
When the courier app's phone travels the routes/grimmaische-strasse.gpx route at 4x
When the courier app's phone travels the routes/grimmaische-strasse.gpx route at 30 km/h
```

`travels` waits for the phone to arrive. `starts traveling` lets the scenario go on while the phone moves, to check what the app does on the way, and a later step waits for it to get there:

```gherkin
When the courier app's phone starts traveling at 30 km/h through:
  | 51.3397 | 12.3731 |
  | 51.3397 | 12.3760 |
Then within 20s the tracking event stream has an event where:
  | event type | courier |
  | speedKmh   | 30      |
And the courier app's phone reaches the end of its route
```

The phone moves in real time, as a phone on the road does: keep routes short, or play a long one faster. On iOS, the simulator reports the route as CoreLocation reports a moving phone (`simctl location`): its speed, and its course where the simulator gives one (older simulators report none). On Android, the emulators axx starts report it every second, as NMEA sentences that carry the speed and course a plain location fix lacks; a device of your own reports where it really is.

## What the camera sees, and AR

An iOS simulator has no camera, and an Android emulator's sees a room of its own. An app that scans, reads or photographs through it (a QR code, a printed reference, a photo at the door) has nothing to see, and an app that measures or places things with ARKit (RealityKit's `ARView`) or ARCore has no AR either: the simulator's ARKit says world tracking is not supported, and an emulator has no Google services for AR. axx plays the camera from what a scenario says it sees, `the courier app's camera sees <file>`, on iOS simulators and on the Android emulators it starts:

- **an image** (PNG or JPEG, and HEIC on iOS), as the phone held upright sees it: a parcel's label, say;
- **a video** (MP4 or MOV; H.264 on Android), which plays in a loop: a parcel lifted into view;
- **a 3D scene**, a glTF file (`.glb`, or `.gltf` with its buffers and images), seen from where the phone is in it: what the camera sees changes as the phone moves (below), and an app's AR finds the scene's surfaces and objects where they are.

```gherkin
Given the courier app's camera sees labels/label-PX-IOS-9621.png
And the courier app is launched
And the "Courier ID" field in the courier app is filled with "CR-LEJ-85"
And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
And the "Sign in" button is tapped in the courier app
When the "Scan label" button is tapped in the courier app
Then the courier app shows "Ida Lorenz"
```

On a simulator, the app's capture session takes the camera as its input, as on a phone (`AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: .back)`), and gets its frames, 30 a second, 1920 by 1080, the sensor's way up as an iPhone's:

- a **video data output** gets them in the pixel format it asks for (BGRA, or YCbCr 4:2:0), turned as its connection says, for Vision and the like;
- a **metadata output** gets the codes in them, QR codes and barcodes, read with Vision;
- a **photo output** takes a photo of the frame, a JPEG (or HEIC) marked to be turned upright as an iPhone's is;
- a **preview layer** shows them, and its conversions (a code's place on the screen, a point of interest) go by them;
- the camera answers what an app sets (focus, exposure, zoom, torch, HDR) as a phone's does, without changing what it sees.

The app asks for the camera as it would on a phone; `permissions | camera` (`CAMERA` on Android) grants it from the start. A scenario taps what an AR app's camera shows by its name in the scene:

```gherkin
Given the courier app's camera sees scenes/parcel-on-porch.glb
And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9581" link
And the "Measure parcel" button is tapped in the courier app
When the "front left corner" in the courier app's camera view is tapped
And the "front right corner" in the courier app's camera view is tapped
Then the courier app shows "60 cm"
```

The scene is where the phone is and what it sees:

- **The phone** is the scene's camera, or the camera named `phone` when it has several: where it stands, held upright, looking along its −Z, its +Y the top of the phone (a glTF camera's own axes). The phone's camera is an iPhone's wide camera (about 62° across, the picture filling the view), or the Android emulator's (its sensor 1280 by 960, about 53° across held upright); the scene's camera gives only where it is and which way it looks.
- **Objects** a scenario names are the scene's top-level nodes with a name, but for its cameras and its furniture: nodes named with a leading `_`, like `_road` or `_porch`.
- **A tap on an object** lands on its middle, or on its `_tap` child, a node that marks where a tap on it lands: a corner guard's `_tap` is the parcel's corner it guards. The app's AR finds the scene's surfaces where they are, and a raycast that hits an object lands where a tap on it lands, so what an app measures between two objects is their distance in the scene, whatever the phone's place.
- **What is drawn** is the scene's triangle meshes, each node's transform (its matrix, or its translation, rotation and scale), and its materials' base color, a color and an image (PNG or JPEG), lit by a soft sky and the sun. Points and lines are left out, as are skins and morph targets; an animation is a path the phone can travel (below), and the scene's objects stay still.

A tool like Blender exports a scene as glTF; a small program writes one too, as the parcels example's [`make-scenes.go`](https://github.com/nimbusxr/axx/blob/main/examples/parcels/acceptance/scenes/make-scenes.go) does. The step reads the scene, and fails on one it cannot play: no camera, two cameras none named `phone`, two objects of one name. A running app starts again, to see it; the step before the app is launched gives the scene from the start.

### Moving the phone

The phone moves through the scene as the person holding it does, and the app's camera, and its AR, see the scene from wherever it is:

```gherkin
Given the courier app's camera sees scenes/parcel-on-porch.glb
And the courier app's phone moves to the scene's "by the van" camera
And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9582" link
And the "Measure parcel" button is tapped in the courier app
When the courier app's phone travels the scene's "up the garden path" path
And the courier app's phone moves 1 m forward
And the courier app's phone turns 40 degrees down
And the "front left corner" in the courier app's camera view is tapped
```

- **A place the scene names** is one of its cameras: `moves to the scene's "by the van" camera` puts the phone where that camera is, looking the way it looks, at once.
- **A path** is one of the scene's animations that moves a camera, the phone's or another one it follows: `travels` carries the phone along it as it was recorded, and waits for it to arrive; `starts traveling` lets the scenario go on while it moves, and `the courier app's phone reaches the end of its route` waits for it. `at 2x` goes twice as fast; `at 1.4 m/s` keeps one speed along the way.
- **A step** moves the phone at a walking pace, still facing the same way: `forward` and `back` along the ground the way it looks (where its top points, when it looks straight down), `left` and `right` across it, `up` and `down`.
- **A turn** turns it where it stands, as the person holding it turns: `left` and `right` about the upright, keeping its tilt; `up` and `down` tilt it.

The phone stays where a step leaves it, the app started again or not: a step before the app is launched puts the phone where the app starts.

On a simulator, axx loads a library of its own into the app when it starts, built once with Xcode into axx's cache: it plays the camera for the app's capture sessions; from a scene, it also makes world tracking supported, runs the session's frames from the scene, draws the camera's picture behind the `ARView`, and answers RealityKit's raycasts. The app is the app you ship, unchanged; the library plays the camera and ARKit, which a simulator has not got. It plays apps that show AR with RealityKit's `ARView`; an `ARSCNView` (SceneKit) app is not played yet, nor the system's own camera screen (`UIImagePickerController` with the `.camera` source), which an app that checks `isSourceTypeAvailable(.camera)` does not offer on a simulator. It leans on ARKit's internals, so each Xcode is checked against it: it is checked with Xcode 27.0, on the iOS 27.0 and 18.1 simulators. A real iPhone's camera, through an Appium server, sees what is in front of it: the step is for simulators.

On an Android emulator, its own back camera shows what the camera sees, to any app (CameraX, Camera2, the system's camera app): axx starts its emulators with the camera that renders the AVD's environment (`-camera-back environment`, from the AVD's `environment.ini`, which axx writes before each one starts), and sets it for each scenario through the emulator's control service: a picture, a video, or the scene, seen from where axx puts the phone in it. The emulator shows a picture off its middle, so axx lays out a picture, and a video frame by frame, so that the camera sees all of it, aspect-filled into its 4:3 sensor held upright; it re-encodes an H.264 video with Cisco's OpenH264, which it downloads the first time, as it does for desktop videos (OpenH264 Video Codec provided by Cisco Systems, Inc.). An emulator started from a quick-boot snapshot keeps the camera the snapshot was saved with: axx starts it afresh, cold, the first time a scenario's camera sees something, unless the snapshot was saved with `hw.camera.back=environment` in the AVD's `config.ini`, as the parcels example's [`create-emulator.sh`](https://github.com/nimbusxr/axx/blob/main/examples/parcels/courier/android/create-emulator.sh) saves it.

An Android app's AR is ARCore's. In a debuggable app that has ARCore, axx attaches an agent of its own as the app starts, which answers the app's ARCore calls from the scene: a session that runs, its camera at the phone's pose, the emulator's camera's picture in the texture the app gave ARCore, filling the view as ARCore fills it, and hit tests that land on the scene's surfaces, and on a named object where a tap on it lands. It answers the session, its frames and camera, hit results, planes and points, anchors and light estimates; depth, cloud anchors, geospatial and recording are not played. axx builds the agent once into its cache, with the SDK's NDK and build-tools and a JDK, as the models-android pack builds its own, and attaches both when a scenario needs both. An app that is not debuggable runs its ARCore as it is. A real phone's camera, through an Appium server, sees what is in front of it: the step is for emulators.

## Traces and videos

Ask for a **trace** of each app, and a scenario keeps one in `.axx/mobile/traces`: a page with its screen after each step, and its controls where it failed. A trace waits, after every step, for the screen's next frame (about a tenth of a second): kept for failed scenarios only, it still slows every step a little, so it is off until you ask.

A **video** is the scenario's phones as they ran, each tap and swipe drawn where the finger was, with the scenario's steps beside them: each one to run, running, passed, failed (its error under it) or skipped, and the lines the running step logs, so what happens off the screen shows too (a call to a service, a row in a database). It opens on its first frame for a moment, the steps from the first (those that ran before the app had a screen, such as its registration, among them), and its last frame shows for two seconds, the steps as they ended. It is an MP4 file (H.264) in `.axx/mobile/videos` that plays wherever videos play, with a chapter for each step; a failed scenario's is attached to the report too. The run keeps one more, `run.mp4`: its kept scenarios one after another, each after a card with its name and whether it passed, and a chapter for each. Every phone has the same place in a video, so an iOS scenario and an Android one are in the same `run.mp4`; a scenario that drives two apps shows both phones, side by side. QuickTime Player, VLC, IINA and mpv list the chapters; a browser plays the video without them.

The screens come from the drivers' streams of them: WebDriverAgent's on iOS, the UiAutomator2 server's on Android, ten frames a second at half the screen's size. A farm's device, driven through `appium`, streams none: its scenarios keep neither.

```yaml title="axx.yaml"
packs:
  mobile-core:
    traces: failed   # always, failed or never (the default)
    videos: failed   # always, failed or never (the default)
```

Videos are encoded with Cisco's OpenH264. OpenH264 Video Codec provided by Cisco Systems, Inc. The first time a scenario keeps a video, axx downloads Cisco's build for your OS from Cisco (about half a megabyte), checks it, and keeps it in its cache; `videos: never`, the default, turns it off, and axx then downloads nothing. Cisco's terms are in axx's `THIRD_PARTY_NOTICES.md`.

## In CI

**Android:** Linux runners start emulators with KVM, and draw them without a GPU. The parcels example's workflow does it this way, with the Android SDK the runner has:

```yaml
- name: KVM, for the emulator
  run: |
    echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | sudo tee /etc/udev/rules.d/99-kvm4all.rules
    sudo udevadm control --reload-rules
    sudo udevadm trigger --name-match=kvm
- run: ./create-emulator.sh   # sdkmanager and avdmanager: the device the features name
- run: axx run --tags @android
```

`create-emulator.sh` saves the device's boot snapshot with the camera axx plays (`hw.camera.back=environment` in its `config.ini`), so that the emulators axx starts from it are ready for what a scenario's camera sees. It gives the device a front camera too (`hw.camera.front=emulated`): the system image says its phone has one, and on an AVD without, CameraX looks for it for some 6 seconds before an app's camera shows. An emulator draws its camera without a GPU too, slower than a Mac's: it takes seconds to show its first frame, which a scenario that reads what the camera sees gives time (`within 30s`). `packs.mobile-android.emulatorArgs` passes more arguments to the emulators, and `bootTimeout` gives a slow machine longer than 3 minutes to boot one. A feature too long for one runner, whose emulator runs a scenario at a time, [shares a run](/guides/run-in-ci/#share-a-run-between-runners) with others: `axx run --shard 1/2` on one, `--shard 2/2` on another.

**iOS:** macOS runners have Xcode and its simulators. A new machine's first simulator boot builds the iOS runtime's shared cache, which takes over ten minutes on GitHub's runners, and every hosted run is a new machine: keep that cache between runs, with what axx downloads, keyed by the macOS and runtime builds it belongs to. Never keep a simulator: its keychain belongs to the machine it was made on.

```yaml
runs-on: macos-latest
steps:
  - uses: actions/checkout@v7
  - id: ios
    run: |
      runtime=$(xcrun simctl list runtimes -j | jq -r '[.runtimes[] | select(.platform == "iOS" and .isAvailable)] | sort_by(.version | split(".") | map(tonumber)) | last | .buildversion')
      echo "key=ios-$(sw_vers -buildVersion)-$runtime" >> "$GITHUB_OUTPUT"
  - uses: actions/cache@v6
    with:
      path: |
        ~/Library/Developer/CoreSimulator/Caches/dyld
        ~/Library/Caches/axx/mobile
        !~/Library/Caches/axx/mobile/simulators
      key: ${{ steps.ios.outputs.key }}
  - run: xcodebuild -project Courier.xcodeproj -target Courier -configuration Debug -sdk iphonesimulator ONLY_ACTIVE_ARCH=NO SYMROOT=build
  - run: axx run --tags @ios
```

With the cache, a run sets its simulators up in a minute or two; without it, the first scenario's steps need over ten minutes (`run.timeouts: {step: 20m}` in `axx.yaml` gives them that; `packs.mobile-ios.bootTimeout`, 20 minutes by default, bounds a simulator's boot). A simulator on a hosted runner stays slow for a few minutes after it boots: give checks there `within` to spare. GitHub's macOS runners have no Docker: when the services your scenarios talk to run in containers, run them elsewhere, or run the iOS scenarios on a Mac that has them.

See the [app-core](/references/packs/app-core/), [mobile-core](/references/packs/mobile-core/), [mobile-android](/references/packs/mobile-android/) and [mobile-ios](/references/packs/mobile-ios/) references for every step.
