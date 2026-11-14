import Foundation

/// What the user chose, kept in UserDefaults; the Go core gets it as JSON.
final class Settings {
    static let defaultRelays = [Relay(name: "eu-1", url: "https://eu-1.mfadem.ir")]
    private let d = UserDefaults.standard

    var onboarded: Bool {
        get { d.bool(forKey: "onboarded") }
        set { d.set(newValue, forKey: "onboarded") }
    }
    var displayName: String {
        get { d.string(forKey: "name") ?? "" }
        set { d.set(newValue, forKey: "name") }
    }
    var discovery: Bool {
        get { d.object(forKey: "discovery") as? Bool ?? true }
        set { d.set(newValue, forKey: "discovery") }
    }
    var bluetooth: Bool {
        get { d.object(forKey: "bluetooth") as? Bool ?? true }
        set { d.set(newValue, forKey: "bluetooth") }
    }
    /// The Bluetooth identifier of the paired LoRa node, if any.
    var loraNode: String? {
        get { d.string(forKey: "lora_node") }
        set { d.set(newValue, forKey: "lora_node") }
    }
    var relays: [Relay] {
        get {
            guard let data = d.data(forKey: "relays"), let r = try? JSONDecoder().decode([Relay].self, from: data) else {
                return Settings.defaultRelays
            }
            return r
        }
        set { d.set(try? JSONEncoder().encode(newValue), forKey: "relays") }
    }

    /// An iPhone is an endpoint, not a gateway: iOS stops apps in the background.
    func coreConfig() -> String {
        let cfg: [String: Any] = [
            "name": displayName,
            "delegate": true,
            "relays": relays.map { ["name": $0.name, "url": $0.url, "via": $0.via] },
            "gateway": ["enabled": false],
            "discovery": ["off": !discovery],
        ]
        let data = (try? JSONSerialization.data(withJSONObject: cfg)) ?? Data("{}".utf8)
        return String(decoding: data, as: UTF8.self)
    }
}
