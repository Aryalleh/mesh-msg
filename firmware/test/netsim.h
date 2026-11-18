// An in-memory network for the direct path and calls: hosts on private
// networks, NATs (endpoint-independent or symmetric mapping, port-restricted
// filtering) and a STUN reflector. Time moves in 5 ms steps.
#pragma once

#include <cstdint>
#include <functional>
#include <map>
#include <vector>

#include "check.h"
#include "direct.h"

namespace netsim {

namespace mc = mesh::call;
using check::Bytes;

inline void rnd(uint8_t* out, size_t n) {
  static uint32_t x = 0x12345678;
  for (size_t i = 0; i < n; i++) {
    x = x * 1103515245u + 12345u;
    out[i] = uint8_t(x >> 16);
  }
}

inline uint64_t key_of(const mc::Addr& a) {
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
  // Datagrams for sockets with no bound agent (a call manager owns the agent).
  std::function<void(const mc::Addr&, const uint8_t*, size_t, uint32_t)> rx;
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
        if (s.open && s.h == h && s.port == to.port) {
          if (s.agent)
            s.agent->on_datagram(p.from, p.d.data(), p.d.size(), now);
          else if (h->rx)
            h->rx(p.from, p.d.data(), p.d.size(), now);
          return;
        }
    }
  }

  void step(uint32_t dt, std::vector<mc::Agent*> agents) {
    step(dt, [&](uint32_t n) {
      for (mc::Agent* a : agents) a->tick(n);
    });
  }

  // Advances time in 5 ms steps; tick runs after each step's deliveries.
  void step(uint32_t dt, const std::function<void(uint32_t)>& tick) {
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
      tick(now);
    }
  }

  void bind(mc::Agent& a) {
    if (a.socket() >= 0) socks[size_t(a.socket())].agent = &a;
  }
};

inline int Host::open() {
  w->socks.push_back(Sock{this, w->next_port++});
  return int(w->socks.size() - 1);
}
inline void Host::close(int s) { w->socks[size_t(s)].open = false; }
inline uint16_t Host::port(int s) { return w->socks[size_t(s)].port; }

inline bool Host::send(int s, const mc::Addr& to, const uint8_t* d, size_t n) {
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

}  // namespace netsim
