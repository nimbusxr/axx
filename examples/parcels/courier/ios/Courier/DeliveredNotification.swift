// SPDX-License-Identifier: Apache-2.0

import UserNotifications

/// Asks for leave to tell the courier of the deliveries the app records: the
/// system shows its alert, and the courier allows it or not.
func askForNotifications() async {
    do {
        let allowed = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
        log.notice("notifications \(allowed ? "allowed" : "not allowed", privacy: .public)")
    } catch {
        log.error("asking for notifications failed: \(error.localizedDescription, privacy: .public)")
    }
}

/// Whether the courier has not yet allowed or refused notifications.
func notificationsUndecided() async -> Bool {
    await UNUserNotificationCenter.current().notificationSettings().authorizationStatus == .notDetermined
}

/// Tells the courier a delivery was recorded. Nothing is posted when the app
/// may not post notifications.
func notifyDelivered(reference: String, signedBy: String) async {
    let center = UNUserNotificationCenter.current()
    switch await center.notificationSettings().authorizationStatus {
    case .authorized, .provisional, .ephemeral:
        break
    default:
        log.notice("\(reference, privacy: .public) delivered; notifications are not allowed")
        return
    }
    let content = UNMutableNotificationContent()
    content.title = "\(reference) delivered"
    content.body = "Signed by \(signedBy)"
    content.sound = .default
    do {
        try await center.add(UNNotificationRequest(identifier: "delivered-\(reference)", content: content, trigger: nil))
        log.notice("notified \(reference, privacy: .public) delivered")
    } catch {
        log.error("notifying failed: \(error.localizedDescription, privacy: .public)")
    }
}
