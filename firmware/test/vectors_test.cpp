// Conformance against spec/test-vectors/vectors.json: the C++ codec must
// reproduce every vector the Go core generated, byte for byte.
#include <algorithm>
#include <fstream>
#include <memory>
#include <sstream>

#include "announce.h"
#include "check.h"
#include "identity.h"
#include "hostlink.h"
#include "json.h"
#include "packet.h"

using check::Bytes;
using check::hex;
using check::unhex;

namespace {

std::string encode_hex(const mesh::Packet& p) {
  Bytes out(p.size() + 8);
  size_t n = 0;
  const mesh::Err e = mesh::encode(p, out.data(), out.size(), &n);
  if (e != mesh::Err::Ok) return std::string("<encode error: ") + mesh::err_name(e) + ">";
  return hex(out.data(), n);
}

void constants(const json::Value& c) {
  CHECK(c["version"].i() == mesh::kVersion);
  CHECK(c["header_size"].i() == long(mesh::kHeaderSize));
  CHECK(c["lora_radio_max"].i() == long(mesh::kLoRaRadioMax));
  CHECK(c["lora_link_header"].i() == long(mesh::kLoRaLinkHeader));
  CHECK(c["lora_mtu"].i() == long(mesh::kLoRaMTU));
  CHECK(c["max_fragments"].i() == long(mesh::kMaxFragments));
  CHECK(c["frag_body_size"].i() == long(mesh::kFragBodySize));
  CHECK(c["default_max_hops"].i() == mesh::kDefaultMaxHops);
}

void headers(const json::Value& vs) {
  for (const auto& v : vs.arr) {
    const std::string name = v["name"].s();
    const Bytes body = unhex(v["body"].s()), id = unhex(v["id"].s()), dest = unhex(v["dest"].s());

    mesh::Packet p;
    p.type = static_cast<mesh::Type>(v["type"].i());
    p.ctx = uint8_t(v["ctx"].i());
    p.hop_count = uint8_t(v["hop_count"].i());
    p.max_hops = uint8_t(v["max_hops"].i());
    std::memcpy(p.id, id.data(), 8);
    std::memcpy(p.dest, dest.data(), 16);
    p.frag_index = uint8_t(v["frag_index"].i());
    p.frag_count = uint8_t(v["frag_count"].i());
    p.body = body.data();
    p.body_len = body.size();
    CHECK_HEX(encode_hex(p), v["encoded"].s(), "header encode " + name);

    const Bytes raw = unhex(v["encoded"].s());
    mesh::Packet d;
    CHECKC(mesh::decode(raw.data(), raw.size(), d) == mesh::Err::Ok, "header decode " + name);
    CHECKC(d.type == p.type && d.ctx == p.ctx && d.hop_count == p.hop_count && d.max_hops == p.max_hops,
           "header fields " + name);
    CHECKC(std::memcmp(d.id, p.id, 8) == 0 && std::memcmp(d.dest, p.dest, 16) == 0, "header ids " + name);
    if (p.fragmented())
      CHECKC(d.frag_index == p.frag_index && d.frag_count == p.frag_count, "header frag " + name);
    CHECK_HEX(hex(d.body, d.body_len), v["body"].s(), "header body " + name);
  }
}

void inc_hop(const json::Value& vs) {
  for (const auto& v : vs.arr) {
    Bytes raw = unhex(v["in"].s());
    const bool ok = mesh::inc_hop(raw.data(), raw.size());
    const std::string want = v["out"].s();
    if (want.empty()) {
      CHECKC(!ok, "inc_hop must drop " + v["in"].s());
      CHECK_HEX(hex(raw), v["in"].s(), "inc_hop drop leaves input untouched");
    } else {
      CHECKC(ok, "inc_hop must forward " + v["in"].s());
      CHECK_HEX(hex(raw), want, "inc_hop");
    }
  }
}

void fragments(const json::Value& vs, const json::Value& hdr0) {
  const Bytes id = unhex(hdr0["id"].s()), dest = unhex(hdr0["dest"].s());
  for (const auto& v : vs.arr) {
    const size_t n = size_t(v["body_len"].i()), max = size_t(v["max_size"].i());
    const std::string ctx = "fragments body_len=" + std::to_string(n);
    Bytes body(n);
    for (size_t i = 0; i < n; i++) body[i] = uint8_t(i);
    mesh::Packet p;
    p.type = mesh::Type::Data;
    p.max_hops = 8;
    std::memcpy(p.id, id.data(), 8);
    std::memcpy(p.dest, dest.data(), 16);
    p.body = body.data();
    p.body_len = n;

    const auto& want = v["fragments"].arr;
    const size_t count = mesh::fragment_count(p, max);
    CHECKC(count == want.size(), ctx + " count=" + std::to_string(count));
    std::vector<Bytes> raws;
    for (size_t i = 0; i < count && i < want.size(); i++) {
      mesh::Packet f;
      CHECKC(mesh::fragment(p, max, i, f) == mesh::Err::Ok, ctx);
      const std::string got = encode_hex(f);
      CHECK_HEX(got, want[i].s(), ctx + " fragment " + std::to_string(i));
      CHECKC(got.size() / 2 <= max, ctx + " fragment exceeds max size");
      raws.push_back(unhex(want[i].s()));
    }

    // Decode the vector fragments in reverse order and reassemble.
    if (count > 1) {
      std::vector<mesh::Packet> frags;
      for (size_t i = raws.size(); i-- > 0;) {
        mesh::Packet f;
        CHECKC(mesh::decode(raws[i].data(), raws[i].size(), f) == mesh::Err::Ok, ctx + " decode");
        frags.push_back(f);
      }
      Bytes joined(mesh::kMaxFragments * mesh::kFragBodySize);
      mesh::Packet whole;
      CHECKC(mesh::reassemble(frags.data(), frags.size(), joined.data(), joined.size(), whole) == mesh::Err::Ok,
             ctx + " reassemble");
      CHECK_HEX(encode_hex(whole), encode_hex(p), ctx + " reassembled");
    }
  }
}

void frames(const json::Value& vs) {
  for (const auto& v : vs.arr) {
    const Bytes pkt = unhex(v["packet"].s());
    uint8_t out[mesh::kLoRaRadioMax];
    size_t n = 0;
    CHECK(mesh::encode_link_frame(uint8_t(v["sender_id"].i()), pkt.data(), pkt.size(), out, sizeof out, &n) ==
          mesh::Err::Ok);
    CHECK_HEX(hex(out, n), v["frame"].s(), "link frame");

    const Bytes frame = unhex(v["frame"].s());
    uint8_t sender = 0;
    const uint8_t* raw = nullptr;
    size_t raw_len = 0;
    CHECK(mesh::decode_link_frame(frame.data(), frame.size(), &sender, &raw, &raw_len) == mesh::Err::Ok);
    CHECK(sender == v["sender_id"].i());
    CHECK_HEX(hex(raw, raw_len), v["packet"].s(), "link frame decode");
  }
}

void identities(const json::Value& vs) {
  for (const auto& v : vs.arr) {
    const Bytes seed = unhex(v["seed"].s());
    const mesh::Identity id = mesh::Identity::from_seed(seed.data());
    CHECK_HEX(hex(id.pub().x, 32), v["x25519_pub"].s(), "x25519");
    CHECK_HEX(hex(id.pub().ed, 32), v["ed25519_pub"].s(), "ed25519");
    uint8_t h[16];
    id.pub().hash(h);
    CHECK_HEX(hex(h, 16), v["dest_hash"].s(), "dest hash");
  }
}

void announces(const json::Value& vs) {
  for (const auto& v : vs.arr) {
    const std::string ctx = "announce: " + v["comment"].s();
    const Bytes raw = unhex(v["packet"].s());
    mesh::Packet p;
    CHECKC(mesh::decode(raw.data(), raw.size(), p) == mesh::Err::Ok, ctx);
    mesh::Announce a;
    const mesh::AnnounceErr e = mesh::parse_announce(p, a);
    CHECKC((e == mesh::AnnounceErr::Ok) == v["valid"].b, ctx);
    if (e != mesh::AnnounceErr::Ok) continue;

    const Bytes seed = unhex(v["seed"].s());
    const mesh::Identity id = mesh::Identity::from_seed(seed.data());
    CHECKC(std::memcmp(a.pub.ed, id.pub().ed, 32) == 0, ctx + " signer");
    // Ed25519 is deterministic: rebuilding from the seed must give identical bytes.
    uint8_t body[mesh::kMaxAnnounceBody];
    mesh::Packet b;
    CHECKC(mesh::build_announce(id, a.time, a.app, p.max_hops, p.id, body, sizeof body, b) ==
               mesh::AnnounceErr::Ok,
           ctx);
    CHECK_HEX(encode_hex(b), v["packet"].s(), ctx + " rebuild");
  }
}

void receipts(const json::Value& vs) {
  for (const auto& v : vs.arr) {
    const std::string ctx = "receipt: " + v["comment"].s();
    const Bytes raw = unhex(v["packet"].s());
    mesh::Packet p;
    CHECKC(mesh::decode(raw.data(), raw.size(), p) == mesh::Err::Ok, ctx);
    const Bytes seed = unhex(v["seed"].s());
    const mesh::Identity id = mesh::Identity::from_seed(seed.data());

    uint8_t acked[8];
    CHECKC(mesh::receipt_acked(p, acked) == mesh::AnnounceErr::Ok, ctx + " acked");
    CHECK_HEX(hex(acked, 8), v["acked"].s(), ctx);
    const mesh::AnnounceErr e = mesh::verify_receipt(p, id.pub(), acked);
    CHECKC((e == mesh::AnnounceErr::Ok) == v["valid"].b, ctx);
    if (e != mesh::AnnounceErr::Ok) continue;

    uint8_t body[mesh::kReceiptBody];
    mesh::Packet b;
    CHECKC(mesh::build_receipt(id, acked, p.max_hops, body, sizeof body, b) == mesh::AnnounceErr::Ok, ctx);
    CHECK_HEX(encode_hex(b), v["packet"].s(), ctx + " rebuild");
  }
}

void hostlink(const json::Value& hv) {
  using namespace mesh::host;
  // Frames: byte-identical encoding, and the decoder recovers them from one
  // concatenated stream fed in odd-sized chunks.
  Bytes stream;
  for (const auto& v : hv["frames"].arr) {
    const Bytes payload = unhex(v["payload"].s());
    uint8_t out[kMaxEncoded];
    size_t n = encode(uint8_t(v["kind"].i()), payload.data(), payload.size(), out, sizeof out);
    CHECK_HEX(hex(out, n), v["encoded"].s(), "hostlink frame");
    const Bytes e = unhex(v["encoded"].s());
    stream.insert(stream.end(), e.begin(), e.end());
  }
  auto dec = std::make_unique<Decoder>();
  size_t got = 0, off = 0, chunk = 1;
  while (off < stream.size()) {
    size_t n = std::min(chunk, stream.size() - off);
    size_t used = dec->feed(stream.data() + off, n);
    off += used;
    chunk = chunk % 37 + 3;
    if (dec->frame()) {
      const auto& v = hv["frames"].arr.at(got++);
      CHECK(dec->kind() == v["kind"].i());
      CHECK_HEX(hex(dec->payload(), dec->payload_len()), v["payload"].s(), "hostlink decode");
    }
  }
  CHECK(got == hv["frames"].arr.size() && dec->errors() == 0);

  for (const auto& v : hv["configs"].arr) {
    NodeConfig c;
    c.role = uint8_t(v["role"].i());
    c.region = uint8_t(v["region"].i());
    c.preset = uint8_t(v["preset"].i());
    c.freq_hz = uint32_t(v["freq_hz"].i());
    c.tx_dbm = int8_t(v["tx_dbm"].i());
    std::snprintf(c.name, sizeof c.name, "%s", v["name"].s().c_str());
    c.announce_min = uint16_t(v["announce_min"].i());
    CHECK(c.valid());
    uint8_t t[kMaxConfigTLV];
    size_t n = c.encode_tlv(t, sizeof t);
    CHECK_HEX(hex(t, n), v["tlv"].s(), "config tlv");
    NodeConfig d;
    const Bytes tlv = unhex(v["tlv"].s());
    CHECK(d.apply_tlv(tlv.data(), tlv.size()) && d == c);
  }

  const auto& sv = hv["status"]["status"];
  Status s;
  s.firmware_version = uint8_t(sv["firmware_version"].i());
  s.role = uint8_t(sv["role"].i());
  s.region = uint8_t(sv["region"].i());
  s.preset = uint8_t(sv["preset"].i());
  s.freq_hz = uint32_t(sv["freq_hz"].i());
  s.tx_dbm = int8_t(sv["tx_dbm"].i());
  s.short_id = uint8_t(sv["short_id"].i());
  s.uptime_s = uint32_t(sv["uptime_s"].i());
  s.stored = uint16_t(sv["stored"].i());
  s.queued = uint8_t(sv["queued"].i());
  s.neighbours = uint8_t(sv["neighbours"].i());
  s.air_used_ms = uint32_t(sv["air_used_ms"].i());
  s.air_budget_ms = uint32_t(sv["air_budget_ms"].i());
  s.rx_frames = uint32_t(sv["rx_frames"].i());
  s.tx_frames = uint32_t(sv["tx_frames"].i());
  s.battery_mv = uint16_t(sv["battery_mv"].i());
  uint8_t st[kStatusSize];
  s.encode(st);
  CHECK_HEX(hex(st, sizeof st), hv["status"]["encoded"].s(), "status");

  // The firmware's region and preset tables are the Go ones.
  CHECK(hv["regions"].arr.size() == kRegionCount && hv["presets"].arr.size() == kPresetCount);
  for (size_t i = 0; i < kRegionCount && i < hv["regions"].arr.size(); i++) {
    const auto& r = hv["regions"].arr[i];
    const RegionInfo& m = kRegions[i];
    CHECKC(r["id"].i() == m.id && r["name"].s() == m.name && r["freq_hz"].i() == m.freq_hz &&
               r["min_hz"].i() == m.min_hz && r["max_hz"].i() == m.max_hz && r["max_dbm"].i() == m.max_dbm &&
               r["duty_ppm"].i() == m.duty_ppm,
           r["name"].s());
  }
  for (size_t i = 0; i < kPresetCount && i < hv["presets"].arr.size(); i++) {
    const auto& p = hv["presets"].arr[i];
    const PresetInfo& m = kPresets[i];
    CHECKC(p["id"].i() == m.id && p["name"].s() == m.name && p["sf"].i() == m.sf && p["bw_hz"].i() == m.bw_hz &&
               p["cr"].i() == m.cr,
           p["name"].s());
  }
}

}  // namespace

