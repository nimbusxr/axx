# 0020: A scroll is asked of the view; a swipe is a finger

- Status: Proposed
- Date: 2026-11-15

## Context

A step that taps or fills a control further down a screen, and the `scrolled into view` step,
bring the control into view first. On both phones axx did that with a finger: the UiAutomator2
server's scroll gesture on Android, drags of WebDriverAgent's on iOS. A finger on the glass is
slow, and it touches what is under it. What that cost, measured on a courier's report in a web
view (a form four screens long):

- **Time.** A tap on a radio button back up the page took 9 to 12 seconds on Android: a
  gesture a page (about a second), a reading of the screen after each, and two to three seconds
  at each end of the page to see that it no longer moved. Android's driver lists only what the
  screen shows, so the step could not know where the control was, and searched: down to the
  page's end, then back up.
- **What a finger hits.**
  - The server's scroll gesture puts its finger down where it sees fit, and that was the
    lowest part of the screen (y=2280 of 2400), from which Android takes a swipe up for "go
    home". A scenario ended on the home screen.
  - A swipe that starts near the top edge pulls the notifications down.
  - A swipe over a keyboard types. So the step closed the keyboard first, with Back, and a
    Back pressed with no keyboard up took the app a screen back, out of the page.

Android has another way to scroll, and so has iOS. A screen reader does not swipe: it asks the
view. Android's accessibility actions ask a view to scroll a page (`ACTION_SCROLL_FORWARD` and
the directions), or to bring one of its controls onto the screen (`ACTION_SHOW_ON_SCREEN`);
XCTest scrolls to an element itself before it taps one out of view. Measured on the same form:

| | Asked | Time |
| --- | --- | --- |
| Android 15, the button 1,826 px below the screen | `ACTION_SHOW_ON_SCREEN` | 0.1 s |
| Android 15, a page of the web view | `ACTION_SCROLL_DOWN` | 0.1 s, and "not offered" at the page's end |
| iOS 27.2, the button a screen and a half below | XCTest's scroll to an element | 0.06 s |

The UiAutomator2 server has no command for an accessibility action (its scroll commands are all
gestures), and Android gives its test connection (`UiAutomation`) to one process at a time, so a
second instrumentation of axx's cannot perform them beside it. WebDriverAgent has XCTest's
scroll to an element (`/wda/element/:uuid/scrollTo`); it refuses now and then, and has nothing
to work through in a SwiftUI scroll view of plain text.

## Decision

The two words mean two things, in the steps, the guide and the code.

- A **swipe** is a finger on the glass: the `swiped` step, a pull to refresh, a carousel.
- A **scroll** is the view moving because it was asked to. `scrolled into view`, and the reach
  of a tap or a fill, scroll.

A step scrolls by asking, and swipes only where a view cannot be asked.

**Android.** axx has an accessibility service of its own on the device, the scroll service
(`packs/mobile/android/scroll`, about 300 lines of Java):

- It performs "show this control" and "scroll this view a page that way" for the control axx
  names (its class, text, description, resource ID and place, as the screen's source gave them),
  waits until the view is at rest, and says whether anything moved.
- A release of axx builds it (`release.yml`'s `android-scroll` job), signs it with NimbusXR's
  key for it, and publishes it beside its archives, in its checksums: with them it has the
  release's signature and its build provenance. That release's axx downloads it, once, checked
  against the checksums, from GitHub or the mirror `AXX_DOWNLOAD_URL` names. An axx that is no
  release (built from a commit) has none to download, and builds it on the machine, with the
  SDK's build-tools and a JDK, signed with a key it makes there.
- axx installs it; names it in the device's `enabled_accessibility_services` beside what is
  there, for the run; and talks to it over adb, on a socket of the device.
- It is installed over adb, as the UiAutomator2 server and the app under test are. It is not
  an app of the Play Store, and needs nothing of Google's: Android asks only that an app is
  signed, and that its updates have the key of the one installed.
- The UiAutomator2 server is started with `DISABLE_SUPPRESS_ACCESSIBILITY_SERVICES`, its own
  switch for leaving other accessibility services running. Without it, Android pauses every
  service while the server has the device's accessibility.
- The session lists what the screen has out of view too (the driver's
  `allowInvisibleElements`), marked as not shown, where it is. A step then knows that a control
  is there, and has its view show it: no search.
- A list that makes its rows as it scrolls lists only the rows it has made. A row further on
  is looked for a page at a time, each page asked of the list.
- What the keyboard covers is not shown. A control under it is scrolled above it; where the
  view is at its end, the step closes the keyboard, with Back, pressed only while Android's
  input method service says the keyboard shows.

**iOS.** The step asks XCTest to scroll to the control first. What that leaves (a control it
refused, one still under the keyboard, a SwiftUI text) is done by the drags iOS had. XCTest
scrolls only to an element that exists, so a page of a list that makes its rows as it scrolls
is a swipe: a drag axx places, at a set speed and held at its end, a second or so a page.
(WebDriverAgent's own scroll took seven to eight seconds a page of a SwiftUI list.)

**Both.** The steps remember which controls went off the top as they paged down a screen, and
look up first for one of those.

**Where a view cannot be asked**, the step swipes: a device without the scroll service (axx has
none, which the run says once, with why; a device another Appium server drives), and a view
that offers no scroll action. On a device axx drives itself, that swipe is
a drag axx places: from a point to a point, held a moment so that nothing is flung, above the
lowest twelfth of the screen.

## Consequences

- **A second thing on the device.** A scenario's Android device has an accessibility service
  switched on while it runs, and other accessibility services a device has on keep running
  during a run, where the server used to pause them. An app that asks whether accessibility is
  on was already told yes, by the server.
- **An app can tell that a service runs, and a web view does.** With any accessibility service
  running, Android's web view reports its page as it does to a screen reader, with more of it:
  a text field's hint is its label and then its placeholder, where it was the label alone.
  Found after 0.7.1, in which a page's field with a placeholder was no longer found by its
  label. axx names such a field by the label the page shows before it, so a step names it the
  same way either way; a selector on the raw hint sees what the device has.
- **A file more in a release, and a key.** A release has the service to build and sign, and
  NimbusXR a key for it to keep (a keystore and its password, secrets of the release
  environment). A release cut without them signs with a key of its own and warns: axx installs
  the service afresh on a device that has one of another key.
- **A JDK and build-tools, for an axx that is no release.** Without them such an axx's run
  still passes, by swiping, and says so.
- **Device farms.** A device another Appium server drives has no scroll service and keeps the
  gestures.
- **The driver's selectors see the whole screen.** With what is out of view listed, an
  `xpath=` selector matches controls out of view too. A step that uses one scrolls to it; a
  check asks whether it shows.
- **Appium's server stays.** axx still installs it and reads the screen through it. The scroll
  service is the first part of a driver of axx's own, should axx come to carry one.
