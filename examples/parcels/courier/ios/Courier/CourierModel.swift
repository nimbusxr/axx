// SPDX-License-Identifier: Apache-2.0

import Foundation
import Observation

/// What the app shows, and what the courier does in it.
@MainActor
@Observable
final class CourierModel {
    static let wrongSignIn = "The courier ID or the PIN is wrong."
    static let unreachable = "The parcels service cannot be reached."

    static func notWithYou(_ reference: String) -> String { "\(reference) is not out for delivery with you." }

    private static func unexpected(_ error: APIError) -> String { "The parcels service answered \(error.status)." }

    @ObservationIgnored private let store = SessionStore()
    @ObservationIgnored private let api = CourierAPI(baseURL: Backend.url)

    private(set) var session: Session?

    /// The deliveries opened over the list of today's deliveries: none while it shows.
    var path: [String] = [] {
        didSet {
            guard path != oldValue else { return }
            // A delivery just recorded leaves the list out of date.
            let stale = deliveries == nil || delivered
            delivered = false
            deliveryError = nil
            if path.isEmpty && stale { refresh() }
        }
    }

    // Sign in.
    private(set) var signingIn = false
    private(set) var signInError: String?

    /// Whether the app asked for leave to post notifications while it runs.
    @ObservationIgnored private var askedForNotifications = false

    /// A delivery a deep link asked for before the courier signed in.
    @ObservationIgnored private var pendingReference: String?

    // Today's deliveries: nil until they are loaded.
    private(set) var deliveries: [Delivery]?
    private(set) var refreshing = false
    private(set) var deliveriesError: String?
    @ObservationIgnored private var refreshTask: Task<Void, Never>?

    // A delivery.
    private(set) var posting = false
    private(set) var delivered = false
    private(set) var deliveryError: String?

    init() {
        session = store.load()
        if session != nil { refresh() }
    }

    func signIn(courier: String, pin: String) {
        if signingIn { return }
        signingIn = true
        signInError = nil
        Task {
            defer { signingIn = false }
            do {
                let s = try await api.signIn(courier: courier, pin: pin)
                store.save(s)
                deliveries = nil
                deliveriesError = nil
                let reference = pendingReference
                pendingReference = nil
                path = reference.map { [$0] } ?? []
                session = s
                refresh()
            } catch let e as APIError {
                signInError = e.status == 401 ? Self.wrongSignIn : Self.unexpected(e)
            } catch {
                signInError = Self.unreachable
            }
        }
    }

    func signOut() {
        store.clear()
        refreshTask?.cancel()
        refreshTask = nil
        refreshing = false
        session = nil
        deliveries = nil
        deliveriesError = nil
        signInError = nil
        pendingReference = nil
        path = []
    }

    /// The sign-in expired: the courier signs in again.
    private func expired() {
        signOut()
    }

    func refresh() {
        guard let s = session else { return }
        refreshTask?.cancel()
        refreshing = true
        refreshTask = Task {
            do {
                let loaded = try await api.deliveries(s)
                if Task.isCancelled { return }
                deliveries = loaded
                deliveriesError = nil
            } catch let e as APIError {
                if Task.isCancelled { return }
                if e.status == 401 { expired() } else { deliveriesError = Self.unexpected(e) }
            } catch {
                if Task.isCancelled { return }
                deliveriesError = Self.unreachable
            }
            refreshing = false
        }
    }

    /// Reloads today's deliveries, and returns once they are: pulling the list down.
    func reload() async {
        refresh()
        await refreshTask?.value
    }

    /// Asks, once while the app runs, for leave to post notifications, if the
    /// courier has not decided yet.
    func askForNotificationsOnce() async {
        if askedForNotifications || session == nil { return }
        guard await notificationsUndecided(), !askedForNotifications else { return }
        askedForNotifications = true
        await askForNotifications()
    }

    /// parcels-courier://deliveries/{reference}: after signing in, if the courier has not.
    func openDeepLink(_ reference: String) {
        if session == nil {
            pendingReference = reference
            return
        }
        path = [reference]
        refresh()
    }

    func backToDeliveries() {
        path = []
    }

    func markDelivered(_ reference: String, signedBy: String) {
        guard let s = session, !posting else { return }
        posting = true
        deliveryError = nil
        Task {
            defer { posting = false }
            do {
                try await api.markDelivered(s, reference: reference, signedBy: signedBy)
                // "Delivered" shows until the courier goes back to the list, which
                // reloads then; a courier who went back already gets it reloaded now.
                if path == [reference] { delivered = true } else { refresh() }
                await notifyDelivered(reference: reference, signedBy: signedBy)
            } catch let e as APIError {
                switch e.status {
                case 401: expired()
                case 404: deliveryError = Self.notWithYou(reference)
                case 409: deliveryError = "\(reference) is not out for delivery."
                default: deliveryError = Self.unexpected(e)
                }
            } catch {
                deliveryError = Self.unreachable
            }
        }
    }
}
