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

mac::Config Node::mac_config(const host::NodeConfig& c, uint32_t random, const mac::Preset* modulation) {
  mac::Config mc;
  const host::PresetInfo& p = host::kPresets[c.preset];
  mc.preset = modulation ? *modulation : mac::Preset{p.sf, p.bw_hz, p.cr};
  mc.duty_ppm = host::kRegions[c.region].duty_ppm;
  mc.seed = random ^ 0x85ebca6bu;
  return mc;
}

Node::Node(const host::NodeConfig& cfg, const uint8_t seed[kSeedSize], Platform& platform, store::Persist* persist,
           uint32_t random, const mac::Preset* modulation)
    : cfg_(sanitized(cfg)),
      id_(Identity::from_seed(seed)),
      platform_(platform),
      modulation_(modulation),
      short_id_(short_id_of(id_.pub())),
      rng_(random ? random : 1),
      router_(router_config(cfg_, short_id_, random)),
      mac_(mac_config(cfg_, random, modulation)),
      store_(persist) {
  ports_[kWebPort].trusted = false;
}

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

void Node::ingest(const Packet& p, const uint8_t* raw, size_t n, store::Origin origin, uint8_t have, bool purge,
                  Millis now) {
  if (p.type == Type::Receipt && purge) {
    uint8_t acked[8];
    if (receipt_acked(p, acked) == AnnounceErr::Ok) store_.delete_data(acked);
  }
  if (p.type == Type::Announce) {
    Announce a;
    if (parse_announce(p, a) == AnnounceErr::Ok) serve(p.dest);
  }
  if (p.type == Type::Data || p.type == Type::Receipt || p.type == Type::Announce) {
    store_.put(raw, n, origin, have, uint32_t(now / 1000), kStoreTTL);
    pump();
  }
}

void Node::serve(const uint8_t dest[16]) {
  for (size_t i = 0; i < served_n_; i++)
    if (std::memcmp(served_[i], dest, 16) == 0) return;
  std::memcpy(served_[served_next_], dest, 16);
  served_next_ = (served_next_ + 1) % kServed;
  if (served_n_ < kServed) served_n_++;
}

size_t Node::served(uint8_t (*out)[16], size_t max) const {
  size_t n = served_n_ < max ? served_n_ : max;
  for (size_t i = 0; i < n; i++) std::memcpy(out[i], served_[i], 16);
  return n;
}

void Node::radio_rx(const uint8_t* frame, size_t n, int snr_db, Millis now) {
  route::RxResult r = router_.on_receive(frame, n, snr_db, now);
  if (r.verdict == route::Verdict::Invalid) return;
  heard(r.sender, now);
  if (r.verdict != route::Verdict::New) return;
  // The packet bytes follow the one-byte link header. Receipts were already
  // verified by the router (r.purged); unverifiable ones are still stored.
  ingest(r.packet, frame + kLoRaLinkHeader, n - kLoRaLinkHeader, store::Origin::Air, 0, r.purged, now);
}

