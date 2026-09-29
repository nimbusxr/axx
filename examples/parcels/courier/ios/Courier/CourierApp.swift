// SPDX-License-Identifier: Apache-2.0

import SwiftUI
import UserNotifications
import os

/// What the app tells the system log, under the subsystem example.parcels.courier.
let log = Logger(subsystem: "example.parcels.courier", category: "app")

/// The couriers' app: sign in, today's deliveries, and a delivery. Launched with
/// an API_URL argument or environment variable, it talks to that parcels service;
/// opened with parcels-courier://deliveries/{reference}, it shows that delivery.
@main
struct CourierApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var model = CourierModel()

    var body: some Scene {
        WindowGroup {
            ContentView(model: model)
                .onOpenURL { url in
                    guard let reference = deepLinkReference(url) else {
                        log.notice("ignored the link \(url.absoluteString, privacy: .public)")
                        return
                    }
                    log.notice("opened the link to \(reference, privacy: .public)")
                    model.openDeepLink(reference)
                }
        }
    }
}

/// The reference in parcels-courier://deliveries/{reference}, if the URL is that link.
func deepLinkReference(_ url: URL) -> String? {
    guard url.scheme?.lowercased() == "parcels-courier", url.host == "deliveries" else { return nil }
    let reference = url.pathComponents.first { $0 != "/" }?.trimmingCharacters(in: .whitespaces)
    guard let reference, !reference.isEmpty else { return nil }
    return reference
}

/// Shows the app's notifications while it is in the foreground too.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list])
    }
}
