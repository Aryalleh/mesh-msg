import Foundation
import SwiftUI

/// The app's state: owns the core, the radios and what the screens show.
@MainActor
final class AppModel: ObservableObject {
    let settings = Settings()
    @Published var onboarded: Bool
    @Published var chats: [Chat] = []
    @Published var messages: [Message] = []
    @Published var status = NetStatus()
    @Published var call: CallInfo?
    @Published var error: String?
    @Published var nearby = 0
    @Published var loraState = ""

    private(set) var core: Core?
    private var ble: BleMesh?
    private var lora: LoRaBle?
    private var audio: CallAudio?
    private var openChat: String?
    private var timer: Timer?

    init() {
        onboarded = settings.onboarded
        if onboarded { start() }
    }

    private var dataDir: String {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("mesh")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        // The account key and messages: encrypted while the phone is locked.
        try? (dir as NSURL).setResourceValue(URLFileProtection.completeUntilFirstUserAuthentication, forKey: .fileProtectionKey)
        return dir.path
    }

    func finishOnboarding(name: String) {
        settings.displayName = name
        settings.onboarded = true
        onboarded = true
        start()
    }

    func start() {
        guard core == nil else { return }
        do {
            let c = try Core(dataDir: dataDir, config: settings.coreConfig())
            c.onEvent = { [weak self] e in self?.handle(e) }
            core = c
            if settings.bluetooth {
                ble = BleMesh(core: c) { [weak self] n in self?.nearby = n }
                ble?.start()
            }
            attachLoRa()
            reload()
            timer = Timer.scheduledTimer(withTimeInterval: 3, repeats: true) { [weak self] _ in
                Task { @MainActor in self?.refreshStatus() }
            }
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Settings that the core reads at start (name, relays) need a restart.
    func restart() {
        timer?.invalidate()
        ble?.stop()
        ble = nil
        lora?.stop()
        lora = nil
        core?.stop()
        core = nil
        start()
    }

    func attachLoRa() {
        lora?.stop()
        lora = nil
        loraState = ""
        guard let c = core, let id = settings.loraNode, let uuid = UUID(uuidString: id) else { return }
        lora = LoRaBle(core: c, node: uuid) { [weak self] s in self?.loraState = s }
        lora?.start()
    }

    private func handle(_ e: CoreEvent) {
        if e.type == "call" {
            updateCall(e.call)
            return
        }
        reload()
    }

    func reload() {
        guard let core else { return }
        chats = core.chats()
        if let c = openChat { messages = core.messages(c) }
        refreshStatus()
    }

    private func refreshStatus() {
        if let core { status = core.status() }
    }

    // ---- chats ----

    func open(_ chat: String) {
        openChat = chat
        messages = core?.messages(chat) ?? []
        core?.markRead(chat)
    }

    func close() {
        openChat = nil
        messages = []
    }

    func send(_ chat: String, _ text: String) {
        run { try self.core?.send(chat, text) }
        reload()
    }

    func addContact(_ uri: String, name: String) -> Bool {
        guard let core else { return false }
        do {
            try core.addContact(uri.trimmingCharacters(in: .whitespacesAndNewlines), name: name)
            reload()
            return true
        } catch {
            self.error = error.localizedDescription
            return false
        }
    }

    func removeContact(_ hash: String) {
        run { try self.core?.removeContact(hash) }
        reload()
    }

    private func run(_ f: () throws -> Void) {
        do { try f() } catch { self.error = error.localizedDescription }
    }

    func name(of hash: String) -> String {
        chats.first { $0.contact.hash == hash }?.title ?? String(hash.prefix(8))
    }

    // ---- calls ----

    func startCall(_ contact: String) {
        CallAudio.askMicrophone { ok in
            guard ok else { self.error = "Allow the microphone in Settings to make calls."; return }
            self.run { try self.core?.startCall(contact) }
        }
    }

    func answer() {
        guard let id = call?.id else { return }
        CallAudio.askMicrophone { ok in
            guard ok else { self.error = "Allow the microphone in Settings to answer."; return }
            self.run { try self.core?.answerCall(id) }
        }
    }

    func hangUp() {
        if let id = call?.id { core?.endCall(id) }
    }

    func mute(_ on: Bool) {
        if let id = call?.id { core?.muteCall(id, on) }
        audio?.muted = on
    }

    func speaker(_ on: Bool) { audio?.setSpeaker(on) }

    func dismissCall() {
        if call?.state == "ended" { call = nil }
    }

    private func updateCall(_ info: CallInfo?) {
        guard let info else { return }
        if let current = call, current.id != info.id, current.state != "ended" { return } // a second call: the core answers busy
        call = info
        if info.state == "active", audio == nil, let core {
            audio = CallAudio(core: core, id: info.id)
            audio?.start()
        }
        if info.state == "ended" {
            audio?.stop()
            audio = nil
        }
    }
}
