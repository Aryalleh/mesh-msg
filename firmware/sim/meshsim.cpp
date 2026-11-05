// meshnode-sim: runs N real firmware nodes (node::Node: router, MAC, store,
// host link) on a simulated LoRa radio, in real time on a Linux host. Each
// node's host link is a pseudo-terminal, so meshd attaches to a simulated
// node exactly as to a board on USB:
//
//   make -C firmware sim
//   firmware/build/meshnode-sim -n 3 -topology line
//   → node 0: /dev/pts/7   (meshd config: [lora] enabled = true, port = "/dev/pts/7")
//
// Radio model (as test/net_test.cpp): exact airtime per the node's preset,
// half-duplex, collisions at a receiver when two neighbours overlap, CAD
// senses transmitting neighbours. Topology: line (each node hears only its
// neighbours: 0-1-2-…) or full (everyone hears everyone).
#include <fcntl.h>
#include <poll.h>
#include <termios.h>
#include <unistd.h>

#include <cerrno>
#include <chrono>
#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <memory>
#include <string>
#include <vector>

#include "node.h"

using mesh::route::Millis;

namespace {

volatile std::sig_atomic_t g_stop = 0;
bool g_verbose = false;

struct SimBoard : mesh::node::Platform {
  int index = 0;
  void save_config(const mesh::host::NodeConfig& c) override {
    std::printf("node %d: config saved (role %d, region %s, preset %s, %d dBm, name \"%s\")\n", index, c.role,
                mesh::host::kRegions[c.region].name, mesh::host::kPresets[c.preset].name, c.tx_dbm, c.name);
  }
  void apply_radio(const mesh::host::NodeConfig& c) override {
    std::printf("node %d: radio retuned to %.3f MHz\n", index, c.channel_hz() / 1e6);
  }
  void wipe() override { std::printf("node %d: wipe requested (simulated)\n", index); }
  void reboot() override {}
  uint16_t battery_mv() override { return 0; }
};

struct SimNode {
  SimBoard board;
  std::unique_ptr<mesh::node::Node> node;
  int master = -1, slave = -1;
  std::string path;
  std::vector<uint8_t> out;  // host bytes not yet written to the pty
  std::vector<int> nbrs;
  bool txing = false;
  Millis tx_end = 0;
};

struct Tx {
  int from;
  Millis start, end;
  std::vector<uint8_t> frame;
};

Millis now_ms() {
  using namespace std::chrono;
  static const auto t0 = steady_clock::now();
  return Millis(duration_cast<milliseconds>(steady_clock::now() - t0).count());
}

bool open_pty(SimNode& n) {
  n.master = posix_openpt(O_RDWR | O_NOCTTY);
  if (n.master < 0 || grantpt(n.master) != 0 || unlockpt(n.master) != 0) return false;
  const char* name = ptsname(n.master);
  if (!name) return false;
  n.path = name;
  // Hold the slave open ourselves: the master then never reports hang-up
  // while no meshd is attached, and the line discipline is raw from the start.
  n.slave = open(name, O_RDWR | O_NOCTTY);
  if (n.slave < 0) return false;
  termios t{};
  tcgetattr(n.slave, &t);
  cfmakeraw(&t);
  tcsetattr(n.slave, TCSANOW, &t);
  fcntl(n.master, F_SETFL, fcntl(n.master, F_GETFL) | O_NONBLOCK);
  return true;
}

void usage() {
  std::fprintf(stderr,
               "usage: meshnode-sim [-n N] [-topology line|full] [-region ID] [-preset 0|1|2] [-v]\n"
               "  -n         number of nodes (default 3)\n"
               "  -topology  line: 0-1-2-… each hears only its neighbours (default); full: all hear all\n"
               "  -region    frequency plan ID (default 3 = US_915, no duty cycle; 0 = EU_868 10 %%)\n"
               "  -preset    0 FAST (default), 1 BALANCED, 2 LONG\n");
}

}  // namespace

