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

The steps are the same on Android and on iOS: only the app's registration says which it is. Add the pack for each platform you test with `axx pack add mobile-android` or `axx pack add mobile-ios` (both build on `mobile-core`, the steps every mobile app has; [Choose packs](/guides/use-packs/)).

## What it needs

**For Android:**

- **The Android SDK**, with `ANDROID_HOME` pointing at it: its emulator, and adb. Android Studio installs it, or its command-line tools.
- **An emulator's device (AVD)**, like the one Android Studio's Device Manager makes, or one `avdmanager` makes:

  ```sh
  sdkmanager --install "system-images;android-35;google_apis;arm64-v8a"   # x86_64 on Intel and AMD
  avdmanager create avd --name parcels-pixel --package "system-images;android-35;google_apis;arm64-v8a" --device pixel_8
  ```

**For iOS:**

- **A Mac with Xcode**, and an iOS runtime for its simulators (Xcode's Settings, Components). axx makes the simulators itself.
- **The app built for the simulator**: the `.app` that `xcodebuild -sdk iphonesimulator` builds, or it zipped.

`axx doctor` checks them: the Android SDK and its devices (and KVM on Linux), Xcode and its iOS runtimes.

**Nothing else:** axx downloads [Appium](https://appium.io) and its driver (UiAutomator2 for Android, XCUITest for iOS) the first time a run needs them, pinned, with the Node.js that runs them, as it downloads a browser driver for web apps. For iOS it also downloads WebDriverAgent, the app on the simulator that drives yours, as Appium builds it: nothing is built with Xcode, and nothing needs signing.

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
| `locale`, `timezone`, `location` | The language and region (`de-DE`), the time zone (`Europe/Berlin`), and where the device says it is (`51.3397, 12.3731`). |
| `host ports` (Android) | Ports of the machine axx runs on that the app reaches as `localhost` on the device, like `8400, 5500`: the app calls `http://localhost:8400` as on a developer's device, and gets the service there. Each scenario has only its own registration's. |
| `appium`, `capability.<name>` | An Appium server of your own or a device farm's, which runs the device, and the capabilities it takes. |

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

- **Android:** its data cleared (and with it its sign-in, its permissions and its notifications), the permissions its registration names granted, the notification shade and any dialog of the system's closed, the ports it reaches on this machine set, and the device's language, time zone and location set: an emulator goes back to where it starts when the registration names none. On the emulators axx starts, other apps that hang or crash show no dialog over yours, and Chrome opens a page at once, without its first-run screens.
- **iOS:** the app installed afresh (its data and notifications go with the old one), the simulator's keychain reset (a sign-in kept there outlives the app), its permissions reset and those its registration names granted, and the location set. It starts in the registration's language, region and time zone.

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

**Screenshots** are `<name>.<device>.png` in the project's `screenshots` folder, like `deliveries.android-parcels-pixel.png` or `deliveries.ios-iPhone-16-iOS-18-1.png`: each device has its own. The device's status and navigation bars, its clock among them, are left out. The first time, the step takes the screenshot and fails: look at it, keep it, and run again. `packs.mobile-core.screenshots` in `axx.yaml` sets the `folder`, the `tolerance` and `update: true`, which takes them again.

## Dialogs the system shows

```gherkin
Then the courier app's dialog shows "send you notifications"
When the courier app's dialog is dismissed
```

A dialog the system shows over the app, like a request for a permission it asked for, is accepted (allowed) or dismissed. iOS lets no one grant notifications ahead: an app that uses them asks, and the scenario answers. A dialog of the app's own is part of its screen: tap its buttons, like `the "Confirm" button is tapped in the courier app`.

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

`packs.mobile-android.emulatorArgs` passes more arguments to the emulators, and `bootTimeout` gives a slow machine longer than 3 minutes to boot one.

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

See the [mobile-core](/references/packs/mobile-core/), [mobile-android](/references/packs/mobile-android/) and [mobile-ios](/references/packs/mobile-ios/) references for every step.
