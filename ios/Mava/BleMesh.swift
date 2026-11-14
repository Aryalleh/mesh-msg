import CoreBluetooth
import Foundation
import Meshcore

/// The Bluetooth mesh with nearby phones: the same GATT protocol as Android
/// (android/.../ble/BleProto.kt, docs/android.md §4).
///
/// Android phones advertise the service plus a 4-byte session ID as
/// manufacturer data. iOS can't advertise manufacturer data, so an iPhone
/// advertises its session ID as the local name (8 hex digits) instead:
///   - an Android phone is seen with its session: the iPhone always connects
///     to it as GATT client (Android doesn't see the iPhone's session);
///   - another iPhone is seen by its name: the larger session ID connects.
/// client → server: writes with response to RX; server → client:
/// notifications on TX; the first write is the hello with the session ID.
final class BleMesh: NSObject, MeshcoreTransportProtocol, CBCentralManagerDelegate, CBPeripheralDelegate, CBPeripheralManagerDelegate {
    static let service = CBUUID(string: "6d657368-6d73-6700-8000-00805f9b34fb")
    static let rxUUID = CBUUID(string: "6d657368-6d73-6701-8000-00805f9b34fb")
    static let txUUID = CBUUID(string: "6d657368-6d73-6702-8000-00805f9b34fb")
    static let link = "ble"
    static let coreChunk = 180
    static let maxPeers = 4
    static let helloMagic: UInt8 = 0x4D

    private let core: Core
    private let onCount: (Int) -> Void
    private let session: [UInt8] = (0..<4).map { _ in UInt8.random(in: 0...255) }
    private var central: CBCentralManager?
    private var peripheral: CBPeripheralManager?
    private var txChar: CBMutableCharacteristic?
    private var running = false

    /// A connection we made (we are the GATT client).
    private final class Out {
        let id: String
        let peripheral: CBPeripheral
        var rx: CBCharacteristic?
        var queue: [Data] = []
        var busy = false
        var up = false
        init(id: String, peripheral: CBPeripheral) { self.id = id; self.peripheral = peripheral }
    }

    /// A phone that connected to us (we are the GATT server).
    private final class In {
        let central: CBCentral
        var id: String?
        var queue: [Data] = []
        init(central: CBCentral) { self.central = central }
    }

    private var outs: [UUID: Out] = [:]
    private var ins: [UUID: In] = [:]

    init(core: Core, onCount: @escaping (Int) -> Void) {
        self.core = core
        self.onCount = onCount
    }

    private var sessionHex: String { session.map { String(format: "%02x", $0) }.joined() }

    func start() {
        running = true
        try? core.raw.addTransport(BleMesh.link, chunk: BleMesh.coreChunk, t: self)
        central = CBCentralManager(delegate: self, queue: .main)
        peripheral = CBPeripheralManager(delegate: self, queue: .main)
    }

    func stop() {
        running = false
        central?.stopScan()
        outs.values.forEach { central?.cancelPeripheralConnection($0.peripheral) }
        peripheral?.stopAdvertising()
        peripheral?.removeAllServices()
        for id in peerIDs() { try? core.raw.peerDown(BleMesh.link, peer: id) }
        outs.removeAll()
        ins.removeAll()
        core.raw.removeTransport(BleMesh.link)
        onCount(0)
    }

    private func peerIDs() -> [String] {
        outs.values.filter { $0.up }.map { $0.id } + ins.values.compactMap { $0.id }
    }

    private func count() { onCount(peerIDs().count) }

    // ---- the core sends (any thread) ----

    func send(_ peer: String?, chunk: Data?) {
        guard let peer, let chunk else { return }
        let copy = Data(chunk)
        DispatchQueue.main.async {
            if let o = self.outs.values.first(where: { $0.id == peer && $0.up }) {
                let size = max(20, o.peripheral.maximumWriteValueLength(for: .withResponse))
                o.queue.append(contentsOf: BleMesh.split(copy, size))
                self.pump(o)
            } else if let i = self.ins.values.first(where: { $0.id == peer }) {
                i.queue.append(contentsOf: BleMesh.split(copy, max(20, i.central.maximumUpdateValueLength)))
                self.pump(i)
            }
        }
    }