int main(int argc, char** argv) {
  int count = 3, region = 3, preset = 0;
  std::string topology = "line";
  for (int i = 1; i < argc; i++) {
    std::string a = argv[i];
    auto next = [&]() -> const char* {
      if (i + 1 >= argc) {
        usage();
        std::exit(2);
      }
      return argv[++i];
    };
    if (a == "-n")
      count = std::atoi(next());
    else if (a == "-topology")
      topology = next();
    else if (a == "-region")
      region = std::atoi(next());
    else if (a == "-preset")
      preset = std::atoi(next());
    else if (a == "-v")
      g_verbose = true;
    else {
      usage();
      return 2;
    }
  }
  if (count < 1 || count > 64 || (topology != "line" && topology != "full") || region < 0 ||
      size_t(region) >= mesh::host::kRegionCount || preset < 0 || size_t(preset) >= mesh::host::kPresetCount) {
    usage();
    return 2;
  }
  std::signal(SIGINT, [](int) { g_stop = 1; });
  std::signal(SIGTERM, [](int) { g_stop = 1; });
  std::setvbuf(stdout, nullptr, _IOLBF, 0);

  std::vector<std::unique_ptr<SimNode>> nodes;
  for (int i = 0; i < count; i++) {
    auto n = std::make_unique<SimNode>();
    n->board.index = i;
    mesh::host::NodeConfig cfg;
    cfg.region = uint8_t(region);
    cfg.preset = uint8_t(preset);
    cfg.tx_dbm = 10;
    std::snprintf(cfg.name, sizeof cfg.name, "sim-%d", i);
    uint8_t seed[64];
    for (int k = 0; k < 64; k++) seed[k] = uint8_t(0x5e + i * 17 + k * 3);
    n->node = std::make_unique<mesh::node::Node>(cfg, seed, n->board, nullptr, uint32_t(0xc0ffee + i));
    if (!open_pty(*n)) {
      std::perror("pty");
      return 1;
    }
    nodes.push_back(std::move(n));
  }
  for (int i = 0; i < count; i++)
    for (int j = 0; j < count; j++)
      if (i != j && (topology == "full" || j == i - 1 || j == i + 1)) nodes[i]->nbrs.push_back(j);

  std::printf("meshnode-sim: %d nodes, %s topology, %s, %s\n", count, topology.c_str(),
              mesh::host::kRegions[region].name, mesh::host::kPresets[preset].name);
  for (int i = 0; i < count; i++)
    std::printf("node %d: %s  (short id 0x%02x)\n", i, nodes[i]->path.c_str(), nodes[i]->node->short_id());
  std::printf("ready\n");

  std::vector<Tx> log;
  std::vector<pollfd> pfds(nodes.size());
  Millis last_tick = 0;
  uint8_t buf[2048];
  while (!g_stop) {
    for (size_t i = 0; i < nodes.size(); i++) pfds[i] = pollfd{nodes[i]->master, POLLIN, 0};
    poll(pfds.data(), pfds.size(), 2);
    Millis now = now_ms();

    // Host side.
    for (size_t i = 0; i < nodes.size(); i++) {
      SimNode& n = *nodes[i];
      if (pfds[i].revents & POLLIN) {
        ssize_t r = read(n.master, buf, sizeof buf);
        if (r > 0) n.node->host_rx(buf, size_t(r), now);
      }
      size_t k;
      while ((k = n.node->host_tx(buf, sizeof buf)) > 0) n.out.insert(n.out.end(), buf, buf + k);
      if (!n.out.empty()) {
        ssize_t w = write(n.master, n.out.data(), n.out.size());
        if (w > 0) n.out.erase(n.out.begin(), n.out.begin() + w);
        if (n.out.size() > 64 * 1024) n.out.clear();  // nobody reading for a long time
      }
    }

    // Finished transmissions reach neighbours that were listening.
    for (size_t t = 0; t < log.size(); t++) {
      Tx& tx = log[t];
      if (tx.frame.empty() || tx.end > now) continue;
      SimNode& from = *nodes[tx.from];
      from.txing = false;
      from.node->radio_tx_done(now);
      for (int j : from.nbrs) {
        bool lost = false;
        for (const Tx& o : log) {
          if (&o == &tx || o.end <= tx.start || o.start >= tx.end) continue;
          if (o.from == j) lost = true;  // half-duplex
          for (int k2 : nodes[j]->nbrs)
            if (o.from == k2 && k2 != tx.from) lost = true;  // collision
        }
        if (lost) {
          if (g_verbose) std::printf("  node %d lost a frame from node %d (collision / half-duplex)\n", j, tx.from);
          continue;
        }
        nodes[j]->node->radio_rx(tx.frame.data(), tx.frame.size(), 0, now);
      }
      tx.frame.clear();  // delivered; kept a while for overlap checks
    }
    while (!log.empty() && log.front().frame.empty() && log.front().end + 30000 < now) log.erase(log.begin());

    // Radio side.
    for (size_t i = 0; i < nodes.size(); i++) {
      SimNode& n = *nodes[i];
      if (n.txing) continue;
      mesh::mac::Action a = n.node->radio_next(now);
      if (a.kind == mesh::mac::Kind::Cad) {
        bool busy = false;
        for (int j : n.nbrs) busy |= nodes[j]->txing;
        n.node->radio_cad(busy, now);
        a = n.node->radio_next(now);
      }
      if (a.kind == mesh::mac::Kind::Transmit) {
        n.txing = true;
        n.tx_end = now + a.air_ms;
        log.push_back(Tx{int(i), now, n.tx_end, std::vector<uint8_t>(a.frame->data, a.frame->data + a.frame->len)});
        if (g_verbose) {
          mesh::Packet p;
          const char* type = "?";
          if (mesh::decode(a.frame->data + 1, a.frame->len - 1, p) == mesh::Err::Ok) type = mesh::type_name(p.type);
          std::printf("node %zu tx %s %zu B, %u ms on air\n", i, type, a.frame->len, a.air_ms);
        }
      }
    }

    if (now - last_tick >= 1000) {
      last_tick = now;
      for (auto& n : nodes) n->node->tick(now);
    }
  }
  std::printf("meshnode-sim: stopped\n");
  for (auto& n : nodes) {
    close(n->slave);
    close(n->master);
  }
  return 0;
}
