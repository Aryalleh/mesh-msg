import CoreImage.CIFilterBuiltins
import SwiftUI

struct OnboardingView: View {
    @EnvironmentObject var model: AppModel
    @State private var name = ""

    var body: some View {
        VStack(spacing: 20) {
            Spacer()
            Image(systemName: "dot.radiowaves.left.and.right").font(.system(size: 56)).foregroundStyle(.tint)
            Text("Messages without the internet").font(.title2.bold()).multilineTextAlignment(.center)
            Text("Mava passes encrypted messages from phone to phone over Bluetooth, over LoRa radios, and through relays when any internet exists.")
                .multilineTextAlignment(.center).foregroundStyle(.secondary)
            TextField("Your name (optional)", text: $name).textFieldStyle(.roundedBorder)
            Button("Start") { model.finishOnboarding(name: name.trimmingCharacters(in: .whitespaces)) }
                .buttonStyle(.borderedProminent).controlSize(.large)
            Spacer()
        }
        .padding(24)
    }
}

struct ChatsView: View {
    @EnvironmentObject var model: AppModel
    @State private var adding = false

    var body: some View {
        NavigationStack {
            List {
                if model.chats.isEmpty {
                    Text("No chats yet. Add a contact with their link.").foregroundStyle(.secondary)
                }
                ForEach(model.chats) { chat in
                    NavigationLink(value: chat) { ChatRow(chat: chat) }
                        .swipeActions {
                            if !chat.isGroup {
                                Button("Remove", role: .destructive) { model.removeContact(chat.contact.hash) }
                            }
                        }
                }
            }
            .navigationTitle("Mava")
            .navigationDestination(for: Chat.self) { ChatView(chat: $0) }
            .toolbar {
                ToolbarItem(placement: .navigationBarLeading) {
                    NavigationLink { SettingsView() } label: { Image(systemName: "gearshape") }.accessibilityLabel("Settings")
                }
                ToolbarItemGroup(placement: .navigationBarTrailing) {
                    NavigationLink { NetworkView() } label: { Image(systemName: "antenna.radiowaves.left.and.right") }.accessibilityLabel("Network")
                    Button { adding = true } label: { Image(systemName: "plus") }.accessibilityLabel("Add contact")
                }
            }
            .sheet(isPresented: $adding) { AddContactView() }
        }
    }
}

struct ChatRow: View {
    let chat: Chat

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                Text(chat.title).font(.headline)
                if let last = chat.last {
                    Text(last.media != nil && last.text.isEmpty ? "File" : last.text).lineLimit(1).foregroundStyle(.secondary)
                }
            }
            Spacer()
            if chat.unread > 0 {
                Text("\(chat.unread)").font(.caption.bold()).padding(.horizontal, 8).padding(.vertical, 3)
                    .background(Capsule().fill(.tint)).foregroundStyle(.white)
            }
        }
    }
}

struct ChatView: View {
    @EnvironmentObject var model: AppModel
    let chat: Chat
    @State private var draft = ""

    var body: some View {
        VStack(spacing: 0) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 6) {
                        ForEach(model.messages) { m in Bubble(message: m, group: chat.isGroup).id(m.id) }
                    }
                    .padding(12)
                }
                .onChange(of: model.messages.count) { _ in
                    if let last = model.messages.last { proxy.scrollTo(last.id, anchor: .bottom) }
                }
                .onAppear {
                    if let last = model.messages.last { proxy.scrollTo(last.id, anchor: .bottom) }
                }
            }
            Divider()
            HStack(alignment: .bottom) {
                TextField("Message", text: $draft, axis: .vertical).lineLimit(1...5).textFieldStyle(.roundedBorder)
                Button {
                    let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !text.isEmpty else { return }
                    model.send(chat.id, text)
                    draft = ""
                } label: { Image(systemName: "arrow.up.circle.fill").font(.title) }
                    .accessibilityLabel("Send")
            }
            .padding(10)
        }
        .navigationTitle(chat.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !chat.isGroup {
                Button { model.startCall(chat.contact.hash) } label: { Image(systemName: "phone") }.accessibilityLabel("Voice call")
            }
        }
        .onAppear { model.open(chat.id) }
        .onDisappear { model.close() }
    }
}

struct Bubble: View {
    let message: Message
    let group: Bool

