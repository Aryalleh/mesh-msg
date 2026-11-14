import CoreBluetooth
import Foundation
import Meshcore

/// GATT service of a LoRa node's host link (docs/firmware.md §6): the same
/// byte stream as the USB serial link, written to RX and notified on TX.
enum LoRaGatt {
    static let service = CBUUID(string: "6d657368-6c72-0000-8000-00805f9b34fb")
    static let rx = CBUUID(string: "6d657368-6c72-0001-8000-00805f9b34fb")
    static let tx = CBUUID(string: "6d657368-6c72-0002-8000-00805f9b34fb")
}

struct FoundNode: Identifiable, Hashable {
    let id: UUID
    let name: String
}

/// Finds LoRa nodes that offer the host link over Bluetooth.
final class LoRaScanner: NSObject, ObservableObject, CBCentralManagerDelegate {
    @Published var found: [FoundNode] = []
    private var central: CBCentralManager?

    func start() { central = CBCentralManager(delegate: self, queue: .main) }

    func stop() {
        central?.stopScan()
        central = nil
    }

    func centralManagerDidUpdateState(_ c: CBCentralManager) {
        if c.state == .poweredOn { c.scanForPeripherals(withServices: [LoRaGatt.service]) }
    }

    func centralManager(_ c: CBCentralManager, didDiscover p: CBPeripheral, advertisementData ad: [String: Any], rssi: NSNumber) {
        let name = (ad[CBAdvertisementDataLocalNameKey] as? String) ?? p.name ?? "LoRa node"
        if !found.contains(where: { $0.id == p.identifier }) { found.append(FoundNode(id: p.identifier, name: name)) }
    }
}

/// A LoRa node over Bluetooth for the core: `Core.attachLoRa` opens it as a
/// byte stream and speaks the host link over it (core/hostlink), exactly as
/// Android does with a node on a USB cable.
final class LoRaBle: NSObject, MeshcoreStreamOpenerProtocol, MeshcoreStreamProtocol, CBCentralManagerDelegate, CBPeripheralDelegate {
    static let name = "lora-ble"
    private let core: Core
    private let node: UUID
    private let onState: (String) -> Void
    private var central: CBCentralManager?
    private var peripheral: CBPeripheral?
    private var rx: CBCharacteristic?

    // The core reads and writes on its own threads.
    private let lock = NSCondition()
    private var inbox = Data()
    private var connected = false
    private var closed = false

    init(core: Core, node: UUID, onState: @escaping (String) -> Void) {
        self.core = core
        self.node = node
        self.onState = onState
    }

    func start() {
        central = CBCentralManager(delegate: self, queue: .main)
        try? core.raw.attachLoRa(LoRaBle.name, opener: self)
    }

    func stop() {
        core.raw.detachLoRa(LoRaBle.name)
        lock.lock()
        closed = true
        connected = false
        lock.broadcast()
        lock.unlock()
        if let p = peripheral { central?.cancelPeripheralConnection(p) }
        central = nil
    }

    // ---- stream for the core ----

    func open() throws -> MeshcoreStreamProtocol {
        lock.lock()
        defer { lock.unlock() }
        let deadline = Date().addingTimeInterval(15)
        while !connected && !closed {
            if !lock.wait(until: deadline) { throw CoreError(message: "LoRa node not in reach") }
        }
        if closed { throw CoreError(message: "LoRa node removed") }
        inbox.removeAll()
        return self
    }

    func read() throws -> Data {
        lock.lock()
        defer { lock.unlock() }
        while inbox.isEmpty && connected && !closed { lock.wait() }
        if inbox.isEmpty { throw CoreError(message: "LoRa node disconnected") }
        let out = inbox
        inbox = Data()
        return out
    }

    func write(_ b: Data?) throws {
        guard let b else { return }
        let copy = Data(b)
        lock.lock()
        let ok = connected && !closed
        lock.unlock()
        if !ok { throw CoreError(message: "LoRa node disconnected") }
        DispatchQueue.main.async {
            guard let p = self.peripheral, let rx = self.rx else { return }
            let size = max(20, p.maximumWriteValueLength(for: .withoutResponse))
            var at = 0
            while at < copy.count {
                let end = min(at + size, copy.count)
                p.writeValue(copy.subdata(in: at..<end), for: rx, type: .withoutResponse)
                at = end
            }
        }
    }

    func close() throws {
        // The core closes the stream after an error and opens it again.
        lock.lock()
        inbox.removeAll()
        lock.unlock()
    }

    // ---- Bluetooth ----

    func centralManagerDidUpdateState(_ c: CBCentralManager) {
        guard c.state == .poweredOn else { return onState("Bluetooth is off") }
        connect()
    }

    private func connect() {
        guard let c = central else { return }
        if let p = c.retrievePeripherals(withIdentifiers: [node]).first {
            peripheral = p
            p.delegate = self
            onState("Connecting")
            c.connect(p) // waits until the node is in reach
        } else {
            onState("Node not found: pair it again")
        }
    }

    func centralManager(_ c: CBCentralManager, didConnect p: CBPeripheral) {
        p.discoverServices([LoRaGatt.service])
    }

    func centralManager(_ c: CBCentralManager, didDisconnectPeripheral p: CBPeripheral, error: Error?) {
        lock.lock()
        connected = false
        lock.broadcast()
        lock.unlock()
        rx = nil
        onState("Disconnected")
        if central != nil { connect() }
    }

    func peripheral(_ p: CBPeripheral, didDiscoverServices error: Error?) {
        if let s = p.services?.first(where: { $0.uuid == LoRaGatt.service }) {
            p.discoverCharacteristics([LoRaGatt.rx, LoRaGatt.tx], for: s)
        }
    }

    func peripheral(_ p: CBPeripheral, didDiscoverCharacteristicsFor s: CBService, error: Error?) {
        guard let r = s.characteristics?.first(where: { $0.uuid == LoRaGatt.rx }),
              let t = s.characteristics?.first(where: { $0.uuid == LoRaGatt.tx }) else { return }
        rx = r
        p.setNotifyValue(true, for: t)
    }

    func peripheral(_ p: CBPeripheral, didUpdateNotificationStateFor ch: CBCharacteristic, error: Error?) {
        guard error == nil else { return }
        lock.lock()
        connected = true
        lock.broadcast()
        lock.unlock()
        onState("Connected")
    }

    func peripheral(_ p: CBPeripheral, didUpdateValueFor ch: CBCharacteristic, error: Error?) {
        guard let v = ch.value else { return }
        lock.lock()
        inbox.append(v)
        lock.broadcast()
        lock.unlock()
    }
}
