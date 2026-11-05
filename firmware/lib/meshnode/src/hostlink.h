// Node ↔ host (phone over BLE, PC over USB) byte stream, mirroring
// core/hostlink: COBS( kind | len(2) | payload | crc16(2) ) 0x00.
// Also the config TLVs, the status block and the region / preset tables.
// Must reproduce the "hostlink" section of spec/test-vectors/vectors.json.
#pragma once

#include <cstddef>
#include <cstdint>

namespace mesh {
namespace host {

constexpr uint8_t kKindPacket = 0;
constexpr uint8_t kKindCommand = 1;
constexpr uint8_t kKindEvent = 2;
constexpr uint8_t kKindTunnel = 3;

constexpr size_t kMaxPayload = 512;
constexpr size_t kMaxRaw = 5 + kMaxPayload;
// Longest encoded frame including the delimiter.
constexpr size_t kMaxEncoded = kMaxRaw + kMaxRaw / 254 + 2;

uint16_t crc16(const uint8_t* b, size_t n);

// encode writes one delimited frame to out. Returns its length, or 0 if the
// payload is too large or out too small.
size_t encode(uint8_t kind, const uint8_t* payload, size_t n, uint8_t* out, size_t cap);

// Decoder splits a byte stream into frames. feed() consumes bytes up to and
// including the first completed frame and returns how many it consumed; if
// a frame completed, frame() returns true until the next feed().
class Decoder {
 public:
  size_t feed(const uint8_t* b, size_t n);
  bool frame() const { return ready_; }
  uint8_t kind() const { return kind_; }
  const uint8_t* payload() const { return raw_ + 3; }
  size_t payload_len() const { return len_; }
  uint32_t errors() const { return errors_; }

 private:
  bool finish();
  uint8_t enc_[kMaxEncoded];
  uint8_t raw_[kMaxRaw];
  size_t n_ = 0;
  bool overflow_ = false, ready_ = false;
  uint8_t kind_ = 0;
  size_t len_ = 0;
  uint32_t errors_ = 0;
};

// Commands / events (payload: op | req | data).
constexpr uint8_t kOpHello = 0x01;
constexpr uint8_t kOpGetStatus = 0x02;
constexpr uint8_t kOpGetConfig = 0x03;
constexpr uint8_t kOpSetConfig = 0x04;
constexpr uint8_t kOpGetIdentity = 0x05;
constexpr uint8_t kOpResync = 0x06;
constexpr uint8_t kOpReboot = 0x07;
constexpr uint8_t kOpWipe = 0x08;

constexpr uint8_t kEvStatus = 0x81;
constexpr uint8_t kEvConfig = 0x82;
constexpr uint8_t kEvIdentity = 0x83;
constexpr uint8_t kEvAck = 0x84;
constexpr uint8_t kEvLog = 0x85;

constexpr uint8_t kAckOk = 0;
constexpr uint8_t kAckBadArgs = 1;
constexpr uint8_t kAckUnknownOp = 2;
constexpr uint8_t kAckNotAllowed = 3;

constexpr uint8_t kRoleClient = 0;
constexpr uint8_t kRoleRepeater = 1;
constexpr uint8_t kRoleGateway = 2;

struct PresetInfo {
  uint8_t id;
  const char* name;
  uint8_t sf;
  uint32_t bw_hz;
  uint8_t cr;
};

// Frequency plans: defaults to start from, not legal advice — the operator
// must check local rules (see core/hostlink.Regions for the notes).
struct RegionInfo {
  uint8_t id;
  const char* name;
  uint32_t freq_hz, min_hz, max_hz;
  int8_t max_dbm;
  uint32_t duty_ppm;
};

extern const PresetInfo kPresets[];
extern const size_t kPresetCount;
extern const RegionInfo kRegions[];
extern const size_t kRegionCount;

constexpr uint8_t kTagRole = 1;
constexpr uint8_t kTagRegion = 2;
constexpr uint8_t kTagPreset = 3;
constexpr uint8_t kTagFreqHz = 4;
constexpr uint8_t kTagTxDBm = 5;
constexpr uint8_t kTagName = 6;
constexpr uint8_t kTagAnnounceMin = 7;
constexpr size_t kMaxNameLen = 32;
constexpr size_t kMaxConfigTLV = 3 * 3 + 6 + 3 + 2 + kMaxNameLen + 4;

struct NodeConfig {
  uint8_t role = kRoleClient;
  uint8_t region = 0;
  uint8_t preset = 1;  // BALANCED
  uint32_t freq_hz = 0;  // 0 = region default
  int8_t tx_dbm = 14;
  char name[kMaxNameLen + 1] = {};
  uint16_t announce_min = 30;

  bool valid() const;
  uint32_t channel_hz() const;  // freq_hz or the region default
  size_t encode_tlv(uint8_t* out, size_t cap) const;
  // apply_tlv updates from (possibly partial) TLVs; unchanged on error.
  bool apply_tlv(const uint8_t* b, size_t n);
  bool operator==(const NodeConfig& o) const;
};

constexpr size_t kStatusSize = 36;

struct Status {
  uint8_t firmware_version = 0, role = 0, region = 0, preset = 0;
  uint32_t freq_hz = 0;
  int8_t tx_dbm = 0;
  uint8_t short_id = 0;
  uint32_t uptime_s = 0;
  uint16_t stored = 0;
  uint8_t queued = 0, neighbours = 0;
  uint32_t air_used_ms = 0, air_budget_ms = 0, rx_frames = 0, tx_frames = 0;
  uint16_t battery_mv = 0;

  void encode(uint8_t out[kStatusSize]) const;
};

}  // namespace host
}  // namespace mesh