    var body: some View {
        HStack {
            if message.outgoing { Spacer(minLength: 40) }
            VStack(alignment: .leading, spacing: 3) {
                if group, !message.outgoing, let from = message.from_name, !from.isEmpty {
                    Text(from).font(.caption.bold()).foregroundStyle(.tint)
                }
                if let media = message.media {
                    Label(media.name.isEmpty ? "File" : media.name, systemImage: "doc").font(.subheadline)
                }
                if !message.text.isEmpty { Text(message.text) }
                HStack(spacing: 4) {
                    Text(message.sent, style: .time)
                    if message.outgoing {
                        Image(systemName: message.read == true ? "checkmark.circle.fill" : message.delivered ? "checkmark.circle" : "clock")
                    }
                }
                .font(.caption2).foregroundStyle(.secondary)
            }
            .padding(.horizontal, 12).padding(.vertical, 8)
            .background(RoundedRectangle(cornerRadius: 16).fill(message.outgoing ? Color.accentColor.opacity(0.2) : Color(.secondarySystemBackground)))
            if !message.outgoing { Spacer(minLength: 40) }
        }
    }
}

struct AddContactView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var link = ""
    @State private var name = ""

    var body: some View {
        NavigationStack {
            Form {
                Section("Their link") {
                    TextField("meshmsg:…", text: $link, axis: .vertical).textInputAutocapitalization(.never).autocorrectionDisabled()
                    Button("Paste") { link = UIPasteboard.general.string ?? link }
                    TextField("Name", text: $name)
                    Button("Add") { if model.addContact(link, name: name) { dismiss() } }.disabled(link.isEmpty)
                }
                Section("Your link") {
                    if let qr = qrCode(model.status.uri) {
                        Image(uiImage: qr).interpolation(.none).resizable().scaledToFit().frame(maxWidth: 220).frame(maxWidth: .infinity)
                    }
                    Text(model.status.uri).font(.footnote.monospaced()).textSelection(.enabled)
                    Button("Copy") { UIPasteboard.general.string = model.status.uri }
                    ShareLink(item: model.status.uri)
                }
            }
            .navigationTitle("Add contact")
            .toolbar { Button("Close") { dismiss() } }
        }
    }

    private func qrCode(_ text: String) -> UIImage? {
        guard !text.isEmpty else { return nil }
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        guard let out = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)),
              let cg = CIContext().createCGImage(out, from: out.extent) else { return nil }
        return UIImage(cgImage: cg)
    }
}

struct NetworkView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        List {
            Section("Nearby phones") {
                Label(model.nearby == 0 ? "Looking for phones around you" : "\(model.nearby) connected", systemImage: "iphone.radiowaves.left.and.right")
            }
            Section("LoRa node") {
                Label(model.settings.loraNode == nil ? "No node. Pair one in Settings." : (model.loraState.isEmpty ? "Connecting" : model.loraState), systemImage: "antenna.radiowaves.left.and.right")
            }
            Section("Links") {
                ForEach(model.status.links.keys.sorted().filter { $0 != "core" && $0 != "direct" }, id: \.self) { k in
                    HStack {
                        Text(k)
                        Spacer()
                        Text(model.status.links[k] ?? "").foregroundStyle(.secondary)
                    }
                }
            }
            Section("This phone") {
                LabeledContent("Known people and nodes", value: "\(model.status.routes)")
                LabeledContent("Messages waiting", value: "\(model.status.stored)")
            }
        }
        .navigationTitle("Network")
    }
}

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @State private var name = ""
    @State private var relayName = ""
    @State private var relayURL = ""
    @State private var relays: [Relay] = []
    @State private var bluetooth = true
    @State private var picking = false

    var body: some View {
        Form {
            Section("Your name") {
                TextField("Name", text: $name)
            }
            Section("Bluetooth") {
                Toggle("Mesh with phones around me", isOn: $bluetooth)
            }
            Section {
                if let id = model.settings.loraNode {
                    LabeledContent("Paired", value: String(id.prefix(8)))
                    Button("Forget node", role: .destructive) {
                        model.settings.loraNode = nil
                        model.attachLoRa()
                    }
                } else {
                    Button("Pair a LoRa node over Bluetooth") { picking = true }
                }
            } header: {
                Text("LoRa node")
            } footer: {
                Text("An iPhone can't use a node on a cable. Use a node with Bluetooth (Heltec V3, T-Beam, T-Deck and similar).")
            }
            Section("Relays") {
                ForEach(relays) { r in
                    VStack(alignment: .leading) {
                        Text(r.name)
                        Text(r.url).font(.caption).foregroundStyle(.secondary)
                    }
                }
                .onDelete { relays.remove(atOffsets: $0) }
                TextField("Name", text: $relayName).textInputAutocapitalization(.never)
                TextField("https://…", text: $relayURL).textInputAutocapitalization(.never).autocorrectionDisabled().keyboardType(.URL)
                Button("Add relay") {
                    relays.removeAll { $0.name == relayName }
                    relays.append(Relay(name: relayName, url: relayURL))
                    relayName = ""
                    relayURL = ""
                }
                .disabled(relayName.isEmpty || !(relayURL.hasPrefix("https://") || relayURL.hasPrefix("http://")))
            }
            Section {
                Button("Save and reconnect") {
                    model.settings.displayName = name
                    model.settings.relays = relays
                    model.settings.bluetooth = bluetooth
                    model.restart()
                }
            }
        }
        .navigationTitle("Settings")
        .sheet(isPresented: $picking) { NodePicker().environmentObject(model) }
        .onAppear {
            name = model.settings.displayName
            relays = model.settings.relays
            bluetooth = model.settings.bluetooth
        }
    }
}

