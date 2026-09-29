// SPDX-License-Identifier: Apache-2.0

import Foundation

/// Where the parcels service is: http://127.0.0.1:8400, the Mac the simulator runs
/// on (it shares the Mac's network), or the launch's API_URL: the argument
/// `-API_URL <url>`, else the environment variable API_URL.
enum Backend {
    static let defaultURL = "http://127.0.0.1:8400"

    static let url: String = {
        let process = ProcessInfo.processInfo
        var chosen = process.environment["API_URL"]
        if let i = process.arguments.firstIndex(of: "-API_URL"), i + 1 < process.arguments.count {
            chosen = process.arguments[i + 1]
        }
        var url = chosen?.trimmingCharacters(in: .whitespaces) ?? ""
        while url.hasSuffix("/") { url.removeLast() }
        if url.isEmpty || URL(string: url) == nil { url = defaultURL }
        log.notice("the parcels service is at \(url, privacy: .public)")
        return url
    }()
}

/// A courier's sign-in, as the service answers it and the app keeps it.
struct Session: Codable, Equatable {
    let token: String
    let courier: String
    let name: String
}

struct Recipient: Decodable, Equatable {
    let name: String
    let street: String
    let city: String
    let postcode: String
    let country: String

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        street = try c.decodeIfPresent(String.self, forKey: .street) ?? ""
        city = try c.decodeIfPresent(String.self, forKey: .city) ?? ""
        postcode = try c.decodeIfPresent(String.self, forKey: .postcode) ?? ""
        country = try c.decodeIfPresent(String.self, forKey: .country) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case name, street, city, postcode, country
    }
}

/// A parcel out for delivery with the courier.
struct Delivery: Decodable, Equatable, Identifiable {
    let reference: String
    let serviceLevel: String
    let recipient: Recipient

    var id: String { reference }
    var address: String { "\(recipient.street), \(recipient.postcode) \(recipient.city)" }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        reference = try c.decode(String.self, forKey: .reference)
        serviceLevel = try c.decodeIfPresent(String.self, forKey: .serviceLevel) ?? ""
        recipient = try c.decode(Recipient.self, forKey: .recipient)
    }

    private enum CodingKeys: String, CodingKey {
        case reference, serviceLevel, recipient
    }
}

/// The service answered with a status other than 2xx.
struct APIError: Error {
    let status: Int
}

/// The couriers' endpoints of the parcels service.
struct CourierAPI {
    let baseURL: String

    func signIn(courier: String, pin: String) async throws -> Session {
        let data = try await call("POST", "/api/couriers/sign-in", token: nil, body: ["courier": courier, "pin": pin])
        return try JSONDecoder().decode(Session.self, from: data)
    }

    func deliveries(_ session: Session) async throws -> [Delivery] {
        struct Answer: Decodable { let deliveries: [Delivery] }
        let data = try await call("GET", "/api/couriers/\(enc(session.courier))/deliveries", token: session.token, body: nil)
        return try JSONDecoder().decode(Answer.self, from: data).deliveries
    }

    func markDelivered(_ session: Session, reference: String, signedBy: String) async throws {
        _ = try await call(
            "POST", "/api/couriers/\(enc(session.courier))/deliveries/\(enc(reference))",
            token: session.token, body: ["signedBy": signedBy]
        )
    }

    private func call(_ method: String, _ path: String, token: String?, body: [String: String]?) async throws -> Data {
        guard let url = URL(string: baseURL + path) else { throw URLError(.badURL) }
        var request = URLRequest(url: url, timeoutInterval: 15)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        log.notice("\(method, privacy: .public) \(path, privacy: .public): \(status)")
        guard (200...299).contains(status) else { throw APIError(status: status) }
        return data
    }

    private func enc(_ segment: String) -> String {
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove("/")
        return segment.addingPercentEncoding(withAllowedCharacters: allowed) ?? segment
    }
}
