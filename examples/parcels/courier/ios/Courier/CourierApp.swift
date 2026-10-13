// SPDX-License-Identifier: Apache-2.0

import SwiftUI
import UserNotifications
import os

/// What the app tells the system log, under the subsystem example.parcels.courier.
let log = Logger(subsystem: "example.parcels.courier", category: "app")

/// The couriers' app: sign in, today's deliveries, and a delivery. Launched with
/// an API_URL argument or environment variable, it talks to that parcels service;
/// opened with parcels-courier://deliveries/{reference}, it shows that delivery, with
/// parcels-courier://deliveries/next the next one, and with
/// parcels-courier://deliveries today's deliveries.
@main
struct CourierApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var model = CourierModel()

    var body: some Scene {
        WindowGroup {
            ContentView(model: model)
                .onAppear {
                    // Where a notification the courier opens leads: the delegate hands it on.
                    AppDelegate.open = { model.openDeepLink($0) }
                    if let target = AppDelegate.pending {
                        AppDelegate.pending = nil
                        model.openDeepLink(target)
                    }
                }
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

/// What parcels-courier://deliveries/{reference} links to, if the URL is that link: a
/// reference, "next", or "" for today's deliveries.
func deepLinkReference(_ url: URL) -> String? {
    guard url.scheme?.lowercased() == "parcels-courier", url.host == "deliveries" else { return nil }
    return url.pathComponents.first { $0 != "/" }?.trimmingCharacters(in: .whitespaces) ?? ""
}

/// The delivered notification's category: its action leads to the next delivery.
let deliveredCategory = "DELIVERED"
let nextDeliveryAction = "NEXT_DELIVERY"

/// Shows the app's notifications while it is in the foreground too, and opens what a
/// notification leads to: tapped, today's deliveries; its action, the next delivery.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    /// Opens a target of the app's links ("" for today's deliveries, "next"), once the app shows.
    @MainActor static var open: ((String) -> Void)?
    /// A target a notification opened before the app showed.
    @MainActor static var pending: String?

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        center.setNotificationCategories([
            UNNotificationCategory(
                identifier: deliveredCategory,
                actions: [UNNotificationAction(identifier: nextDeliveryAction, title: "Next delivery", options: [.foreground])],
                intentIdentifiers: []
            ),
        ])
        RoundReminder.register()
        if SessionStore().load() != nil { RoundReminder.schedule() }
        return true
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let target = response.actionIdentifier == nextDeliveryAction ? "next" : ""
        Task { @MainActor in
            log.notice("opened a notification, to \(target.isEmpty ? "today's deliveries" : target, privacy: .public)")
            if let open = AppDelegate.open { open(target) } else { AppDelegate.pending = target }
            completionHandler()
        }
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list])
    }
}
