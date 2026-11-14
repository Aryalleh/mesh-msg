import Foundation
import Meshcore

struct CoreError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

/// The Go core (mobile/meshcore.go) behind Swift types. Every gomobile call
/// of the app goes through this file.
final class Core: NSObject, MeshcoreListenerProtocol {
    let raw: MeshcoreCore
    var onEvent: ((CoreEvent) -> Void)?

    init(dataDir: String, config: String) throws {
        var err: NSError?
        guard let c = MeshcoreOpen(dataDir, config, &err) else {
            throw CoreError(message: err?.localizedDescription ?? "the core did not open")
        }
        raw = c
        super.init()
        raw.setListener(self)
        raw.start()
    }

    func stop() {
        raw.setListener(nil)
        raw.stop()
    }

    // Called on a Go thread.
    func onEvent(_ json: String?) {
        guard let e = JSON.decode(CoreEvent.self, json) else { return }
        DispatchQueue.main.async { self.onEvent?(e) }
    }

    private func check(_ err: NSError?) throws {
        if let err { throw CoreError(message: err.localizedDescription) }
    }

    func status() -> NetStatus { JSON.decode(NetStatus.self, raw.status()) ?? NetStatus() }

    func chats() -> [Chat] {
        var err: NSError?
        return JSON.decode([Chat].self, raw.chats(&err)) ?? []
    }

    func messages(_ chat: String, limit: Int = 200) -> [Message] {
        var err: NSError?
        return JSON.decode([Message].self, raw.messages(chat, limit: limit, error: &err)) ?? []
    }

    func send(_ chat: String, _ text: String) throws {
        var err: NSError?
        _ = raw.send(chat, text: text, error: &err)
        try check(err)
    }

    func markRead(_ chat: String) { try? raw.markRead(chat) }

    func addContact(_ uri: String, name: String) throws {
        var err: NSError?
        _ = raw.addContact(uri, name: name, error: &err)
        try check(err)
    }

    func removeContact(_ hash: String) throws { try raw.removeContact(hash) }

    // ---- calls (docs/calls.md): Opus only on iPhone ----

    static let audioCodecs = "1"

    func startCall(_ contact: String) throws {
        var err: NSError?
        _ = raw.startCall(contact, codecs: Core.audioCodecs, error: &err)
        try check(err)
    }

    func answerCall(_ id: String) throws { try raw.answerCall(id, codecs: Core.audioCodecs) }
    func endCall(_ id: String) { try? raw.endCall(id) }
    func muteCall(_ id: String, _ muted: Bool) { try? raw.muteCall(id, muted: muted) }
    func callAudio(_ id: String, _ frame: Data) { try? raw.callAudio(id, frame: frame) }

    /// (result, frame): 0 a frame, 1 lost, 2 nothing to play.
    func pullCallAudio(_ id: String) -> (Int, Data) {
        guard let b = raw.pullCallAudio(id), !b.isEmpty else { return (2, Data()) }
        return (Int(b[b.startIndex]), b.dropFirst())
    }

    func currentCall() -> CallInfo? { JSON.decode(CallInfo.self, raw.currentCall()) }
}