void Node::air_rx(const uint8_t* raw, size_t n, uint8_t sender, int snr_db, Millis now) {
  if (!raw || n == 0 || n > kLoRaMTU) return;
  uint8_t frame[kLoRaLinkHeader + kLoRaMTU];
  frame[0] = sender;
  std::memcpy(frame + kLoRaLinkHeader, raw, n);
  radio_rx(frame, n + kLoRaLinkHeader, snr_db, now);
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

uint8_t Node::sink_of(size_t port) {
  static constexpr uint8_t kSinks[kHostPorts] = {store::kSinkPhone, store::kSinkHost1, store::kSinkHost2};
  return kSinks[port];
}

bool Node::any_host_attached() const {
  for (const Port& p : ports_)
    if (p.attached) return true;
  return false;
}

size_t Node::hosts_pending() const {
  size_t n = 0;
  for (const Port& p : ports_) n += p.out_len;
  return n;
}

bool Node::emit(size_t port, uint8_t kind, const uint8_t* payload, size_t n) {
  Port& h = ports_[port];
  uint8_t buf[host::kMaxEncoded];
  size_t e = host::encode(kind, payload, n, buf, sizeof buf);
  if (e == 0 || h.out_len + e > sizeof h.out) return false;
  std::memcpy(h.out + h.out_len, buf, e);
  h.out_len += e;
  return true;
}

void Node::event(size_t port, uint8_t ev, uint8_t req, const uint8_t* data, size_t n) {
  if (!ports_[port].attached) return;
  uint8_t p[2 + host::kMaxPayload];
  p[0] = ev;
  p[1] = req;
  if (n > sizeof p - 2) n = sizeof p - 2;
  if (n) std::memcpy(p + 2, data, n);
  if (!emit(port, host::kKindEvent, p, 2 + n)) stats_.host_events_dropped++;
}

void Node::ack(size_t port, uint8_t op, uint8_t req, uint8_t status) {
  uint8_t d[2] = {op, status};
  event(port, host::kEvAck, req, d, 2);
}

void Node::pump() {
  for (size_t i = 0; i < kHostPorts; i++) pump(i);
}

// pump moves stored packets the host does not have into its output buffer
// while there is room for a whole frame.
void Node::pump(size_t port) {
  Port& h = ports_[port];
  if (!h.attached) return;
  uint8_t sink = sink_of(port);
  store::Item it;
  while (sizeof h.out - h.out_len >= host::kMaxEncoded && h.inflight_n < kInflight && store_.next_for(sink, it)) {
    if (!emit(port, host::kKindPacket, it.raw, it.len)) break;
    // Marked now so it is not emitted twice; handed back on detach unless
    // host_tx drained it (inflight end offset).
    store_.mark(it.seq, sink);
    h.inflight[h.inflight_n++] = Inflight{it.seq, h.out_len};
    stats_.host_packets_out++;
  }
}

void Node::host_attach(size_t port, Millis now) {
  if (port >= kHostPorts) return;
  ports_[port].attached = true;
  (void)now;
}

void Node::host_detach(size_t port) {
  if (port >= kHostPorts) return;
  Port& h = ports_[port];
  h.attached = false;
  for (size_t i = 0; i < h.inflight_n; i++) store_.unmark(h.inflight[i].seq, sink_of(port));
  h.inflight_n = 0;
  h.out_len = 0;
  h.dec = host::Decoder();
}

size_t Node::host_tx(size_t port, uint8_t* out, size_t cap) {
  if (port >= kHostPorts) return 0;
  Port& h = ports_[port];
  size_t n = h.out_len < cap ? h.out_len : cap;
  std::memcpy(out, h.out, n);
  std::memmove(h.out, h.out + n, h.out_len - n);
  h.out_len -= n;
  size_t keep = 0;
  for (size_t i = 0; i < h.inflight_n; i++) {
    if (h.inflight[i].end <= n) continue;  // fully handed to the transport
    h.inflight[keep] = Inflight{h.inflight[i].seq, h.inflight[i].end - n};
    keep++;
  }
  h.inflight_n = keep;
  pump(port);
  return n;
}

void Node::host_rx(size_t port, const uint8_t* b, size_t n, Millis now) {
  if (port >= kHostPorts) return;
  Port& h = ports_[port];
  if (!h.attached) host_attach(port, now);
  while (n > 0) {
    uint32_t errs = h.dec.errors();
    size_t used = h.dec.feed(b, n);
    b += used;
    n -= used;
    stats_.host_bad_frames += h.dec.errors() - errs;
    if (!h.dec.frame()) continue;
    switch (h.dec.kind()) {
      case host::kKindPacket: {
        stats_.host_packets_in++;
        const uint8_t* raw = h.dec.payload();
        size_t rn = h.dec.payload_len();
        Packet p;
        if (rn > kLoRaMTU || decode(raw, rn, p) != Err::Ok) {
          stats_.host_packets_rejected++;
          break;
        }
        // The host already handled hops (meshd's router increments them when
        // forwarding to a broadcast link); send as is with implicit-ACK retries.
        if (!router_.send(raw, rn, true, now)) stats_.host_packets_rejected++;
        // The other host and the relay get it too.
        ingest(p, raw, rn, store::Origin::Phone, sink_of(port), h.trusted, now);  // a trusted host's receipts purge
        break;
      }
      case host::kKindCommand:
        command(port, h.dec.payload(), h.dec.payload_len(), now);
        break;
      default:
        break;  // events are node → host; the tunnel is not used (the board has its own uplink)
    }
  }
}

// ---- uplink ----

size_t Node::uplink_take(uint8_t* out, size_t cap, uint32_t* seqs, size_t max, size_t* count) {
  size_t used = 0, k = 0;
  store::Item it;
  while (k < max && store_.next_for(store::kSinkUplink, it)) {
    if (used + 4 + it.len > cap) break;
    out[used] = uint8_t(it.len >> 24);
    out[used + 1] = uint8_t(it.len >> 16);
    out[used + 2] = uint8_t(it.len >> 8);
    out[used + 3] = uint8_t(it.len);
    std::memcpy(out + used + 4, it.raw, it.len);
    used += 4 + it.len;
    store_.mark(it.seq, store::kSinkUplink);
    seqs[k++] = it.seq;
  }
  stats_.uplink_out += uint32_t(k);
  *count = k;
  return used;
}

void Node::uplink_return(const uint32_t* seqs, size_t count) {
  for (size_t i = 0; i < count; i++) store_.unmark(seqs[i], store::kSinkUplink);
}

bool Node::uplink_rx(const uint8_t* raw, size_t n, Millis now) {
  Packet p;
  if (!raw || n == 0 || n > kLoRaMTU || decode(raw, n, p) != Err::Ok) return false;
  if (p.type != Type::Data && p.type != Type::Receipt && p.type != Type::Announce) return false;
  route::Key k = route::Key::of(p);
  if (store_.has(k)) return false;
  stats_.uplink_in++;
  // Broadcast it (another tier: the router bumps the hop count), then hand
  // it to the hosts. NO_LORA packets only reach the hosts.
  if (!p.no_lora()) router_.send(raw, n, false, now);
  // Receipts from the relay are not verified here, so they purge nothing.
  ingest(p, raw, n, store::Origin::Uplink, store::kSinkUplink, false, now);
  return true;
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

void Node::command(size_t port, const uint8_t* p, size_t n, Millis now) {
  if (n < 2) {
    stats_.host_bad_frames++;
    return;
  }
  uint8_t op = p[0], req = p[1];
  const uint8_t* a = p + 2;
  size_t an = n - 2;
  if (!ports_[port].trusted && op != host::kOpHello && op != host::kOpGetStatus && op != host::kOpGetConfig &&
      op != host::kOpGetIdentity && op != host::kOpResync) {
    ack(port, op, req, host::kAckNotAllowed);
    return;
  }
  switch (op) {
    case host::kOpHello: {
      if (an == 4) {
        uint32_t t = uint32_t(a[0]) << 24 | uint32_t(a[1]) << 16 | uint32_t(a[2]) << 8 | a[3];
        set_time(t, now);
      } else if (an != 0) {
        ack(port, op, req, host::kAckBadArgs);
        return;
      }
      uint8_t st[host::kStatusSize];
      status(now).encode(st);
      event(port, host::kEvStatus, req, st, sizeof st);
      pump(port);
      return;
    }
    case host::kOpGetStatus: {
      uint8_t st[host::kStatusSize];
      status(now).encode(st);
      event(port, host::kEvStatus, req, st, sizeof st);
      return;
    }
    case host::kOpGetConfig: {
      uint8_t t[host::kMaxConfigTLV];
      size_t tn = cfg_.encode_tlv(t, sizeof t);
      event(port, host::kEvConfig, req, t, tn);
      return;
    }
    case host::kOpSetConfig: {
      host::NodeConfig c = cfg_;
      if (!c.apply_tlv(a, an)) {
        ack(port, op, req, host::kAckBadArgs);
        return;
      }
      apply_config(c, now);
      ack(port, op, req, host::kAckOk);
      return;
    }
    case host::kOpGetIdentity: {
      uint8_t pub[kPublicSize];
      std::memcpy(pub, id_.pub().x, 32);
      std::memcpy(pub + 32, id_.pub().ed, 32);
      event(port, host::kEvIdentity, req, pub, sizeof pub);
      return;
    }
    case host::kOpResync:
      store_.reset_sink(sink_of(port));
      ack(port, op, req, host::kAckOk);
      pump();
      return;
    case host::kOpReboot:
      ack(port, op, req, host::kAckOk);
      reboot_ = true;
      return;
    case host::kOpWipe:
      if (an != 4 || std::memcmp(a, "WIPE", 4) != 0) {
        ack(port, op, req, host::kAckBadArgs);
        return;
      }
      ack(port, op, req, host::kAckOk);
      platform_.wipe();
      reboot_ = true;
      return;
    default:
      ack(port, op, req, host::kAckUnknownOp);
  }
}

bool Node::apply_config(const host::NodeConfig& c, Millis now) {
  if (!c.valid()) return false;
  bool radio = c.region != cfg_.region || c.preset != cfg_.preset || c.channel_hz() != cfg_.channel_hz() ||
               c.tx_dbm != cfg_.tx_dbm;
  bool modulation = c.region != cfg_.region || c.preset != cfg_.preset;
  cfg_ = c;
  platform_.save_config(cfg_);
  router_.set_role(role_of(cfg_.role));
  if (modulation) mac_ = mac::Mac(mac_config(cfg_, rand32(), modulation_));  // queued frames are dropped
  if (radio) platform_.apply_radio(cfg_);
  if (cfg_.announce_min == 0)
    next_announce_ = 0;
  else if (unix_at_ && next_announce_ == 0)
    next_announce_ = first_announce(now);
  return true;
}

void Node::set_time(uint32_t unix_now, Millis now) {
  if (unix_now == 0) return;
  bool first = unix_at_ == 0;
  unix_at_ = unix_now;
  unix_given_ = now;
  if (first && cfg_.announce_min) next_announce_ = first_announce(now);
}

Millis Node::first_announce(Millis now) { return now + 5000 + rand32() % 25000; }

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
    // ±10 % so nodes that booted together drift apart.
    Millis period = Millis(cfg_.announce_min) * 60000;
    next_announce_ = now + period - period / 10 + rand32() % (period / 5 + 1);
  }
  pump();
}

bool Node::restore(size_t slot, const store::Record& r, Millis now) {
  return store_.restore(slot, r, uint32_t(now / 1000), kStoreTTL);
}

}  // namespace node
}  // namespace mesh
