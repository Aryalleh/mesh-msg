#include "node.h"

#include <cstring>

#include "announce.h"

namespace mesh {
namespace node {

namespace {

route::Role role_of(uint8_t r) {
  switch (r) {
    case host::kRoleRepeater:
      return route::Role::Repeater;
    case host::kRoleGateway:
      return route::Role::Gateway;
    default:
      return route::Role::Client;
  }
}

host::NodeConfig sanitized(const host::NodeConfig& c) { return c.valid() ? c : host::NodeConfig{}; }

uint8_t short_id_of(const PublicKey& pub) {
  uint8_t h[16];
  pub.hash(h);
  return h[0];
}

}  // namespace

route::Config Node::router_config(const host::NodeConfig& c, uint8_t short_id, uint32_t random) {
  route::Config rc;
  rc.short_id = short_id;
  rc.role = role_of(c.role);
  rc.seed = random ^ 0x9e3779b9u;
  return rc;
}

mac::Config Node::mac_config(const host::NodeConfig& c, uint32_t random) {
  mac::Config mc;
  const host::PresetInfo& p = host::kPresets[c.preset];
  mc.preset = mac::Preset{p.sf, p.bw_hz, p.cr};
  mc.duty_ppm = host::kRegions[c.region].duty_ppm;
  mc.seed = random ^ 0x85ebca6bu;
  return mc;
}

Node::Node(const host::NodeConfig& cfg, const uint8_t seed[kSeedSize], Platform& platform, store::Persist* persist,
           uint32_t random)
    : cfg_(sanitized(cfg)),
      id_(Identity::from_seed(seed)),
      platform_(platform),
      short_id_(short_id_of(id_.pub())),
      rng_(random ? random : 1),
      router_(router_config(cfg_, short_id_, random)),
      mac_(mac_config(cfg_, random)),
      store_(persist) {}

uint32_t Node::rand32() {
  rng_ ^= rng_ << 13;
  rng_ ^= rng_ >> 17;
  rng_ ^= rng_ << 5;
  return rng_;
}

uint32_t Node::unix_time(Millis now) const {
  if (unix_at_ == 0) return 0;
  return unix_at_ + uint32_t((now - unix_given_) / 1000);
}

// ---- radio ----

void Node::heard(uint8_t sender, Millis now) {
  for (size_t i = 0; i < nb_count_; i++) {
    if (nb_[i].id == sender) {
      nb_[i].last = now;
      return;
    }
  }
  size_t slot = nb_count_ < kNeighbours ? nb_count_++ : 0;
  if (slot == 0 && nb_count_ == kNeighbours) {  // replace the stalest
    for (size_t i = 1; i < kNeighbours; i++)
      if (nb_[i].last < nb_[slot].last) slot = i;
  }
  nb_[slot] = Neighbour{sender, now};
}

void Node::radio_rx(const uint8_t* frame, size_t n, int snr_db, Millis now) {
  route::RxResult r = router_.on_receive(frame, n, snr_db, now);
  if (r.verdict == route::Verdict::Invalid) return;
  heard(r.sender, now);
  if (r.verdict != route::Verdict::New) return;
  const Packet& p = r.packet;
  if (p.type == Type::Receipt && r.purged) {
    uint8_t acked[8];
    if (receipt_acked(p, acked) == AnnounceErr::Ok) store_.delete_data(acked);
  }
  if (p.type == Type::Data || p.type == Type::Receipt || p.type == Type::Announce) {
    // The packet bytes follow the one-byte link header.
    store_.put(frame + kLoRaLinkHeader, n - kLoRaLinkHeader, store::Origin::Air, 0, uint32_t(now / 1000), kStoreTTL);
    pump();
  }
}

mac::Action Node::radio_next(Millis now) {
  route::Frame f;
  while (mac_.queued() < mac::kQueueCapacity && router_.next_due() <= now && router_.poll(now, f)) mac_.enqueue(f, now);
  mac::Action a = mac_.next(now);
  if (a.kind == mac::Kind::Idle && !mac_.busy()) {
    // Wake for the router's next frame if that is sooner.
    Millis due = router_.next_due();
    if (due < a.wake) a.wake = due < now ? now : due;
  }
  return a;
}

void Node::radio_cad(bool busy, Millis now) { mac_.cad_result(busy, now); }

void Node::radio_tx_done(Millis now) {
  route::Key k;
  if (mac_.tx_done(now, &k)) router_.on_transmitted(k, now);
}

// ---- host ----

bool Node::emit(uint8_t kind, const uint8_t* payload, size_t n) {
  uint8_t buf[host::kMaxEncoded];
  size_t e = host::encode(kind, payload, n, buf, sizeof buf);
  if (e == 0 || out_len_ + e > sizeof out_) return false;
  std::memcpy(out_ + out_len_, buf, e);
  out_len_ += e;
  return true;
}

void Node::event(uint8_t ev, uint8_t req, const uint8_t* data, size_t n) {
  if (!attached_) return;
  uint8_t p[2 + host::kMaxPayload];
  p[0] = ev;
  p[1] = req;
  if (n > sizeof p - 2) n = sizeof p - 2;
  if (n) std::memcpy(p + 2, data, n);
  if (!emit(host::kKindEvent, p, 2 + n)) stats_.host_events_dropped++;
}

void Node::ack(uint8_t op, uint8_t req, uint8_t status) {
  uint8_t d[2] = {op, status};
  event(host::kEvAck, req, d, 2);
}

// pump moves stored packets the host does not have into the output buffer
// while there is room for a whole frame.
void Node::pump() {
  if (!attached_) return;
  store::Item it;
  while (sizeof out_ - out_len_ >= host::kMaxEncoded && inflight_n_ < kInflight &&
         store_.next_for(store::kSinkPhone, it)) {
    if (!emit(host::kKindPacket, it.raw, it.len)) break;
    // Marked now so it is not emitted twice; handed back on detach unless
    // host_tx drained it (inflight end offset).
    store_.mark(it.seq, store::kSinkPhone);
    inflight_[inflight_n_++] = Inflight{it.seq, out_len_};
    stats_.host_packets_out++;
  }
}

void Node::host_attach(Millis now) {
  attached_ = true;
  (void)now;
}

void Node::host_detach() {
  attached_ = false;
  for (size_t i = 0; i < inflight_n_; i++) store_.unmark(inflight_[i].seq, store::kSinkPhone);
  inflight_n_ = 0;
  out_len_ = 0;
  dec_ = host::Decoder();
}

size_t Node::host_tx(uint8_t* out, size_t cap) {
  size_t n = out_len_ < cap ? out_len_ : cap;
  std::memcpy(out, out_, n);
  std::memmove(out_, out_ + n, out_len_ - n);
  out_len_ -= n;
  size_t keep = 0;
  for (size_t i = 0; i < inflight_n_; i++) {
    if (inflight_[i].end <= n) continue;  // fully handed to the transport
    inflight_[keep] = Inflight{inflight_[i].seq, inflight_[i].end - n};
    keep++;
  }
  inflight_n_ = keep;
  pump();
  return n;
}

void Node::host_rx(const uint8_t* b, size_t n, Millis now) {
  if (!attached_) host_attach(now);
  while (n > 0) {
    uint32_t errs = dec_.errors();
    size_t used = dec_.feed(b, n);
    b += used;
    n -= used;
    stats_.host_bad_frames += dec_.errors() - errs;
    if (!dec_.frame()) continue;
    switch (dec_.kind()) {
      case host::kKindPacket:
        stats_.host_packets_in++;
        // The host already handled hops (meshd's router increments them when
        // forwarding to a broadcast link); send as is with implicit-ACK retries.
        if (!router_.send(dec_.payload(), dec_.payload_len(), true, now)) stats_.host_packets_rejected++;
        break;
      case host::kKindCommand:
        command(dec_.payload(), dec_.payload_len(), now);
        break;
      default:
        break;  // events are node → host; tunnel not yet implemented
    }
  }
}

host::Status Node::status(Millis now) const {
  host::Status s;
  s.firmware_version = kFirmwareVersion;
  s.role = cfg_.role;
  s.region = cfg_.region;
  s.preset = cfg_.preset;
  s.freq_hz = cfg_.channel_hz();
  s.tx_dbm = cfg_.tx_dbm;
  s.short_id = short_id_;
  s.uptime_s = uint32_t(now / 1000);
  s.stored = uint16_t(store_.size());
  s.queued = uint8_t(mac_.queued());
  uint8_t nb = 0;
  for (size_t i = 0; i < nb_count_; i++)
    if (now - nb_[i].last < kNeighbourTTL) nb++;
  s.neighbours = nb;
  s.air_used_ms = mac_.duty().used_ms(now);
  s.air_budget_ms = mac_.duty().budget_ms();
  s.rx_frames = router_.stats().rx;
  s.tx_frames = mac_.stats().frames;
  s.battery_mv = platform_.battery_mv();
  return s;
}

void Node::command(const uint8_t* p, size_t n, Millis now) {
  if (n < 2) {
    stats_.host_bad_frames++;
    return;
  }
  uint8_t op = p[0], req = p[1];
  const uint8_t* a = p + 2;
  size_t an = n - 2;
  switch (op) {
    case host::kOpHello: {
      if (an == 4) {
        uint32_t t = uint32_t(a[0]) << 24 | uint32_t(a[1]) << 16 | uint32_t(a[2]) << 8 | a[3];
        if (t != 0) {
          bool first = unix_at_ == 0;
          unix_at_ = t;
          unix_given_ = now;
          if (first && cfg_.announce_min) next_announce_ = now + 5000;
        }
      } else if (an != 0) {
        ack(op, req, host::kAckBadArgs);
        return;
      }
      uint8_t st[host::kStatusSize];
      status(now).encode(st);
      event(host::kEvStatus, req, st, sizeof st);
      pump();
      return;
    }
    case host::kOpGetStatus: {
      uint8_t st[host::kStatusSize];
      status(now).encode(st);
      event(host::kEvStatus, req, st, sizeof st);
      return;
    }
    case host::kOpGetConfig: {
      uint8_t t[host::kMaxConfigTLV];
      size_t tn = cfg_.encode_tlv(t, sizeof t);
      event(host::kEvConfig, req, t, tn);
      return;
    }
    case host::kOpSetConfig: {
      host::NodeConfig c = cfg_;
      if (!c.apply_tlv(a, an)) {
        ack(op, req, host::kAckBadArgs);
        return;
      }
      bool radio = c.region != cfg_.region || c.preset != cfg_.preset || c.channel_hz() != cfg_.channel_hz() ||
                   c.tx_dbm != cfg_.tx_dbm;
      bool modulation = c.region != cfg_.region || c.preset != cfg_.preset;
      cfg_ = c;
      platform_.save_config(cfg_);
      router_.set_role(role_of(cfg_.role));
      if (modulation) mac_ = mac::Mac(mac_config(cfg_, rand32()));  // queued frames are dropped
      if (radio) platform_.apply_radio(cfg_);
      if (cfg_.announce_min == 0)
        next_announce_ = 0;
      else if (unix_at_ && next_announce_ == 0)
        next_announce_ = now + 5000;
      ack(op, req, host::kAckOk);
      return;
    }
    case host::kOpGetIdentity: {
      uint8_t pub[kPublicSize];
      std::memcpy(pub, id_.pub().x, 32);
      std::memcpy(pub + 32, id_.pub().ed, 32);
      event(host::kEvIdentity, req, pub, sizeof pub);
      return;
    }
    case host::kOpResync:
      store_.reset_sink(store::kSinkPhone);
      ack(op, req, host::kAckOk);
      pump();
      return;
    case host::kOpReboot:
      ack(op, req, host::kAckOk);
      reboot_ = true;
      return;
    case host::kOpWipe:
      if (an != 4 || std::memcmp(a, "WIPE", 4) != 0) {
        ack(op, req, host::kAckBadArgs);
        return;
      }
      ack(op, req, host::kAckOk);
      platform_.wipe();
      reboot_ = true;
      return;
    default:
      ack(op, req, host::kAckUnknownOp);
  }
}

void Node::announce(Millis now) {
  uint32_t t = unix_time(now);
  if (t == 0) return;
  AppData app;
  std::memcpy(app.name, cfg_.name, std::strlen(cfg_.name) + 1);
  app.flags = kFlagNode;
  uint8_t pid[8];
  for (int i = 0; i < 8; i += 4) {
    uint32_t r = rand32();
    std::memcpy(pid + i, &r, 4);
  }
  uint8_t body[kMaxAnnounceBody];
  Packet p;
  if (build_announce(id_, t, app, kLoRaMaxHops, pid, body, sizeof body, p) != AnnounceErr::Ok) return;
  uint8_t raw[kLoRaMTU];
  size_t n = 0;
  if (encode(p, raw, sizeof raw, &n) != Err::Ok) return;
  if (router_.send(raw, n, true, now)) stats_.announces++;
}

void Node::tick(Millis now) {
  router_.tick(now);
  if (now / 1000 != last_tick_ / 1000 || last_tick_ == 0) store_.expire(uint32_t(now / 1000));
  last_tick_ = now;
  if (next_announce_ != 0 && now >= next_announce_ && cfg_.announce_min) {
    announce(now);
    next_announce_ = now + Millis(cfg_.announce_min) * 60000;
  }
  pump();
}

bool Node::restore(size_t slot, const store::Record& r, Millis now) {
  return store_.restore(slot, r, uint32_t(now / 1000), kStoreTTL);
}

}  // namespace node
}  // namespace mesh
