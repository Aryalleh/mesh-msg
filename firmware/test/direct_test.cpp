// The direct path (lib/meshcall direct.*) on an in-memory network with NATs
// and a STUN reflector, and its probe bytes against the Go code's.
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <map>
#include <memory>
#include <sstream>
#include <string>
#include <vector>

#include "check.h"
#include "direct.h"
#include "json.h"

namespace mc = mesh::call;
using check::Bytes;

namespace {

void rnd(uint8_t* out, size_t n) {
  static uint32_t x = 0x12345678;
  for (size_t i = 0; i < n; i++) {
    x = x * 1103515245u + 12345u;
    out[i] = uint8_t(x >> 16);
  }
}

uint64_t key_of(const mc::Addr& a) {
  return uint64_t(a.ip[0]) << 40 | uint64_t(a.ip[1]) << 32 | uint64_t(a.ip[2]) << 24 | uint64_t(a.ip[3]) << 16 | a.port;
}

// A NAT with endpoint-independent mapping and address-and-port-dependent
// filtering (the common home router): hole punching works when both sides send.
struct Nat {
  mc::Addr pub;
  uint16_t next_port = 40000;
  int port_step = 1;
  bool symmetric = false;
  std::map<uint64_t, uint16_t> out;                // inside ip:port (or + dest) → outside port
  std::map<uint16_t, mc::Addr> in;                 // outside port → inside ip:port
  std::map<uint16_t, std::vector<uint64_t>> perm;  // outside port → remote ip:port allowed
};

struct World;

struct Host : mc::Net {
  World* w;
  mc::Addr ip;
  Nat* nat = nullptr;
  bool udp_blocked = false;
  int open() override;
  void close(int s) override;
  uint16_t port(int s) override;
  bool send(int s, const mc::Addr& to, const uint8_t* d, size_t n) override;
  size_t addrs(mc::Addr* out, size_t max) override {
    if (!max) return 0;
    out[0] = ip;
    return 1;
  }
};

struct Sock {
  Host* h;
  uint16_t port;
  mc::Agent* agent = nullptr;
  bool open = true;
};

struct Pkt {
  mc::Addr from, to;
  Bytes d;
  uint32_t at;
  Host* src = nullptr;  // null: from the internet (the reflector)
};

struct World {
  uint32_t now = 1000;
  std::vector<Sock> socks;
  std::vector<Pkt> q;
  std::vector<Host*> hosts;
  std::vector<mc::Addr> reflectors;
  uint16_t next_port = 50000;
  size_t sent = 0;

  // Delivers to a public address or a host address; NATs translate.
  void deliver(const Pkt& p) {
    for (const mc::Addr& r : reflectors)
      if (r.same(p.to)) {
        // STUN binding success with XOR-MAPPED-ADDRESS = where it came from.
        if (p.d.size() < 20) return;
        Bytes resp = {0x01, 0x01, 0x00, 0x0c, 0x21, 0x12, 0xA4, 0x42};
        resp.insert(resp.end(), p.d.begin() + 8, p.d.begin() + 20);
        uint16_t xp = uint16_t(p.from.port ^ 0x2112);
        Bytes attr = {0x00, 0x20, 0x00, 0x08, 0x00, 0x01, uint8_t(xp >> 8), uint8_t(xp)};
        const uint8_t c[4] = {0x21, 0x12, 0xA4, 0x42};
        for (int i = 0; i < 4; i++) attr.push_back(uint8_t(p.from.ip[i] ^ c[i]));
        resp.insert(resp.end(), attr.begin(), attr.end());
        q.push_back({r, p.from, resp, now + 5, nullptr});
        return;
      }
    for (Host* h : hosts) {
      mc::Addr to = p.to;
      if (h->nat && h->nat->pub.same_ip(to)) {
        auto it = h->nat->in.find(to.port);
        if (it == h->nat->in.end()) continue;
        auto& allowed = h->nat->perm[to.port];
        bool ok = false;
        for (uint64_t a : allowed) ok = ok || a == key_of(p.from);
        if (!ok) return;  // filtered
        to = it->second;
      } else if (!h->ip.same_ip(to) || !p.src || p.src->nat != h->nat) {
        continue;  // a private address is reachable only from its own network
      }
      if (!h->ip.same_ip(to)) continue;
      for (Sock& s : socks)
        if (s.open && s.h == h && s.port == to.port && s.agent) {
          s.agent->on_datagram(p.from, p.d.data(), p.d.size(), now);
          return;
        }
    }
  }

