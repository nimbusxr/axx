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

    func load() -> Session? {
        var query = item
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var found: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &found)
        guard status == errSecSuccess, let data = found as? Data else {
            if status == errSecItemNotFound {
                log.notice("no sign-in in the keychain")
            } else {
                log.error("reading the sign-in from the keychain failed: \(status)")
            }
            return nil
        }
        guard let session = try? JSONDecoder().decode(Session.self, from: data) else {
            log.error("the keychain's sign-in is unreadable")
            return nil
        }
        log.notice("signed in from the keychain as \(session.courier, privacy: .public)")
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
