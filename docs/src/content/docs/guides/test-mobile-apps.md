---
title: Test mobile apps
description: "Use your Android app the way people do, on emulators axx starts: tap and fill its controls by the names people see, open it with deep links, check what it shows; each scenario with a device to itself and a clean app."
---

Many acceptance criteria describe what a person does in an app: a courier signs in, taps a parcel and marks it delivered. The `mobile-android` pack does exactly that on Android emulators, driven by [Appium](https://appium.io), and a scenario can use every other pack too: it checks what your service did with the delivery, and what the parcel's tracking page shows.

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

Add the packs to the project with `axx pack add mobile-android` (it builds on `mobile-core`, the steps every mobile app has; [Choose packs](/guides/use-packs/)).

## What it needs

- **The Android SDK**, with `ANDROID_HOME` pointing at it: its emulator, and adb. Android Studio installs it, or its command-line tools.
- **An emulator's device (AVD)**, like the one Android Studio's Device Manager makes, or one `avdmanager` makes:

  ```sh
  sdkmanager --install "system-images;android-35;google_apis;arm64-v8a"   # x86_64 on Intel and AMD
  avdmanager create avd --name parcels-pixel --package "system-images;android-35;google_apis;arm64-v8a" --device pixel_8
  ```

- **Nothing else:** axx downloads [Appium](https://appium.io) and its UiAutomator2 driver the first time a run needs them, pinned, with the Node.js that runs them, as it downloads a browser driver for web apps.

## Register the app

```gherkin
Given the courier android app with the following properties:
  | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
  | device      | parcels-pixel                                                |
  | permissions | POST_NOTIFICATIONS                                           |
  | timezone    | Europe/Berlin                                                |
```

| Property | What it is |
| --- | --- |
| `apk` | The app, a file of the project: it is the app the scenario tests, installed the first time a device runs it. |
| `package`, `activity` | The app's package and its first screen: with no `apk`, an app the device has. |
| `device` | An emulator's device (AVD) axx starts, or a device adb lists, by its serial. |
| `permissions` | The permissions the app has from the start, like `POST_NOTIFICATIONS`: it has no other. |
| `locale`, `timezone`, `location` | The device's language and region (`de-DE`), time zone (`Europe/Berlin`), and where an emulator says it is (`51.3397, 12.3731`). |
| `appium`, `capability.<name>` | An Appium server of your own or a device farm's, which runs the device, and the capabilities it takes. |

The app starts when a step launches it or opens a link in it. Every step names the app, so a scenario can drive two, and a web app too.

## Each scenario has a device, and a clean app

A scenario leases a device for its whole run: no other scenario uses it meanwhile. axx starts emulators of the device as scenarios need them, read-only, so that several run at once and nothing a scenario changes outlives it, up to `devices` at once (1 by default: Android scenarios then take turns while the others run).

```yaml title="axx.yaml"
packs:
  mobile-android:
    devices: 2   # two emulators: two Android scenarios at once
```

Before its app starts, a scenario's app is reset: its data cleared (and with it its sign-in, its permissions and its notifications), the permissions its registration names granted, the notification shade closed, and the device's language, time zone and location set. There is no switch that skips it: a scenario is one journey, and that journey is its own. Its data stays unique as everywhere in axx: each scenario above has a courier and parcels of its own.

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

Controls are found by the names people see: a button by its text, a field by its label, a list item by one of its texts (its reference, say), an image by its description. The kinds are `button`, `field`, `checkbox`, `switch`, `tab`, `list item`, `image`, `text` and `element` (anything). Jetpack Compose and Android's views both work: a Compose control is found by the role Compose gives it. When no name tells a control apart, `id=` (its resource ID) or `xpath=` finds it. `${env:..}` values are secrets: masked in logs and failures.

`swiped down` pulls a list to refresh; `up`, `left` and `right` swipe too.

## Check what it shows

```gherkin
Then the courier app shows "Hello, Hanna Wolf"
Then the courier app does not show "PX-MOB-9401"
Then the "Mark delivered" button is disabled in the courier app
Then the "Courier ID" field in the courier app has the value "CR-LEJ-12"
Then the courier app shows a notification "PX-MOB-9411 delivered"
Then the courier app looks like the "deliveries" screenshot
```

Checks wait for the app, 10 seconds or `within {duration}`, as apps take their time. A failed step attaches what the app showed, and the scenario's failure says which device and which resets it had.

**Screenshots** are `<name>.<device>.png` in the project's `screenshots` folder, like `deliveries.android-parcels-pixel.png`: each device has its own. The device's status and navigation bars, its clock among them, are left out. The first time, the step takes the screenshot and fails: look at it, keep it, and run again. `packs.mobile-core.screenshots` in `axx.yaml` sets the `folder`, the `tolerance` and `update: true`, which takes them again.

## Dialogs the system shows

```gherkin
Then the courier app's dialog shows "send you notifications"
When the courier app's dialog is dismissed
```

A dialog Android shows over the app, like a request for a permission it asked for, is accepted (allowed) or dismissed. A dialog of the app's own is part of its screen: tap its buttons, like `the "Confirm" button is tapped in the courier app`.

## In CI

Linux runners start emulators with KVM, and draw them without a GPU. The parcels example's workflow does it this way, with the Android SDK the runner has:

```yaml
- name: KVM, for the emulator
  run: |
    echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | sudo tee /etc/udev/rules.d/99-kvm4all.rules
    sudo udevadm control --reload-rules
    sudo udevadm trigger --name-match=kvm
- run: ./create-emulator.sh   # sdkmanager and avdmanager: the device the features name
- run: axx run --tags @mobile
```

`packs.mobile-android.emulatorArgs` passes more arguments to the emulators, and `bootTimeout` gives a slow machine longer than 3 minutes to boot one.

See the [mobile-core](/references/packs/mobile-core/) and [mobile-android](/references/packs/mobile-android/) references for every step.