    private static func split(_ d: Data, _ size: Int) -> [Data] {
        if d.count <= size { return [d] }
        return stride(from: 0, to: d.count, by: size).map { d.subdata(in: $0..<min($0 + size, d.count)) }
    }

    // ---- central: we find and connect ----

    func centralManagerDidUpdateState(_ c: CBCentralManager) {
        if c.state == .poweredOn, running {
            c.scanForPeripherals(withServices: [BleMesh.service], options: [CBCentralManagerScanOptionAllowDuplicatesKey: false])
        }
    }

    func centralManager(_ c: CBCentralManager, didDiscover p: CBPeripheral, advertisementData ad: [String: Any], rssi: NSNumber) {
        guard running, outs[p.identifier] == nil, outs.count + ins.count < BleMesh.maxPeers else { return }
        var theirs: [UInt8]?
        var android = false
        if let m = ad[CBAdvertisementDataManufacturerDataKey] as? Data, m.count == 6, m[m.startIndex] == 0xFF, m[m.startIndex + 1] == 0xFF {
            theirs = Array(m.dropFirst(2))
            android = true
        } else if let name = ad[CBAdvertisementDataLocalNameKey] as? String, name.count == 8 {
            theirs = stride(from: 0, to: 8, by: 2).compactMap { i -> UInt8? in
                let a = name.index(name.startIndex, offsetBy: i)
                return UInt8(name[a..<name.index(a, offsetBy: 2)], radix: 16)
            }
        }
        guard let theirs, theirs.count == 4 else { return }
        let id = theirs.map { String(format: "%02x", $0) }.joined()
        if peerIDs().contains(id) || outs.values.contains(where: { $0.id == id }) { return }
        // Between iPhones the larger session connects; an Android never connects to us.
        if !android, !theirs.lexicographicallyPrecedes(session) { return }
        let o = Out(id: id, peripheral: p)
        outs[p.identifier] = o
        p.delegate = self
        c.connect(p)
        DispatchQueue.main.asyncAfter(deadline: .now() + 20) { [weak self] in
            if let o = self?.outs[p.identifier], !o.up { self?.drop(o) }
        }
    }

    func centralManager(_ c: CBCentralManager, didConnect p: CBPeripheral) {
        p.discoverServices([BleMesh.service])
    }

    func centralManager(_ c: CBCentralManager, didFailToConnect p: CBPeripheral, error: Error?) {
        if let o = outs[p.identifier] { drop(o) }
    }

    func centralManager(_ c: CBCentralManager, didDisconnectPeripheral p: CBPeripheral, error: Error?) {
        if let o = outs[p.identifier] { drop(o) }
    }

    private func drop(_ o: Out) {
        outs[o.peripheral.identifier] = nil
        if o.up { try? core.raw.peerDown(BleMesh.link, peer: o.id) }
        o.up = false
        central?.cancelPeripheralConnection(o.peripheral)
        count()
    }

    func peripheral(_ p: CBPeripheral, didDiscoverServices error: Error?) {
        guard let s = p.services?.first(where: { $0.uuid == BleMesh.service }) else {
            if let o = outs[p.identifier] { drop(o) }
            return
        }
        p.discoverCharacteristics([BleMesh.rxUUID, BleMesh.txUUID], for: s)
    }

    func peripheral(_ p: CBPeripheral, didDiscoverCharacteristicsFor s: CBService, error: Error?) {
        guard let o = outs[p.identifier] else { return }
        guard let rx = s.characteristics?.first(where: { $0.uuid == BleMesh.rxUUID }),
              let tx = s.characteristics?.first(where: { $0.uuid == BleMesh.txUUID }) else { return drop(o) }
        o.rx = rx
        p.setNotifyValue(true, for: tx)
    }

