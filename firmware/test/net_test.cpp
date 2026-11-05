// Multi-node radio network on the host: every node runs the real Router and
// Mac; radios are half-duplex, overlapping transmissions at a receiver
// collide, and CAD senses neighbours that are transmitting. Checks that text
// crosses several hops (docs/roadmap.md M4: node → repeater → node) with one
// rebroadcast per node.
#include <cstdio>
#include <memory>
#include <vector>

#include "check.h"
#include "mac.h"
#include "packet.h"
#include "router.h"

using check::Bytes;
using mesh::route::Frame;
using mesh::route::Key;
using mesh::route::Millis;
using mesh::route::Verdict;

namespace {

struct Tx {
  int from;
  Millis start, end;
  Frame f;
};

struct Node {
  std::unique_ptr<mesh::route::Router> r;
  std::unique_ptr<mesh::mac::Mac> m;
  std::vector<int> nbrs;
  std::vector<int> snr;  // per neighbour, dB
  bool txing = false;
  Millis tx_end = 0;
  std::vector<Key> got;  // New verdicts
  int sent = 0;
};

class Net {
 public:
  explicit Net(size_t n, mesh::mac::Preset preset = mesh::mac::kFast) {
    for (size_t i = 0; i < n; i++) {
      Node node;
      mesh::route::Config rc;
      rc.short_id = uint8_t(i);
      rc.seed = uint32_t(1000 + i);
      node.r = std::make_unique<mesh::route::Router>(rc);
      mesh::mac::Config mc;
      mc.preset = preset;
      mc.seed = uint32_t(2000 + i);
      mc.duty_ppm = 100000;  // 10 % sub-band: keeps the test short
      node.m = std::make_unique<mesh::mac::Mac>(mc);
      nodes.push_back(std::move(node));
    }
  }

  void link(int a, int b, int snr = 0) {
    nodes[a].nbrs.push_back(b);
    nodes[a].snr.push_back(snr);
    nodes[b].nbrs.push_back(a);
    nodes[b].snr.push_back(snr);
  }

  bool transmitting_near(int j, Millis t) const {
    for (int k : nodes[j].nbrs)
      if (nodes[k].txing && nodes[k].tx_end > t) return true;
    return false;
  }

  void finish(const Tx& t) {
    for (size_t ni = 0; ni < nodes[t.from].nbrs.size(); ni++) {
      int j = nodes[t.from].nbrs[ni];
      bool lost = false;
      for (const Tx& o : log) {
        if (&o == &t || o.end <= t.start || o.start >= t.end) continue;
        // Half-duplex: j was transmitting itself; collision: another
        // neighbour of j overlapped.
        if (o.from == j) lost = true;
        for (int k : nodes[j].nbrs)
          if (o.from == k && k != t.from) lost = true;
      }
      if (lost) {
        collisions++;
        continue;
      }
      auto res = nodes[j].r->on_receive(t.f.data, t.f.len, nodes[t.from].snr[ni], t.end);
      if (res.verdict == Verdict::New) nodes[j].got.push_back(Key::of(res.packet));
    }
  }

  void run(Millis until, Millis step = 2) {
    for (; now < until; now += step) {
      for (size_t i = 0; i < nodes.size(); i++) {
        Node& n = nodes[i];
        if (n.txing && now >= n.tx_end) {
          n.txing = false;
          Key k;
          n.m->tx_done(now, &k);
          n.r->on_transmitted(k, now);
          for (auto& t : log)
            if (t.from == int(i) && t.end == n.tx_end) finish(t);
        }
      }
      for (size_t i = 0; i < nodes.size(); i++) {
        Node& n = nodes[i];
        Frame f;
        while (n.r->poll(now, f)) n.m->enqueue(f, now);
        if (n.txing) continue;
        mesh::mac::Action a = n.m->next(now);
        if (a.kind == mesh::mac::Kind::Cad) {
          n.m->cad_result(transmitting_near(int(i), now), now);
          a = n.m->next(now);
        }
        if (a.kind == mesh::mac::Kind::Transmit) {
          n.txing = true;
          n.tx_end = now + a.air_ms;
          n.sent++;
          log.push_back(Tx{int(i), now, n.tx_end, *a.frame});
        }
        if (now % 1000 == 0) n.r->tick(now);
      }
      // Forget transmissions that can no longer overlap anything.
      while (log.size() > 64 && log.front().end + 20000 < now) log.erase(log.begin());
    }
  }

