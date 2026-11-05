#include "hostlink.h"

#include <cstring>

namespace mesh {
namespace host {

const PresetInfo kPresets[] = {
    {0, "FAST", 7, 250000, 1},
    {1, "BALANCED", 9, 125000, 1},
    {2, "LONG", 11, 125000, 4},
};
const size_t kPresetCount = sizeof kPresets / sizeof kPresets[0];

const RegionInfo kRegions[] = {
    {0, "EU_868", 869525000, 869462500, 869587500, 27, 100000},
    {1, "EU_868_1PCT", 868100000, 868062500, 868537500, 14, 10000},
    {2, "EU_433", 433875000, 433112500, 434727500, 10, 100000},
    {3, "US_915", 906875000, 902062500, 927937500, 30, 1000000},
    {4, "AU_915", 916000000, 915062500, 927937500, 30, 1000000},
    {5, "IN_865", 866000000, 865062500, 866937500, 30, 1000000},
    {6, "AS_923", 923200000, 920062500, 924937500, 16, 100000},
};
const size_t kRegionCount = sizeof kRegions / sizeof kRegions[0];

uint16_t crc16(const uint8_t* b, size_t n) {
  uint16_t crc = 0xFFFF;
  for (size_t i = 0; i < n; i++) {
    crc ^= uint16_t(b[i]) << 8;
    for (int k = 0; k < 8; k++) crc = (crc & 0x8000) ? uint16_t((crc << 1) ^ 0x1021) : uint16_t(crc << 1);
  }
  return crc;
}

namespace {

// COBS-encodes in[0..n) into out; returns the length or 0 if cap is too small.
size_t cobs_encode(const uint8_t* in, size_t n, uint8_t* out, size_t cap) {
  if (cap < 1) return 0;
  size_t o = 1, code_at = 0;
  uint8_t code = 1;
  for (size_t i = 0; i < n; i++) {
    if (in[i] == 0) {
      out[code_at] = code;
      code_at = o;
      if (o >= cap) return 0;
      out[o++] = 0;
      code = 1;
      continue;
    }
    if (o >= cap) return 0;
    out[o++] = in[i];
    if (++code == 0xFF) {
      out[code_at] = code;
      code_at = o;
      if (o >= cap) return 0;
      out[o++] = 0;
      code = 1;
    }
  }
  out[code_at] = code;
  return o;
}

bool cobs_decode(const uint8_t* in, size_t n, uint8_t* out, size_t cap, size_t* out_len) {
  size_t o = 0;
  for (size_t i = 0; i < n;) {
    size_t code = in[i];
    if (code == 0 || i + code > n) return false;
    if (o + code - 1 > cap) return false;
    std::memcpy(out + o, in + i + 1, code - 1);
    o += code - 1;
    i += code;
    if (code < 0xFF && i < n) {
      if (o >= cap) return false;
      out[o++] = 0;
    }
  }
  *out_len = o;
  return true;
}

void put32(uint8_t* p, uint32_t v) {
  p[0] = uint8_t(v >> 24);
  p[1] = uint8_t(v >> 16);
  p[2] = uint8_t(v >> 8);
  p[3] = uint8_t(v);
}
void put16(uint8_t* p, uint16_t v) {
  p[0] = uint8_t(v >> 8);
  p[1] = uint8_t(v);
}
uint32_t get32(const uint8_t* p) { return uint32_t(p[0]) << 24 | uint32_t(p[1]) << 16 | uint32_t(p[2]) << 8 | p[3]; }

}  // namespace

size_t encode(uint8_t kind, const uint8_t* payload, size_t n, uint8_t* out, size_t cap) {
  if (n > kMaxPayload) return 0;
  uint8_t raw[kMaxRaw];
  raw[0] = kind;
  put16(raw + 1, uint16_t(n));
  if (n) std::memcpy(raw + 3, payload, n);
  put16(raw + 3 + n, crc16(raw, 3 + n));
  size_t e = cobs_encode(raw, 5 + n, out, cap);
  if (e == 0 || e >= cap) return 0;
  out[e] = 0;
  return e + 1;
}

size_t Decoder::feed(const uint8_t* b, size_t n) {
  ready_ = false;
  for (size_t i = 0; i < n; i++) {
    if (b[i] != 0) {
      if (n_ >= sizeof enc_)
        overflow_ = true;
      else
        enc_[n_++] = b[i];
      continue;
    }
    bool ok = false;
    if (overflow_)
      errors_++;
    else if (n_ > 0) {
      ok = finish();
      if (!ok) errors_++;
    }
    n_ = 0;
    overflow_ = false;
    if (ok) {
      ready_ = true;
      return i + 1;
    }
  }
  return n;
}

bool Decoder::finish() {
  size_t m = 0;
  if (!cobs_decode(enc_, n_, raw_, sizeof raw_, &m) || m < 5) return false;
  size_t len = size_t(raw_[1]) << 8 | raw_[2];
  if (len > kMaxPayload || m != 5 + len) return false;
  uint16_t want = uint16_t(raw_[3 + len] << 8 | raw_[4 + len]);
  if (crc16(raw_, 3 + len) != want) return false;
  kind_ = raw_[0];
  len_ = len;
  return true;
}

// ---- NodeConfig ----

bool NodeConfig::valid() const {
  if (role > kRoleGateway || region >= kRegionCount || preset >= kPresetCount) return false;
  const RegionInfo& r = kRegions[region];
  if (freq_hz != 0 && (freq_hz < r.min_hz || freq_hz > r.max_hz)) return false;
  if (tx_dbm > r.max_dbm || tx_dbm < -9) return false;
  return std::strlen(name) <= kMaxNameLen;
}

uint32_t NodeConfig::channel_hz() const { return freq_hz ? freq_hz : kRegions[region < kRegionCount ? region : 0].freq_hz; }

size_t NodeConfig::encode_tlv(uint8_t* out, size_t cap) const {
  size_t nl = std::strlen(name);
  size_t need = 3 * 3 + 6 + 3 + 2 + nl + 4;
  if (cap < need) return 0;
  uint8_t* p = out;
  *p++ = kTagRole, *p++ = 1, *p++ = role;
  *p++ = kTagRegion, *p++ = 1, *p++ = region;
  *p++ = kTagPreset, *p++ = 1, *p++ = preset;
  *p++ = kTagFreqHz, *p++ = 4;
  put32(p, freq_hz);
  p += 4;
  *p++ = kTagTxDBm, *p++ = 1, *p++ = uint8_t(tx_dbm);
  *p++ = kTagName, *p++ = uint8_t(nl);
  std::memcpy(p, name, nl);
  p += nl;
  *p++ = kTagAnnounceMin, *p++ = 2;
  put16(p, announce_min);
  p += 2;
  return size_t(p - out);
}

bool NodeConfig::apply_tlv(const uint8_t* b, size_t n) {
  NodeConfig c = *this;
  while (n > 0) {
    if (n < 2 || n < 2 + size_t(b[1])) return false;
    uint8_t tag = b[0], len = b[1];
    const uint8_t* v = b + 2;
    b += 2 + len;
    n -= 2 + len;
    auto need = [&](uint8_t w) { return len == w; };
    switch (tag) {
      case kTagRole:
        if (!need(1)) return false;
        c.role = v[0];
        break;
      case kTagRegion:
        if (!need(1)) return false;
        c.region = v[0];
        break;
      case kTagPreset:
        if (!need(1)) return false;
        c.preset = v[0];
        break;
      case kTagFreqHz:
        if (!need(4)) return false;
        c.freq_hz = get32(v);
        break;
      case kTagTxDBm:
        if (!need(1)) return false;
        c.tx_dbm = int8_t(v[0]);
        break;
      case kTagName:
        if (len > kMaxNameLen) return false;
        std::memcpy(c.name, v, len);
        c.name[len] = 0;
        if (std::strlen(c.name) != len) return false;  // embedded NUL
        break;
      case kTagAnnounceMin:
        if (!need(2)) return false;
        c.announce_min = uint16_t(v[0] << 8 | v[1]);
        break;
      default:
        break;  // unknown tags are skipped
    }
  }
  if (!c.valid()) return false;
  *this = c;
  return true;
}

bool NodeConfig::operator==(const NodeConfig& o) const {
  return role == o.role && region == o.region && preset == o.preset && freq_hz == o.freq_hz && tx_dbm == o.tx_dbm &&
         announce_min == o.announce_min && std::strcmp(name, o.name) == 0;
}

void Status::encode(uint8_t out[kStatusSize]) const {
  out[0] = firmware_version, out[1] = role, out[2] = region, out[3] = preset;
  put32(out + 4, freq_hz);
  out[8] = uint8_t(tx_dbm), out[9] = short_id;
  put32(out + 10, uptime_s);
  put16(out + 14, stored);
  out[16] = queued, out[17] = neighbours;
  put32(out + 18, air_used_ms);
  put32(out + 22, air_budget_ms);
  put32(out + 26, rx_frames);
  put32(out + 30, tx_frames);
  put16(out + 34, battery_mv);
}

}  // namespace host
}  // namespace mesh
