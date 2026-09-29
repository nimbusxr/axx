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
later) and the Android SDK (`ANDROID_HOME`, or `sdk.dir` in `local.properties`).

## The parcels service

The app calls the parcels service at `http://10.0.2.2:8400`: the host, as the Android emulator
sees it. A launch intent's `api_url` extra points it somewhere else until the app's process ends:

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