  // Originates a DATA packet at node i.
  Key send(int i, uint8_t idbyte, uint8_t max_hops = 8, size_t body = 60) {
    Bytes b(body, 0x33);
    mesh::Packet p;
    p.type = mesh::Type::Data;
    p.max_hops = max_hops;
    for (int k = 0; k < 8; k++) p.id[k] = uint8_t(idbyte * 13 + k);
    for (int k = 0; k < 16; k++) p.dest[k] = uint8_t(0xe0 + k);
    p.body = b.data();
    p.body_len = b.size();
    Bytes raw(p.size());
    size_t n = 0;
    mesh::encode(p, raw.data(), raw.size(), &n);
    CHECK(nodes[i].r->send(raw.data(), n, true, now));
    return Key::of(p);
  }

  int count(int i, const Key& k) const {
    int c = 0;
    for (const auto& g : nodes[i].got)
      if (g == k) c++;
    return c;
  }

  std::vector<Node> nodes;
  std::vector<Tx> log;
  Millis now = 0;
  int collisions = 0;
};

void chain() {
  Net net(5);
  for (int i = 0; i < 4; i++) net.link(i, i + 1);
  Key k = net.send(0, 1);
  net.run(60000);
  for (int i = 1; i < 5; i++) CHECKC(net.count(i, k) == 1, "node " + std::to_string(i));
  // One transmission per node; the end of the chain rebroadcasts too (it
  // cannot know nobody is behind it), the origin needed no retry.
  for (int i = 0; i < 5; i++) CHECKC(net.nodes[i].sent == 1, "node " + std::to_string(i));
  CHECK(net.nodes[0].r->stats().acks == 1 && net.nodes[0].r->stats().retries == 0);
}

void hop_limit() {
  Net net(5);
  for (int i = 0; i < 4; i++) net.link(i, i + 1);
  Key k = net.send(0, 2, 2);
  net.run(60000);
  CHECK(net.count(1, k) == 1 && net.count(2, k) == 1 && net.count(3, k) == 1);
  CHECK(net.count(4, k) == 0);
  CHECK(net.nodes[3].sent == 0 && net.nodes[3].r->stats().hop_limit == 1);
}

void lone_node_retries() {
  Net net(1);
  net.send(0, 3);
  net.run(5 * 60000, 10);
  // No neighbour ever rebroadcasts: original + 2 retries, then gives up.
  CHECK(net.nodes[0].sent == 3 && net.nodes[0].r->stats().gave_up == 1);
}

void grid() {
  const int W = 5;
  Net net(W * W);
  uint32_t s = 99;
  auto rnd = [&]() {
    s ^= s << 13;
    s ^= s >> 17;
    s ^= s << 5;
    return s;
  };
  for (int y = 0; y < W; y++)
    for (int x = 0; x < W; x++) {
      int i = y * W + x;
      if (x + 1 < W) net.link(i, i + 1, int(rnd() % 30) - 20);
      if (y + 1 < W) net.link(i, i + W, int(rnd() % 30) - 20);
      if (x + 1 < W && y + 1 < W) net.link(i, i + W + 1, int(rnd() % 30) - 20);
    }
  std::vector<Key> keys;
  for (int m = 0; m < 6; m++) {
    keys.push_back(net.send(m % 2 ? W * W - 1 : 0, uint8_t(10 + m)));
    net.run(net.now + 8000);
  }
  net.run(net.now + 120000);
  int delivered = 0, total = 0, tx = 0;
  for (int i = 0; i < W * W; i++) {
    tx += net.nodes[i].sent;
    for (size_t m = 0; m < keys.size(); m++) {
      int origin = m % 2 ? W * W - 1 : 0;
      if (i == origin) continue;
      total++;
      int c = net.count(i, keys[m]);
      CHECK(c <= 1);
      delivered += c;
    }
  }
  std::printf("net: 5x5 grid, %d/%d delivered, %d transmissions for %zu messages (flood would be %d), %d lost to collisions\n",
              delivered, total, tx, keys.size(), int(keys.size()) * W * W, net.collisions);
  CHECK(delivered == total);
  // Cancellation saves airtime versus a plain flood (every node once per message).
  CHECK(tx < int(keys.size()) * W * W);
}

}  // namespace

void run_net() {
  chain();
  hop_limit();
  lone_node_retries();
  grid();
}