int run_vectors(const char* path) {
  std::ifstream f(path, std::ios::binary);
  if (!f) {
    std::fprintf(stderr, "cannot open %s\n", path);
    return 1;
  }
  std::stringstream ss;
  ss << f.rdbuf();
  const json::Value v = json::parse(ss.str());

  CHECK(v["version"].i() == 1);
  constants(v["constants"]);
  headers(v["headers"]);
  inc_hop(v["inc_hop"]);
  fragments(v["fragments"], v["headers"].arr.at(0));
  frames(v["frames"]);
  identities(v["identities"]);
  announces(v["announces"]);
  receipts(v["receipts"]);
  hostlink(v["hostlink"]);
  // "text" (compact Persian encoding) is end-to-end payload: nodes forward
  // ciphertext and never see it, so the firmware does not implement it.
  std::printf("vectors: %zu headers, %zu inc_hop, %zu fragment sets, %zu frames, %zu identities, %zu announces, "
              "%zu receipts (%zu text vectors not applicable to firmware)\n",
              v["headers"].arr.size(), v["inc_hop"].arr.size(), v["fragments"].arr.size(), v["frames"].arr.size(),
              v["identities"].arr.size(), v["announces"].arr.size(), v["receipts"].arr.size(),
              v["text"].arr.size());
  return 0;
}