struct NodePicker: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @StateObject private var scanner = LoRaScanner()

    var body: some View {
        NavigationStack {
            List {
                if scanner.found.isEmpty { Text("Looking for LoRa nodes. Turn the node on and keep it close.").foregroundStyle(.secondary) }
                ForEach(scanner.found) { n in
                    Button(n.name) {
                        model.settings.loraNode = n.id.uuidString
                        model.attachLoRa()
                        dismiss()
                    }
                }
            }
            .navigationTitle("LoRa nodes")
            .toolbar { Button("Close") { dismiss() } }
            .onAppear { scanner.start() }
            .onDisappear { scanner.stop() }
        }
    }
}

struct CallView: View {
    @EnvironmentObject var model: AppModel
    @State private var muted = false
    @State private var speaker = false

    var body: some View {
        let call = model.call ?? CallInfo()
        VStack(spacing: 18) {
            Spacer()
            Text(model.name(of: call.peer)).font(.largeTitle.bold())
            Text(stateText(call)).foregroundStyle(.secondary).multilineTextAlignment(.center)
            if call.state == "active", let code = call.code, !code.isEmpty {
                Text(code).font(.title.monospaced())
                Text("Read this code to each other. If it matches, nobody is listening in.").font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            if call.relayed == true { Text("This call goes through your relay (no direct path was found).").font(.footnote) }
            Spacer()
            if call.state == "ended" {
                Button("Close") { model.dismissCall() }.buttonStyle(.bordered).controlSize(.large)
            } else if call.state == "ringing" {
                HStack(spacing: 48) {
                    round("phone.down.fill", .red, "Decline") { model.hangUp() }
                    round("phone.fill", .green, "Answer") { model.answer() }
                }
            } else {
                HStack(spacing: 32) {
                    round(muted ? "mic.slash.fill" : "mic.fill", .gray, "Mute") { muted.toggle(); model.mute(muted) }
                    round("phone.down.fill", .red, "Hang up") { model.hangUp() }
                    round(speaker ? "speaker.wave.3.fill" : "speaker.fill", .gray, "Speaker") { speaker.toggle(); model.speaker(speaker) }
                }
            }
        }
        .padding(32)
    }

    private func round(_ icon: String, _ color: Color, _ label: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: icon).font(.title).foregroundStyle(.white).frame(width: 68, height: 68).background(Circle().fill(color))
        }
        .accessibilityLabel(label)
    }

    private func stateText(_ c: CallInfo) -> String {
        switch c.state {
        case "outgoing": return "Calling…"
        case "ringing": return "Incoming voice call"
        case "connecting": return "Connecting…"
        case "active": return c.peer_muted == true ? "They muted their microphone" : "Call in progress"
        default:
            switch c.reason ?? "" {
            case "declined": return "Declined"
            case "busy": return "Busy on another call"
            case "no direct path": return "No direct connection could be made. Try Wi-Fi, or another network."
            case "missed", "no answer": return "No answer"
            case "connection lost": return "Connection lost"
            default: return "Call ended"
            }
        }
    }
}