    func peripheral(_ p: CBPeripheral, didUpdateNotificationStateFor ch: CBCharacteristic, error: Error?) {
        guard let o = outs[p.identifier], let rx = o.rx else { return }
        if error != nil { return drop(o) }
        // Hello first; the peer counts as up once it is acknowledged.
        o.busy = true
        p.writeValue(Data([BleMesh.helloMagic] + session), for: rx, type: .withResponse)
    }

    func peripheral(_ p: CBPeripheral, didWriteValueFor ch: CBCharacteristic, error: Error?) {
        guard let o = outs[p.identifier] else { return }
        if error != nil { return drop(o) }
        o.busy = false
        if !o.up {
            o.up = true
            try? core.raw.peerUp(BleMesh.link, peer: o.id)
            count()
        }
        pump(o)
    }

    private func pump(_ o: Out) {
        guard o.up, !o.busy, !o.queue.isEmpty, let rx = o.rx else { return }
        o.busy = true
        o.peripheral.writeValue(o.queue.removeFirst(), for: rx, type: .withResponse)
    }

    func peripheral(_ p: CBPeripheral, didUpdateValueFor ch: CBCharacteristic, error: Error?) {
        guard let o = outs[p.identifier], o.up, let v = ch.value else { return }
        do { try core.raw.receive(BleMesh.link, peer: o.id, chunk: v) } catch { drop(o) }
    }

    // ---- peripheral: other iPhones connect to us ----

    func peripheralManagerDidUpdateState(_ pm: CBPeripheralManager) {
        guard pm.state == .poweredOn, running else { return }
        let tx = CBMutableCharacteristic(type: BleMesh.txUUID, properties: [.notify], value: nil, permissions: [])
        let rx = CBMutableCharacteristic(type: BleMesh.rxUUID, properties: [.write], value: nil, permissions: [.writeable])
        let s = CBMutableService(type: BleMesh.service, primary: true)
        s.characteristics = [rx, tx]
        txChar = tx
        pm.removeAllServices()
        pm.add(s)
        pm.startAdvertising([CBAdvertisementDataServiceUUIDsKey: [BleMesh.service], CBAdvertisementDataLocalNameKey: sessionHex])
    }

    func peripheralManager(_ pm: CBPeripheralManager, didReceiveWrite requests: [CBATTRequest]) {
        for r in requests {
            guard r.characteristic.uuid == BleMesh.rxUUID, let v = r.value else { continue }
            let i = ins[r.central.identifier] ?? In(central: r.central)
            ins[r.central.identifier] = i
            if i.id == nil {
                guard v.count == 5, v[v.startIndex] == BleMesh.helloMagic else { continue }
                let id = v.dropFirst().map { String(format: "%02x", $0) }.joined()
                i.id = id
                try? core.raw.peerUp(BleMesh.link, peer: id)
                count()
            } else if let id = i.id {
                do { try core.raw.receive(BleMesh.link, peer: id, chunk: v) } catch { drop(i) }
            }
        }
        if let first = requests.first { pm.respond(to: first, withResult: .success) }
    }

    func peripheralManager(_ pm: CBPeripheralManager, central: CBCentral, didUnsubscribeFrom ch: CBCharacteristic) {
        if let i = ins[central.identifier] { drop(i) }
    }

    func peripheralManagerIsReady(toUpdateSubscribers pm: CBPeripheralManager) {
        ins.values.forEach { pump($0) }
    }

    private func pump(_ i: In) {
        guard let tx = txChar, let pm = peripheral else { return }
        while let next = i.queue.first {
            if !pm.updateValue(next, for: tx, onSubscribedCentrals: [i.central]) { return } // full: wait for isReady
            i.queue.removeFirst()
        }
    }

    private func drop(_ i: In) {
        ins[i.central.identifier] = nil
        if let id = i.id { try? core.raw.peerDown(BleMesh.link, peer: id) }
        count()
    }
}