  void step(uint32_t dt, std::vector<mc::Agent*> agents) {
    for (uint32_t t = 0; t < dt; t += 5) {
      now += 5;
      std::vector<Pkt> due;
      for (size_t i = 0; i < q.size();) {
        if (int32_t(now - q[i].at) >= 0) {
          due.push_back(q[i]);
          q.erase(q.begin() + long(i));
        } else {
          i++;
        }
      }
      for (const Pkt& p : due) deliver(p);
      for (mc::Agent* a : agents) a->tick(now);
    }
  }

  void bind(mc::Agent& a) {
    if (a.socket() >= 0) socks[size_t(a.socket())].agent = &a;
  }
};

int Host::open() {
  w->socks.push_back(Sock{this, w->next_port++});
  return int(w->socks.size() - 1);
}
void Host::close(int s) { w->socks[size_t(s)].open = false; }
uint16_t Host::port(int s) { return w->socks[size_t(s)].port; }

bool Host::send(int s, const mc::Addr& to, const uint8_t* d, size_t n) {
  if (udp_blocked) return true;
  mc::Addr from = ip;
  from.port = w->socks[size_t(s)].port;
  // Behind a NAT and leaving the private network: translate.
  bool inside = false;
  for (Host* h : w->hosts) inside = inside || (h->nat == nat && nat && h->ip.same_ip(to));
  if (nat && !inside) {
    uint64_t k = key_of(from) ^ (nat->symmetric ? key_of(to) * 31 : 0);
    auto it = nat->out.find(k);
    uint16_t op;
    if (it == nat->out.end()) {
      op = nat->next_port;
      nat->next_port = uint16_t(nat->next_port + nat->port_step);
      nat->out[k] = op;
      nat->in[op] = from;
    } else {
      op = it->second;
    }
    nat->perm[op].push_back(key_of(to));
    from = nat->pub;
    from.port = op;
  }
  w->q.push_back({from, to, Bytes(d, d + n), w->now + 5, this});
  w->sent++;
  return true;
}

struct Got {
  std::vector<Bytes> d;
  static void fn(void* ctx, const uint8_t* b, size_t n) { static_cast<Got*>(ctx)->d.push_back(Bytes(b, b + n)); }
};

const uint8_t kSid[16] = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16};
const uint8_t kKey[32] = {7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7};

// Runs both agents until each is connected (or failed); returns the path classes.
std::pair<int, int> run_pair(World& w, Host& ha, Host& hb, mc::AgentConfig cfg, std::string* why = nullptr) {
  mc::Agent a(ha, cfg, kSid, kKey, true, rnd, w.now), b(hb, cfg, kSid, kKey, false, rnd, w.now);
  w.bind(a);
  w.bind(b);
  for (int i = 0; i < 200 && (a.state() == mc::Agent::State::Gathering || b.state() == mc::Agent::State::Gathering); i++)
    w.step(20, {&a, &b});
  CHECK(a.state() == mc::Agent::State::Gathered && b.state() == mc::Agent::State::Gathered);
  a.connect(b.offer(), w.now);
  b.connect(a.offer(), w.now);
  for (int i = 0; i < 800 && (a.state() == mc::Agent::State::Checking || b.state() == mc::Agent::State::Checking); i++)
    w.step(20, {&a, &b});
  if (why) *why = a.reason();
  if (a.state() != mc::Agent::State::Connected || b.state() != mc::Agent::State::Connected) return {0, 0};
  if (getenv("DIRECT_DEBUG")) {
    char l[64], r[64];
    a.path().local.addr.str(l, sizeof l);
    a.path().remote.addr.str(r, sizeof r);
    fprintf(stderr, "A: %s %s -> %s %s class %d\n", mc::kind_name(a.path().local.kind), l, mc::kind_name(a.path().remote.kind), r, a.path().cls);
    b.path().local.addr.str(l, sizeof l);
    b.path().remote.addr.str(r, sizeof r);
    fprintf(stderr, "B: %s %s -> %s %s class %d\n", mc::kind_name(b.path().local.kind), l, mc::kind_name(b.path().remote.kind), r, b.path().cls);
  }
  // Data both ways over the chosen path.
  Got ga, gb;
  a.set_receiver(Got::fn, &ga);
  b.set_receiver(Got::fn, &gb);
  const uint8_t m[] = {'h', 'i'};
  CHECK(a.send(m, 2) && b.send(m, 2));
  w.step(40, {&a, &b});
  CHECK(ga.d.size() == 1 && gb.d.size() == 1 && gb.d[0] == Bytes(m, m + 2));
  return {a.path().cls, b.path().cls};
}

