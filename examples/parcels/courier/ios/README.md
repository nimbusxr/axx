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

## Logs

The app logs what it does under the subsystem `example.parcels.courier`:

```sh
xcrun simctl spawn booted log stream --predicate 'subsystem == "example.parcels.courier"'
```
