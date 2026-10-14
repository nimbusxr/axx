// SPDX-License-Identifier: Apache-2.0

import CoreLocation
import Foundation
import Observation
import UniformTypeIdentifiers

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
    /// The check of the photos the courier attaches, loaded when the first comes.
    @ObservationIgnored private lazy var photos = Result { try PhotoCheck() }
    @ObservationIgnored private lazy var finder = Result { try ParcelFinder() }

    private(set) var session: Session?

    /// The deliveries opened over the list of today's deliveries: none while it shows.
    var path: [String] = [] {
        didSet {
            guard path != oldValue else { return }
            // A delivery just recorded leaves the list out of date.
            let stale = deliveries == nil || delivered
            delivered = false
            deliveryError = nil
            attached = []
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

    // The van's location, while the courier shares it.
    private(set) var sharingLocation = false
    @ObservationIgnored private lazy var van = VanLocation { [weak self] fix in self?.report(fix) }
    @ObservationIgnored private var reporting = false

    // A delivery.
    private(set) var posting = false
    private(set) var delivered = false
    private(set) var deliveryError: String?
    /// The documents the courier attached to the delivery, as the app tells of each.
    private(set) var attached: [String] = []
    private(set) var attaching = false
    /// The recipients' notes, as the courier reads them, by the note.
    @ObservationIgnored private var readNotes: [String: String] = [:]

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
                RoundReminder.schedule()
                deliveries = nil
                deliveriesError = nil
                let reference = pendingReference
                pendingReference = nil
                session = s
                if let reference, !reference.isEmpty {
                    openDeepLink(reference)
                } else {
                    path = []
                    refresh()
                }
            } catch let e as APIError {
                signInError = e.status == 401 ? Self.wrongSignIn : Self.unexpected(e)
            } catch {
                signInError = Self.unreachable
            }
        }
    }

    /// Shares the van's location with the parcels service, or stops. iOS asks the courier first, once.
    func shareLocation(_ on: Bool) {
        sharingLocation = on
        if on { van.start() } else { van.stop() }
    }

    private func report(_ fix: CLLocation) {
        guard let session, sharingLocation, !reporting else { return } // a fix on its way: the next one follows it
        reporting = true
        Task {
            defer { reporting = false }
            // A fix that does not get through is followed by the next one, a second later.
            try? await api.reportLocation(
                session, latitude: fix.coordinate.latitude, longitude: fix.coordinate.longitude,
                speed: fix.speed >= 0 ? fix.speed : nil, course: fix.course >= 0 ? fix.course : nil
            )
        }
    }

    func signOut() {
        shareLocation(false)
        store.clear()
        RoundReminder.cancel()
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
    /// parcels-courier://deliveries/{reference}: a delivery, the next one ("next"), or today's ("").
    func openDeepLink(_ reference: String) {
        if session == nil {
            pendingReference = reference
            return
        }
        switch reference {
        case "":
            path = []
            refresh()
        case "next":
            openNext()
        default:
            path = [reference]
            refresh()
        }
    }

    /// The first of today's deliveries not delivered yet, as the service has them now; the list when none is left.
    private func openNext() {
        guard let s = session else { return }
        Task {
            if let left = try? await api.deliveries(s) {
                deliveries = left
                path = left.first.map { [$0.reference] } ?? []
            } else {
                path = []
            }
        }
    }

    func backToDeliveries() {
        path = []
    }

    /// A recipient's delivery note, as the courier reads it (DeliveryNotes).
    func noteForCourier(_ note: String) async -> String {
        if let read = readNotes[note] { return read }
        let read = await DeliveryNotes.forCourier(note)
        readNotes[note] = read
        return read
    }

    /// Attaches a document the courier picked in the phone's files, like the recipient's
    /// safe-place authorization, to the delivery.
    func attachDocument(_ reference: String, from file: URL) {
        guard let s = session, !attaching else { return }
        attaching = true
        deliveryError = nil
        Task {
            defer { attaching = false }
            let name = file.lastPathComponent
            let scoped = file.startAccessingSecurityScopedResource()
            defer { if scoped { file.stopAccessingSecurityScopedResource() } }
            guard let content = try? Data(contentsOf: file) else {
                deliveryError = "\(name) cannot be read."
                return
            }
            let uti = UTType(filenameExtension: file.pathExtension)
            let type = uti?.preferredMIMEType ?? "application/octet-stream"
            var upload = content
            var done = "\(name) attached"
            let photo = content
            let image = uti?.conforms(to: .image) == true
            // A photo of a page, like the recipient's signed note, is a document.
            let page = image ? await Task.detached { (try? DeliveryPhoto.isDocument(photo)) ?? false }.value : false
            // A photo must show the parcel, where the courier leaves it.
            if image && !page {
                let check = photos
                let parcel = await Task.detached { (try? check.get().check(photo).parcel) ?? false }.value
                guard parcel else {
                    deliveryError = "\(name) shows no parcel: take a photo of the parcel where you leave it."
                    return
                }
                // What else it shows. A photo Vision cannot look at goes as it is.
                let seen = await Task.detached { try? DeliveryPhoto.look(at: photo) }.value
                if let other = seen?.reference, other != reference {
                    deliveryError = "\(name) shows the label of \(other): take a photo of \(reference)."
                    return
                }
                if seen?.dog == true {
                    deliveryError = "\(name) shows a dog beside the parcel: leave the parcel out of its reach, and take a photo of it there."
                    return
                }
                // The whole parcel, not cut off at the photo's edge.
                let finder = finder
                if await Task.detached(operation: { (try? finder.get().cutOff(photo)) ?? false }).value {
                    deliveryError = "\(name) shows the parcel cut off at its edge: step back and take a photo of the whole parcel."
                    return
                }
                // Nobody's face leaves the phone.
                if seen?.people == true {
                    guard let blurred = await Task.detached(operation: { try? DeliveryPhoto.blurringPeople(in: photo) }).value else {
                        deliveryError = "\(name) shows someone the phone cannot blur out: take a photo without them."
                        return
                    }
                    upload = blurred
                    done = "\(name) attached: the people in it are blurred out"
                }
            }
            await attach(s, reference, name: name, contentType: type, content: upload, done: done)
        }
    }

    /// Attaches the photo the courier took of the parcel where they left it, as a JPEG.
    func attachPhoto(_ reference: String, jpeg: Data) {
        guard let s = session, !attaching else { return }
        attaching = true
        deliveryError = nil
        Task {
            defer { attaching = false }
            let name = "photo-at-the-door.jpg"
            await attach(s, reference, name: name, contentType: "image/jpeg", content: jpeg, done: "\(name) attached")
        }
    }

    private func attach(_ s: Session, _ reference: String, name: String, contentType: String, content: Data, done: String) async {
        do {
            try await api.attachDocument(s, reference: reference, name: name, contentType: contentType, content: content)
            if path == [reference] { attached.append(done) }
        } catch let e as APIError {
            switch e.status {
            case 401: expired()
            case 404: deliveryError = Self.notWithYou(reference)
            case 413: deliveryError = "\(name) is too large to attach."
            default: deliveryError = Self.unexpected(e)
            }
        } catch {
            deliveryError = Self.unreachable
        }
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

    func saveNote(_ reference: String, note: String) async -> Bool {
        guard let session else { return false }
        do {
            try await api.saveNote(session, reference: reference, note: note)
            return true
        } catch {
            deliveryError = "The note could not be saved. Please try again."
            return false
        }
    }

    /// No one home: the attempt, recorded with what the phone heard at the door.
    func recordAttempt(_ reference: String, heard: String) async -> Bool {
        guard let session else { return false }
        do {
            try await api.recordAttempt(session, reference: reference, heard: heard)
            return true
        } catch {
            deliveryError = "The attempt could not be recorded. Please try again."
            return false
        }
    }
}

/// The van's location from the phone's GPS, every second while the courier shares it: where it
/// is, how fast it goes and which way.
final class VanLocation: NSObject, CLLocationManagerDelegate {
    private let manager = CLLocationManager()
    private let onFix: (CLLocation) -> Void
    private var wanted = false

    init(onFix: @escaping (CLLocation) -> Void) {
        self.onFix = onFix
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyBestForNavigation
        manager.activityType = .automotiveNavigation
    }

    func start() {
        wanted = true
        if manager.authorizationStatus == .notDetermined {
            manager.requestWhenInUseAuthorization() // starts once the courier answers
        } else {
            manager.startUpdatingLocation()
        }
    }

    func stop() {
        wanted = false
        manager.stopUpdatingLocation()
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        if wanted, manager.authorizationStatus == .authorizedWhenInUse || manager.authorizationStatus == .authorizedAlways {
            manager.startUpdatingLocation()
        }
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        locations.forEach(onFix)
    }
}
