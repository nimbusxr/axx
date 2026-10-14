// SPDX-License-Identifier: Apache-2.0

import BackgroundTasks
import Foundation
import UserNotifications

/// The background task that reminds the courier of the deliveries left in their round. iOS runs
/// it now and then while the app is in the background, an hour apart at the soonest: it loads
/// today's deliveries from the parcels service and tells the courier how many are left, and
/// which is next.
let roundReminderTask = "example.parcels.courier.round-reminder"

enum RoundReminder {
    /// Tells iOS what the task does: before the app finishes launching, as iOS asks.
    static func register() {
        let registered = BGTaskScheduler.shared.register(forTaskWithIdentifier: roundReminderTask, using: nil) { task in
            schedule() // the next reminder
            let work = Task {
                await remind()
                task.setTaskCompleted(success: !Task.isCancelled)
            }
            task.expirationHandler = { work.cancel() }
        }
        if !registered { log.error("the round reminder could not be registered") }
    }

    /// Asks iOS to run the task in an hour or so, while the courier is signed in.
    static func schedule() {
        let request = BGAppRefreshTaskRequest(identifier: roundReminderTask)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 60 * 60)
        do {
            try BGTaskScheduler.shared.submit(request)
            log.notice("round reminder scheduled")
        } catch {
            log.error("scheduling the round reminder failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Stops the reminders: the courier signed out.
    static func cancel() {
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: roundReminderTask)
    }

    /// Tells the courier how many deliveries are left today, and which is next. Nothing is posted
    /// when no courier is signed in, none is left, or the app may not post notifications.
    static func remind() async {
        guard let session = SessionStore().load() else { return }
        let left: [Delivery]
        do {
            left = try await CourierAPI(baseURL: Backend.url).deliveries(session)
        } catch {
            log.error("the round reminder could not load the deliveries: \(error.localizedDescription, privacy: .public)")
            return
        }
        guard let next = left.first else { return }
        let center = UNUserNotificationCenter.current()
        switch await center.notificationSettings().authorizationStatus {
        case .authorized, .provisional, .ephemeral:
            break
        default:
            log.notice("\(left.count) deliveries left; notifications are not allowed")
            return
        }
        let content = UNMutableNotificationContent()
        content.title = left.count == 1 ? "1 delivery left today" : "\(left.count) deliveries left today"
        content.body = "Next: \(next.reference), \(next.address)"
        content.sound = .default
        do {
            try await center.add(UNNotificationRequest(identifier: "round-reminder", content: content, trigger: nil))
            log.notice("reminded the courier of \(left.count) deliveries left")
        } catch {
            log.error("the round reminder could not be posted: \(error.localizedDescription, privacy: .public)")
        }
    }
}
