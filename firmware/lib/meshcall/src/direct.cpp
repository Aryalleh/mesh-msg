#include "direct.h"

#include <cstring>

#include "callcrypto.h"
#include "monocypher.h"

namespace mesh {
namespace call {
namespace {

const uint8_t kProbeMagic[4] = {'M', 'D', 'P', 1};
const uint8_t kDataMagic[4] = {'M', 'D', 'D', 1};
constexpr uint8_t kProbeReq = 1, kProbeResp = 2;
constexpr size_t kMacLen = 16;
constexpr uint32_t kCookie = 0x2112A442;
constexpr uint32_t kKeepEvery = 15000, kIdleAfter = 45000;

bool after(uint32_t now, uint32_t t) { return int32_t(now - t) >= 0; }

void put16(uint8_t* p, uint16_t v) {
  p[0] = uint8_t(v >> 8);
  p[1] = uint8_t(v);
}

size_t put_addr(uint8_t* p, const Addr& a) {
  if (!a.valid()) {
    p[0] = 0;
    return 1;
  }
  p[0] = a.family;
  size_t k = a.is4() ? 4 : 16;
  std::memcpy(p + 1, a.ip, k);
  put16(p + 1 + k, a.port);
  return 1 + k + 2;
}

bool get_addr(const uint8_t* p, size_t n, Addr& a) {
  a = Addr();
  if (n < 1) return false;
  if (p[0] == 0) return true;
  size_t k = p[0] == 4 ? 4 : p[0] == 6 ? 16 : 0;
  if (!k || n < 1 + k + 2) return false;
  a.family = p[0];
  std::memcpy(a.ip, p + 1, k);
  a.port = uint16_t(p[1 + k] << 8 | p[2 + k]);
  return true;
}

}  // namespace

Agent::Agent(Net& net, const AgentConfig& cfg, const uint8_t sid[16], const uint8_t key[32], bool controlling,
             Random rnd, uint32_t now)
    : net_(net), cfg_(cfg), rnd_(rnd), controlling_(controlling), t0_(now) {
  std::memcpy(sid_, sid, 16);
  std::memcpy(key_, key, 32);
  hkdf_sha256(key_, 32, "mava direct probe", mac_key_);
  hkdf_sha256(key_, 32, controlling ? "mava direct data a" : "mava direct data b", seal_key_);
  hkdf_sha256(key_, 32, controlling ? "mava direct data b" : "mava direct data a", open_key_);
  sock_ = net_.open();
  if (sock_ < 0) {
    fail("no UDP socket");
    return;
  }
  n_hosts_ = net_.addrs(hosts_, sizeof hosts_ / sizeof hosts_[0]);
  for (size_t i = 0; i < cfg_.n_reflectors && n_refl_ < 4; i++) {
    Refl& r = refl_[n_refl_++];
    r.dst = cfg_.reflectors[i];
    rnd_(r.tx, sizeof r.tx);
    r.next = now + uint32_t(i) * 30;  // spread a little: a sequential NAT shows its order
  }
  if (!n_refl_) finish_gather(now);
}

Agent::~Agent() {
  close();
  crypto_wipe(key_, sizeof key_);
  crypto_wipe(mac_key_, sizeof mac_key_);
  crypto_wipe(seal_key_, sizeof seal_key_);
  crypto_wipe(open_key_, sizeof open_key_);
}

void Agent::close() {
  if (sock_ >= 0) net_.close(sock_);
  sock_ = -1;
  if (state_ != State::Failed) state_ = State::Closed;
}

void Agent::fail(const char* why) {
  reason_ = why;
  state_ = State::Failed;
}

// ---- gathering ----

void Agent::finish_gather(uint32_t now) {
  (void)now;
  uint16_t port = sock_ >= 0 ? net_.port(sock_) : 0;
  local_ = Offer();
  for (size_t i = 0; i < n_hosts_; i++) {
    Candidate c;
    c.kind = Kind::Host;
    c.addr = hosts_[i];
    c.addr.port = port;
    c.priority = priority(Kind::Host, c.addr);
    local_.add(c);
  }
  // Reflexive candidates in reflector order, and the NAT type (core/direct reflect).
  size_t got = 0;
  const Refl* first = nullptr;
  for (size_t i = 0; i < n_refl_; i++) {
    const Refl& r = refl_[i];
    if (!r.mapped.valid()) continue;
    if (!first) first = &r;
    got++;
    Candidate c;
    c.kind = Kind::Reflexive;
    c.addr = r.mapped;
    c.priority = priority(Kind::Reflexive, r.mapped);
    local_.add(c);
    local_.nat.last = r.mapped;
  }
  if (!got) {
    local_.nat.type = n_refl_ ? NatType::Blocked : NatType::Unknown;
  } else {
    bool open = false;
    for (size_t i = 0; i < n_hosts_; i++) open = open || (hosts_[i].same_ip(first->mapped) && first->mapped.port == port);
    if (open) {
      local_.nat.type = NatType::Open;
    } else {
      // The outer port seen by each distinct destination, in order.
      uint16_t order[4];
      size_t n = 0;
      for (size_t i = 0; i < n_refl_; i++) {
        const Refl& r = refl_[i];
        if (!r.mapped.valid()) continue;
        bool seen = false;
        for (size_t j = 0; j < i; j++) seen = seen || (refl_[j].mapped.valid() && refl_[j].dst.same(r.dst));
        if (!seen) order[n++] = r.mapped.port;
      }
      bool same = true;
      for (size_t i = 1; i < n; i++) same = same && order[i] == order[0];
      if (n < 2 || same) {
        local_.nat.type = NatType::Indep;
      } else {
        int step = int(order[1]) - int(order[0]);
        bool seq = step != 0 && step >= -64 && step <= 64;
        for (size_t i = 2; i < n && seq; i++) seq = int(order[i]) - int(order[i - 1]) == step;
        local_.nat.type = seq ? NatType::SymSeq : NatType::SymRandom;
        if (seq) local_.nat.step = int16_t(step);
      }
    }
  }
  // Highest priority first (the order core/direct sends).
  for (size_t i = 1; i < local_.n; i++)
    for (size_t j = i; j > 0 && local_.cand[j].priority > local_.cand[j - 1].priority; j--) {
      Candidate t = local_.cand[j];
      local_.cand[j] = local_.cand[j - 1];
      local_.cand[j - 1] = t;
    }
  if (state_ == State::Gathering) state_ = State::Gathered;
}

void Agent::on_stun(const uint8_t* d, size_t n) {
  // Binding success: 0x0101 | len | cookie | tx(12) | attributes.
  if (n < 20 || d[0] != 0x01 || d[1] != 0x01) return;
  Refl* r = nullptr;
  for (size_t i = 0; i < n_refl_; i++)
    if (std::memcmp(refl_[i].tx, d + 8, 12) == 0) r = &refl_[i];
  if (!r || r->done) return;
  size_t len = size_t(d[2] << 8 | d[3]);
  if (20 + len > n) return;
  Addr found;
  for (size_t off = 20; off + 4 <= 20 + len;) {
    uint16_t type = uint16_t(d[off] << 8 | d[off + 1]);
    size_t al = size_t(d[off + 2] << 8 | d[off + 3]);
    const uint8_t* v = d + off + 4;
    if (off + 4 + al > 20 + len) break;
    bool xored = type == 0x0020, plain = type == 0x0001;
    if ((xored || (plain && !found.valid())) && al >= 8 && v[1] == 0x01) {  // IPv4
      Addr a;
      a.family = 4;
      uint16_t port = uint16_t(v[2] << 8 | v[3]);
      std::memcpy(a.ip, v + 4, 4);
      if (xored) {
        port ^= uint16_t(kCookie >> 16);
        for (int i = 0; i < 4; i++) a.ip[i] ^= uint8_t(kCookie >> (24 - 8 * i));
      }
      a.port = port;
      found = a;
      if (xored) break;
    }
    off += 4 + ((al + 3) & ~size_t(3));
  }
  if (!found.valid()) return;
  r->mapped = found;
  r->done = true;
}

// ---- probes (core/direct: "MDP" | 1 | sid(16) | type(1) | tx(8) | [seen] | mac(16)) ----

size_t Agent::probe(uint8_t typ, const uint8_t tx[8], const Addr& seen, uint8_t* out, size_t cap) const {
  if (cap < 4 + 16 + 1 + 8 + 19 + kMacLen) return 0;
  size_t n = 0;
  std::memcpy(out, kProbeMagic, 4), n += 4;
  std::memcpy(out + n, sid_, 16), n += 16;
  out[n++] = uint8_t(typ + (controlling_ ? 0 : 2));
  std::memcpy(out + n, tx, 8), n += 8;
  if (typ == kProbeResp) n += put_addr(out + n, seen);
  uint8_t mac[32];
  hmac_sha256(mac_key_, 32, out, n, nullptr, 0, mac);
  std::memcpy(out + n, mac, kMacLen);
  return n + kMacLen;
}

void Agent::on_probe(const Addr& from, const uint8_t* d, size_t n, uint32_t now) {
  if (n < 4 + 16 + 1 + 8 + kMacLen) return;
  size_t body = n - kMacLen;
  uint8_t mac[32];
  hmac_sha256(mac_key_, 32, d, body, nullptr, 0, mac);
  if (crypto_verify16(mac, d + body) != 0 || std::memcmp(d + 4, sid_, 16) != 0) return;
  uint8_t typ = d[20];
  bool theirs = typ > 2;
  if (theirs) typ = uint8_t(typ - 2);
  // A probe with our own role is one of ours that came back (two networks
  // with the same private addresses): never a path.
  if (theirs != controlling_ || typ < kProbeReq || typ > kProbeResp) return;
  const uint8_t* tx = d + 21;
  if (typ == kProbeReq) {
    uint8_t out[80];
    size_t k = probe(kProbeResp, tx, from, out, sizeof out);
    if (k && sock_ >= 0) net_.send(sock_, from, out, k);
    // Triggered check: an address we did not know is peer-reflexive.
    if (state_ == State::Checking) {
      bool known = false;
      for (size_t i = 0; i < n_pairs_; i++) known = known || pairs_[i].remote.addr.same(from);
      if (!known) {
        Candidate c;
        c.kind = Kind::PeerRefl;
        c.addr = from;
        c.priority = priority(Kind::PeerRefl, from);
        add_pair(c);
      }
    }
    return;
  }
  Addr seen;
  if (!get_addr(d + 29, body - 29, seen)) return;
  for (size_t i = 0; i < n_pairs_; i++) {
    Pair& p = pairs_[i];
    if (p.ok || std::memcmp(p.tx, tx, 8) != 0) continue;
    p.ok = true;
    p.rtt = now - p.sent;
    p.seen = seen;
    if (!from.same(p.remote.addr)) {  // answered from elsewhere: that is the path
      p.remote.kind = Kind::PeerRefl;
      p.remote.addr = from;
      p.remote.priority = priority(Kind::PeerRefl, from);
    }
    if (state_ == State::Checking && !settling_) {
      settling_ = true;
      settle_ = now + 400;  // a better pair may still answer
    }
    return;
  }
}

Agent::Pair* Agent::add_pair(const Candidate& c) {
  for (size_t i = 0; i < n_pairs_; i++)
    if (pairs_[i].remote.addr.same(c.addr)) return &pairs_[i];
  if (!c.addr.is4()) return nullptr;  // one IPv4 socket
  if (n_pairs_ >= kMaxPairs) return nullptr;
  Pair& p = pairs_[n_pairs_++];
  p = Pair();
  p.remote = c;
  rnd_(p.tx, sizeof p.tx);
  return &p;
}

void Agent::connect(const Offer& remote, uint32_t now) {
  if (state_ != State::Gathered) return;
  remote_ = remote;
  for (size_t i = 0; i < remote.n; i++) add_pair(remote.cand[i]);
  // A symmetric sequential peer: its next ports are predictable.
  if (remote.nat.type == NatType::SymSeq && remote.nat.last.valid()) {
    for (int k = 1; k <= 64; k++) {
      int port = int(remote.nat.last.port) + k * int(remote.nat.step);
      if (port <= 0 || port >= 65536) continue;
      Candidate c;
      c.kind = Kind::Predicted;
      c.addr = remote.nat.last;
      c.addr.port = uint16_t(port);
      c.priority = priority(Kind::Predicted, c.addr);
      add_pair(c);
    }
  }
  state_ = State::Checking;
  check_end_ = now + cfg_.check_ms;
  settling_ = false;
  tick(now);
}

Kind Agent::local_kind(const Pair& p) const {
  for (size_t i = 0; i < local_.n; i++)
    if (local_.cand[i].addr.same(p.seen)) return local_.cand[i].kind;
  return Kind::PeerRefl;
}

void Agent::choose(uint32_t now) {
  const Pair* best = nullptr;
  int best_cls = 99;
  for (size_t i = 0; i < n_pairs_; i++) {
    const Pair& p = pairs_[i];
    if (!p.ok) continue;
    int c = pair_class(local_kind(p), p.remote.kind, p.remote.addr);
    if (!best || c < best_cls || (c == best_cls && p.rtt < best->rtt)) best = &p, best_cls = c;
  }
  if (!best) {
    fail("no address pair answered");
    return;
  }
  path_.local.kind = local_kind(*best);
  path_.local.addr = best->seen;
  path_.remote = best->remote;
  path_.rtt_ms = best->rtt;
  path_.cls = best_cls;
  peer_ = best->remote.addr;
  last_rx_ = last_tx_ = now;
  state_ = State::Connected;
}

// ---- the link (core/direct: "MDD" | 1 | seq(8) | sealed) ----

bool Agent::send(const uint8_t* d, size_t n) {
  if (state_ != State::Connected || n > kMaxPayload || sock_ < 0) return false;
  uint8_t b[kLinkHead + kMaxPayload + 16];
  std::memcpy(b, kDataMagic, 4);
  seq_++;
  for (int i = 0; i < 8; i++) b[4 + i] = uint8_t(seq_ >> (56 - 8 * i));
  uint8_t nonce[12] = {};
  std::memcpy(nonce + 4, b + 4, 8);
  aead_seal(seal_key_, nonce, b, kLinkHead, d, n, b + kLinkHead);
  return net_.send(sock_, peer_, b, kLinkHead + n + 16);
}

bool Agent::fresh(uint64_t seq) const {
  if (seq == 0) return false;
  if (seq > max_rx_) return true;
  if (max_rx_ - seq >= 64) return false;
  return (window_ & (uint64_t(1) << (max_rx_ - seq))) == 0;
}

void Agent::mark(uint64_t seq) {
  if (seq > max_rx_) {
    uint64_t shift = seq - max_rx_;
    window_ = shift >= 64 ? 0 : window_ << shift;
    window_ |= 1;
    max_rx_ = seq;
    return;
  }
  window_ |= uint64_t(1) << (max_rx_ - seq);
}

void Agent::on_data(const Addr& from, const uint8_t* d, size_t n, uint32_t now) {
  if (state_ != State::Connected || n < kLinkHead + 16 || n > kLinkHead + kMaxPayload + 16) return;
  uint64_t seq = 0;
  for (int i = 0; i < 8; i++) seq = seq << 8 | d[4 + i];
  if (!fresh(seq)) return;
  uint8_t nonce[12] = {};
  std::memcpy(nonce + 4, d + 4, 8);
  uint8_t p[kMaxPayload];
  if (!aead_open(open_key_, nonce, d, kLinkHead, d + kLinkHead, n - kLinkHead, p)) return;
  mark(seq);
  last_rx_ = now;
  if (!from.same(peer_)) peer_ = from;  // one socket: the peer moved (NAT rebinding, new network)
  size_t len = n - kLinkHead - 16;
  if (len && recv_fn_) recv_fn_(recv_ctx_, p, len);
}

// ---- driving ----

void Agent::on_datagram(const Addr& from, const uint8_t* d, size_t n, uint32_t now) {
  if (n >= 4 && std::memcmp(d, kProbeMagic, 4) == 0) {
    on_probe(from, d, n, now);
  } else if (n >= 4 && std::memcmp(d, kDataMagic, 4) == 0) {
    on_data(from, d, n, now);
  } else if (n >= 20 && (d[0] & 0xC0) == 0 && d[4] == 0x21 && d[5] == 0x12 && d[6] == 0xA4 && d[7] == 0x42) {
    on_stun(d, n);
    if (state_ == State::Gathering) {
      bool all = true;
      for (size_t i = 0; i < n_refl_; i++) all = all && refl_[i].done;
      if (all) finish_gather(now);
    }
  }
}

void Agent::tick(uint32_t now) {
  switch (state_) {
    case State::Gathering: {
      bool pending = false;
      for (size_t i = 0; i < n_refl_; i++) {
        Refl& r = refl_[i];
        if (r.done) continue;
        if (r.tries >= 3 && after(now, r.next)) {
          r.done = true;  // no answer
          continue;
        }
        pending = true;
        if (r.tries < 3 && after(now, r.next) && sock_ >= 0) {
          uint8_t req[20] = {0x00, 0x01, 0x00, 0x00, 0x21, 0x12, 0xA4, 0x42};
          std::memcpy(req + 8, r.tx, 12);
          net_.send(sock_, r.dst, req, sizeof req);
          r.tries++;
          r.next = now + 300 * r.tries;
        }
      }
      if (!pending || after(now, t0_ + cfg_.gather_ms)) finish_gather(now);
      return;
    }
    case State::Checking: {
      for (size_t i = 0; i < n_pairs_; i++) {
        Pair& p = pairs_[i];
        if (p.ok || !after(now, p.next) || sock_ < 0) continue;
        uint8_t out[80];
        size_t k = probe(kProbeReq, p.tx, Addr(), out, sizeof out);
        net_.send(sock_, p.remote.addr, out, k);
        p.sent = now;
        p.next = now + p.interval;
        p.interval = p.interval * 2 < 1000 ? p.interval * 2 : 1000;
      }
      if (settling_ && after(now, settle_)) {
        choose(now);
      } else if (!settling_ && after(now, check_end_)) {
        bool sym = remote_.nat.type == NatType::SymRandom || remote_.nat.type == NatType::SymSeq;
        bool usym = local_.nat.type == NatType::SymRandom || local_.nat.type == NatType::SymSeq;
        fail(local_.nat.type == NatType::Blocked || remote_.nat.type == NatType::Blocked
                 ? "a network on the way blocks UDP"
             : usym && sym ? "both sides are behind strict (symmetric) NATs: try Wi-Fi"
             : usym        ? "this network's NAT is strict (symmetric): try another network"
             : sym         ? "the other side's NAT is strict (symmetric)"
                           : "no address pair answered (firewall or NAT on the way)");
      }
      return;
    }
    case State::Connected:
      if (now - last_rx_ > kIdleAfter) {
        reason_ = "the other side went silent";
        close();
        return;
      }
      if (now - last_tx_ >= kKeepEvery) {
        last_tx_ = now;
        send(nullptr, 0);  // keepalive
      }
      return;
    default:
      return;
  }
}

}  // namespace call
}  // namespace mesh
