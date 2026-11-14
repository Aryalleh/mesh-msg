import Foundation

// JSON shapes of the Go core (core/node, core/call). Unknown fields are ignored.

struct Contact: Codable, Hashable, Identifiable {
    var hash: String = ""
    var name: String = ""
    var uri: String = ""
    var id: String { hash }
}

struct GroupInfo: Codable, Hashable {
    var hash: String = ""
    var name: String = ""
}

struct MediaInfo: Codable, Hashable {
    var id: String = ""
    var mime: String = ""
    var name: String = ""
    var size: Int = 0
    var state: String = ""
}

struct Message: Codable, Hashable, Identifiable {
    var id: String = ""
    var peer: String = ""
    var outgoing: Bool = false
    var text: String = ""
    var sent: Date = .distantPast
    var delivered: Bool = false
    var read: Bool?
    var group: Bool?
    var from_name: String?
    var media: MediaInfo?
}

struct Chat: Codable, Hashable, Identifiable {
    var contact: Contact = Contact()
    var group: GroupInfo?
    var last: Message?
    var unread: Int = 0
    var id: String { group?.hash ?? contact.hash }
    var title: String { group?.name ?? (contact.name.isEmpty ? String(contact.hash.prefix(8)) : contact.name) }
    var isGroup: Bool { group != nil }
}

struct NetStatus: Codable {
    var hash: String = ""
    var uri: String = ""
    var links: [String: String] = [:]
    var routes: Int = 0
    var stored: Int = 0
    var contacts: Int = 0
}

struct CallInfo: Codable, Equatable {
    var id: String = ""
    var peer: String = ""
    var incoming: Bool = false
    var state: String = ""
    var codec: Int?
    var code: String?
    var relayed: Bool?
    var reason: String?
    var missed: Bool?
    var peer_muted: Bool?
}

struct CoreEvent: Codable {
    var type: String = ""
    var message: Message?
    var call: CallInfo?
    var info: String?
}

struct Relay: Codable, Hashable, Identifiable {
    var name: String
    var url: String
    var via: String = "direct"
    var id: String { name }
}

enum JSON {
    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        // Go writes RFC 3339 with or without fractional seconds.
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            let f = ISO8601DateFormatter()
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let t = f.date(from: s) { return t }
            f.formatOptions = [.withInternetDateTime]
            return f.date(from: s) ?? .distantPast
        }
        return d
    }()

    static func decode<T: Decodable>(_ type: T.Type, _ s: String?) -> T? {
        guard let s, !s.isEmpty, s != "null", let data = s.data(using: .utf8) else { return nil }
        return try? decoder.decode(type, from: data)
    }
}
