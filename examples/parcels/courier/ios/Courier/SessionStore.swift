// SPDX-License-Identifier: Apache-2.0

import Foundation
import Security

/// Keeps the courier's sign-in across launches, in the keychain: a generic
/// password of the service example.parcels.courier. Like every keychain item,
/// it outlives the app: reinstalled, the app finds the courier still signed in.
struct SessionStore {
    private let service = "example.parcels.courier"
    private let account = "sign-in"

    private var item: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    /// The sign-in the keychain keeps. How long the keychain took is logged with it: a read in
    /// the background once took 27 of the 35 seconds a background task had, on a busy Mac.
    func load() -> Session? {
        var query = item
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var found: CFTypeRef?
        let started = ContinuousClock.now
        let status = SecItemCopyMatching(query as CFDictionary, &found)
        let took = (ContinuousClock.now - started).formatted(.units(allowed: [.seconds, .milliseconds], width: .narrow))
        guard status == errSecSuccess, let data = found as? Data else {
            if status == errSecItemNotFound {
                log.notice("no sign-in in the keychain (it answered in \(took, privacy: .public))")
            } else {
                log.error("reading the sign-in from the keychain failed: \(status) (in \(took, privacy: .public))")
            }
            return nil
        }
        guard let session = try? JSONDecoder().decode(Session.self, from: data) else {
            log.error("the keychain's sign-in is unreadable")
            return nil
        }
        log.notice("signed in from the keychain as \(session.courier, privacy: .public) (read in \(took, privacy: .public))")
        return session
    }

    func save(_ session: Session) {
        guard let data = try? JSONEncoder().encode(session) else { return }
        SecItemDelete(item as CFDictionary)
        var add = item
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        let status = SecItemAdd(add as CFDictionary, nil)
        if status == errSecSuccess {
            log.notice("kept the sign-in of \(session.courier, privacy: .public) in the keychain")
        } else {
            log.error("keeping the sign-in in the keychain failed: \(status)")
        }
    }

    func clear() {
        let status = SecItemDelete(item as CFDictionary)
        if status != errSecSuccess && status != errSecItemNotFound {
            log.error("removing the sign-in from the keychain failed: \(status)")
        }
    }
}