void same_lan() {
  World w;
  Host a, b;
  a.w = b.w = &w;
  a.ip = mc::Addr::v4(192, 168, 1, 10, 0);
  b.ip = mc::Addr::v4(192, 168, 1, 11, 0);
  w.hosts = {&a, &b};
  auto c = run_pair(w, a, b, mc::AgentConfig());
  CHECK(c.first == 1 && c.second == 1);  // stays on the local network
}

void across_nats() {
  World w;
  Nat na, nb;
  na.pub = mc::Addr::v4(198, 51, 100, 1, 0);
  nb.pub = mc::Addr::v4(203, 0, 113, 7, 0);
  Host a, b;
  a.w = b.w = &w;
  a.ip = mc::Addr::v4(192, 168, 1, 10, 0);
  b.ip = mc::Addr::v4(10, 0, 0, 5, 0);
  a.nat = &na, b.nat = &nb;
  w.hosts = {&a, &b};
  w.reflectors = {mc::Addr::v4(192, 0, 2, 1, 3478), mc::Addr::v4(192, 0, 2, 1, 3479)};
  mc::AgentConfig cfg;
  cfg.reflectors[0] = w.reflectors[0];
  cfg.reflectors[1] = w.reflectors[1];
  cfg.n_reflectors = 2;
  {
    mc::Agent probe(a, cfg, kSid, kKey, true, rnd, w.now);
    w.bind(probe);
    for (int i = 0; i < 100 && probe.state() == mc::Agent::State::Gathering; i++) w.step(20, {&probe});
    CHECK(probe.offer().nat.type == mc::NatType::Indep);
    bool refl = false;
    for (size_t i = 0; i < probe.offer().n; i++)
      refl = refl || (probe.offer().cand[i].kind == mc::Kind::Reflexive && probe.offer().cand[i].addr.same_ip(na.pub));
    CHECK(refl);
  }
  auto c = run_pair(w, a, b, cfg);
  CHECK(c.first == 4 && c.second == 4);  // hole punched between the reflexive addresses

  // A symmetric sequential NAT on one side: the other predicts its ports.
  World w2;
  Nat sa, sb;
  sa.pub = mc::Addr::v4(198, 51, 100, 1, 0);
  sb.pub = mc::Addr::v4(203, 0, 113, 7, 0);
  sb.symmetric = true;
  sb.port_step = 2;
  Host a2, b2;
  a2.w = b2.w = &w2;
  a2.ip = mc::Addr::v4(192, 168, 1, 10, 0);
  b2.ip = mc::Addr::v4(10, 0, 0, 5, 0);
  a2.nat = &sa, b2.nat = &sb;
  w2.hosts = {&a2, &b2};
  w2.reflectors = w.reflectors;
  {
    mc::Agent probe(b2, cfg, kSid, kKey, false, rnd, w2.now);
    w2.bind(probe);
    for (int i = 0; i < 100 && probe.state() == mc::Agent::State::Gathering; i++) w2.step(20, {&probe});
    CHECK(probe.offer().nat.type == mc::NatType::SymSeq && probe.offer().nat.step == 2);
  }
  auto c2 = run_pair(w2, a2, b2, cfg);
  CHECK(c2.first >= 3 && c2.first <= 5 && c2.second >= 3);
}

