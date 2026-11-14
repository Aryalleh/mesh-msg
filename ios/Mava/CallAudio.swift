import AVFoundation
import Foundation

/// The audio of one call (docs/calls.md): 16 kHz mono, 20 ms Opus frames
/// (codec 1), the same as Android. Capture: microphone with the system's
/// echo canceller → Opus → core. Playback: the core's jitter buffer every
/// 20 ms → Opus → speaker. A lost frame plays as silence.
final class CallAudio {
    private static let rate = 16_000.0
    private static let frame: AVAudioFrameCount = 320 // 20 ms

    private let core: Core
    private let id: String
    private let engine = AVAudioEngine()
    private let player = AVAudioPlayerNode()
    private let pcm = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: CallAudio.rate, channels: 1, interleaved: false)!
    private let opus: AVAudioFormat
    private var toPCM: AVAudioConverter?   // microphone format → 16 kHz mono
    private var encoder: AVAudioConverter?
    private var decoder: AVAudioConverter?
    private var pending: [Float] = []
    private var playTimer: DispatchSourceTimer?
    private let queue = DispatchQueue(label: "call-audio")
    var muted = false

    init(core: Core, id: String) {
        self.core = core
        self.id = id
        var d = AudioStreamBasicDescription(
            mSampleRate: CallAudio.rate, mFormatID: kAudioFormatOpus, mFormatFlags: 0, mBytesPerPacket: 0,
            mFramesPerPacket: CallAudio.frame, mBytesPerFrame: 0, mChannelsPerFrame: 1, mBitsPerChannel: 0, mReserved: 0)
        opus = AVAudioFormat(streamDescription: &d)!
    }

    static func askMicrophone(_ done: @escaping (Bool) -> Void) {
        AVAudioSession.sharedInstance().requestRecordPermission { ok in
            DispatchQueue.main.async { done(ok) }
        }
    }

    func start() {
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playAndRecord, mode: .voiceChat, options: [.allowBluetooth])
        try? session.setPreferredSampleRate(48_000)
        try? session.setPreferredIOBufferDuration(0.02)
        try? session.setActive(true)

        let input = engine.inputNode
        try? input.setVoiceProcessingEnabled(true) // echo cancellation, noise suppression
        let inFormat = input.outputFormat(forBus: 0)
        toPCM = AVAudioConverter(from: inFormat, to: pcm)
        encoder = AVAudioConverter(from: pcm, to: opus)
        encoder?.bitRate = 20_000
        decoder = AVAudioConverter(from: opus, to: pcm)

        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: pcm)
        input.installTap(onBus: 0, bufferSize: 1024, format: inFormat) { [weak self] buf, _ in
            self?.queue.async { self?.captured(buf) }
        }
        do {
            try engine.start()
            player.play()
        } catch {
            NSLog("CallAudio: engine: \(error)")
        }

        let t = DispatchSource.makeTimerSource(queue: queue)
        t.schedule(deadline: .now() + .milliseconds(20), repeating: .milliseconds(20), leeway: .milliseconds(2))
        t.setEventHandler { [weak self] in self?.playNext() }
        t.resume()
        playTimer = t
    }

    func stop() {
        playTimer?.cancel()
        playTimer = nil
        engine.inputNode.removeTap(onBus: 0)
        player.stop()
        engine.stop()
        try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
    }

    func setSpeaker(_ on: Bool) {
        try? AVAudioSession.sharedInstance().overrideOutputAudioPort(on ? .speaker : .none)
    }

    // ---- capture ----

    private func captured(_ buf: AVAudioPCMBuffer) {
        guard let toPCM else { return }
        let ratio = CallAudio.rate / buf.format.sampleRate
        let cap = AVAudioFrameCount(Double(buf.frameLength) * ratio) + 32
        guard let out = AVAudioPCMBuffer(pcmFormat: pcm, frameCapacity: cap) else { return }
        var fed = false
        var err: NSError?
        toPCM.convert(to: out, error: &err) { _, status in
            if fed { status.pointee = .noDataNow; return nil }
            fed = true
            status.pointee = .haveData
            return buf
        }
        guard err == nil, let ch = out.floatChannelData else { return }
        pending.append(contentsOf: UnsafeBufferPointer(start: ch[0], count: Int(out.frameLength)))
        let n = Int(CallAudio.frame)
        while pending.count >= n {
            let frame = Array(pending[0..<n])
            pending.removeFirst(n)
            encode(muted ? [Float](repeating: 0, count: n) : frame)
        }
    }

    private func encode(_ samples: [Float]) {
        guard let encoder, let inBuf = AVAudioPCMBuffer(pcmFormat: pcm, frameCapacity: CallAudio.frame) else { return }
        inBuf.frameLength = CallAudio.frame
        samples.withUnsafeBufferPointer { src in
            inBuf.floatChannelData![0].update(from: src.baseAddress!, count: samples.count)
        }
        let out = AVAudioCompressedBuffer(format: opus, packetCapacity: 1, maximumPacketSize: 1500)
        var fed = false
        var err: NSError?
        encoder.convert(to: out, error: &err) { _, status in
            if fed { status.pointee = .noDataNow; return nil }
            fed = true
            status.pointee = .haveData
            return inBuf
        }
        guard err == nil, out.packetCount > 0, out.byteLength > 0, !muted else { return }
        core.callAudio(id, Data(bytes: out.data, count: Int(out.byteLength)))
    }

    // ---- playback ----

    private func playNext() {
        let (result, frame) = core.pullCallAudio(id)
        guard let buf = AVAudioPCMBuffer(pcmFormat: pcm, frameCapacity: CallAudio.frame * 2) else { return }
        var decoded = false
        if result == 0, !frame.isEmpty, let decoder {
            let inBuf = AVAudioCompressedBuffer(format: opus, packetCapacity: 1, maximumPacketSize: frame.count)
            frame.withUnsafeBytes { src in
                inBuf.data.copyMemory(from: src.baseAddress!, byteCount: frame.count)
            }
            inBuf.byteLength = UInt32(frame.count)
            inBuf.packetCount = 1
            inBuf.packetDescriptions?.pointee = AudioStreamPacketDescription(mStartOffset: 0, mVariableFramesInPacket: 0, mDataByteSize: UInt32(frame.count))
            var fed = false
            var err: NSError?
            decoder.convert(to: buf, error: &err) { _, status in
                if fed { status.pointee = .noDataNow; return nil }
                fed = true
                status.pointee = .haveData
                return inBuf
            }
            decoded = err == nil && buf.frameLength > 0
        }
        if !decoded {
            buf.frameLength = CallAudio.frame
            memset(buf.floatChannelData![0], 0, Int(CallAudio.frame) * MemoryLayout<Float>.size)
        }
        player.scheduleBuffer(buf, completionHandler: nil)
    }
}