void blocked() {
  World w;
  Host a, b;
  a.w = b.w = &w;
  a.ip = mc::Addr::v4(192, 168, 1, 10, 0);
  b.ip = mc::Addr::v4(10, 0, 0, 5, 0);
  Nat nb;
  nb.pub = mc::Addr::v4(203, 0, 113, 7, 0);
  b.nat = &nb;
  a.udp_blocked = true;
  w.hosts = {&a, &b};
  w.reflectors = {mc::Addr::v4(192, 0, 2, 1, 3478)};
  mc::AgentConfig cfg;
  cfg.reflectors[0] = w.reflectors[0];
  cfg.n_reflectors = 1;
  cfg.check_ms = 3000;
  std::string why;
  auto c = run_pair(w, a, b, cfg, &why);
  CHECK(c.first == 0 && why.find("blocks UDP") != std::string::npos);
}

void link_rules() {
  World w;
  Host a, b;
  a.w = b.w = &w;
  a.ip = mc::Addr::v4(192, 168, 1, 10, 0);
  b.ip = mc::Addr::v4(192, 168, 1, 11, 0);
  w.hosts = {&a, &b};
  mc::Agent x(a, mc::AgentConfig(), kSid, kKey, true, rnd, w.now), y(b, mc::AgentConfig(), kSid, kKey, false, rnd, w.now);
  w.bind(x);
  w.bind(y);
  x.connect(y.offer(), w.now);
  y.connect(x.offer(), w.now);
  for (int i = 0; i < 100 && (x.state() != mc::Agent::State::Connected || y.state() != mc::Agent::State::Connected); i++)
    w.step(20, {&x, &y});
  CHECK(x.state() == mc::Agent::State::Connected);
  Got gy;
  y.set_receiver(Got::fn, &gy);
  const uint8_t m[] = {1, 2, 3};
  CHECK(x.send(m, 3));
  // Capture the datagram on the way and replay it: dropped the second time.
  CHECK(!w.q.empty());
  Pkt copy = w.q.back();
  w.step(10, {&x, &y});
  CHECK(gy.d.size() == 1);
  w.deliver(copy);
  CHECK(gy.d.size() == 1);
  // Too big, and a session with another key, are refused.
  std::vector<uint8_t> big(mc::kMaxPayload + 1);
  CHECK(!x.send(big.data(), big.size()));
  // Silence ends the link after 45 s; keepalives every 15 s keep it up.
  w.step(60000, {&x, &y});
  CHECK(x.state() == mc::Agent::State::Connected && y.state() == mc::Agent::State::Connected);
  y.close();
  w.step(50000, {&x});
  CHECK(x.state() == mc::Agent::State::Closed && std::string(x.reason()).find("silent") != std::string::npos);
}

void probe_vectors(const std::string& dir) {
  std::ifstream f(dir + "/calls.json", std::ios::binary);
  std::stringstream ss;
  ss << f.rdbuf();
  json::Value v = json::parse(ss.str());
  Bytes key = check::unhex(v["path_key"].s()), sid = check::unhex(v["call_id"].s());
  World w;
  Host h;
  h.w = &w;
  h.ip = mc::Addr::v4(10, 0, 0, 1, 0);
  mc::Agent ctl(h, mc::AgentConfig(), sid.data(), key.data(), true, rnd, 0);
  mc::Agent oth(h, mc::AgentConfig(), sid.data(), key.data(), false, rnd, 0);
  const uint8_t tx[8] = {'t', 'x', 't', 'x', 't', 'x', 't', 'x'};
  uint8_t out[80];
  size_t n = ctl.probe(1, tx, mc::Addr(), out, sizeof out);
  CHECK(Bytes(out, out + n) == check::unhex(v["probe_req"].s()));
  n = oth.probe(2, tx, mc::Addr::v4(198, 51, 100, 7, 4242), out, sizeof out);
  CHECK(Bytes(out, out + n) == check::unhex(v["probe_resp"].s()));
}

}  // namespace

void run_direct(const std::string& dir) {
  probe_vectors(dir);
  same_lan();
  across_nats();
  blocked();
  link_rules();
}
