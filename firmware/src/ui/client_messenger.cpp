#include "client_messenger.h"

#include <Arduino.h>
#include <LittleFS.h>

#include <memory>

#include "board.h"
#include "client.h"
#include "hostlink.h"

namespace ui {
namespace {

namespace mc = mesh::client;
constexpr size_t kPort = mesh::node::Node::kLocalPort;

// Files on LittleFS: /cl-<name>.
class Flash final : public mc::Storage {
 public:
  bool read(const std::string& name, std::string& out) override {
    File f = LittleFS.open(("/cl-" + name).c_str(), "r");
    if (!f) return false;
    out.resize(f.size());
    size_t n = f.read(reinterpret_cast<uint8_t*>(&out[0]), out.size());
    f.close();
    out.resize(n);
    return true;
  }
  bool write(const std::string& name, const std::string& data) override {
    std::string path = "/cl-" + name, tmp = path + ".t";
    File f = LittleFS.open(tmp.c_str(), "w");
    if (!f) return false;
    bool ok = f.write(reinterpret_cast<const uint8_t*>(data.data()), data.size()) == data.size();
    f.close();
    return ok && LittleFS.rename(tmp.c_str(), path.c_str());
  }
};

Flash g_flash;
std::unique_ptr<mc::Client> g_client;
mesh::host::Decoder g_dec;
bool g_attached = false;
uint32_t g_unix = 0;  // the node's clock, for sending from the UI

class ClientMessenger final : public Messenger {
 public:
  bool connected() const override { return true; }
  std::vector<Contact> contacts() override {
    std::vector<Contact> out;
    const auto& cs = g_client->contacts();
    std::vector<size_t> order(cs.size());
    for (size_t i = 0; i < order.size(); i++) order[i] = i;
    // People we talk to first (newest first), then people heard nearby.
    std::sort(order.begin(), order.end(), [&](size_t a, size_t b) {
      if (cs[a].saved != cs[b].saved) return cs[a].saved;
      return cs[a].last > cs[b].last;
    });
    for (size_t i : order) out.push_back(Contact{cs[i].id(), cs[i].saved ? cs[i].label() : cs[i].label() + "  · نزدیک"});
    return out;
  }
  std::vector<Message> messages(const std::string& id) override {
    std::vector<Message> out;
    int i = g_client->find(id);
    if (i < 0) return out;
    for (const auto& m : g_client->messages(size_t(i))) out.push_back(Message{m.mine, m.text, m.time, m.delivered});
    return out;
  }
  bool send(const std::string& id, const std::string& text, uint32_t now) override {
    int i = g_client->find(id);
    return i >= 0 && g_client->send_text(size_t(i), text, now ? now : g_unix);
  }
  bool call(const std::string&) override { return false; }
  void hang_up() override {}
  std::string my_link() override { return g_client->link(); }
  std::string my_name() override { return g_client->name(); }
  uint32_t revision() override { return g_client->revision(); }
};

ClientMessenger g_messenger;

void feed(mesh::node::Node& node, const uint8_t* payload, size_t n, uint8_t kind, mesh::route::Millis now) {
  uint8_t f[mesh::host::kMaxEncoded];
  size_t e = mesh::host::encode(kind, payload, n, f, sizeof f);
  if (e) node.host_rx(kPort, f, e, now);
}

}  // namespace

Messenger* client_begin(const char* name) {
  uint8_t seed[mesh::kSeedSize];
  if (board::storage_read("/cl-seed", seed, sizeof seed) != sizeof seed) {
    board::random_bytes(seed, sizeof seed);  // the user identity, separate from the node's
    if (!board::storage_write("/cl-seed", seed, sizeof seed)) return nullptr;
  }
  g_client.reset(new mc::Client(seed, g_flash, [](uint8_t* b, size_t n) { board::random_bytes(b, n); }));
  memset(seed, 0, sizeof seed);
  if (g_client->name().empty()) g_client->set_name(name);
  return &g_messenger;
}

void client_pump(mesh::node::Node& node, mesh::route::Millis now) {
  if (!g_client) return;
  g_unix = node.unix_time(now);
  if (!g_attached || !node.host_attached(kPort)) {
    uint8_t hello[2] = {mesh::host::kOpHello, 0};  // attach; the stored backlog streams to us
    feed(node, hello, sizeof hello, mesh::host::kKindCommand, now);
    g_attached = true;
  }
  // The node → the client: everything it carries (packet frames), events dropped.
  uint8_t raw[512];
  for (int round = 0; round < 8; round++) {
    size_t m = node.host_tx(kPort, raw, sizeof raw);
    if (!m) break;
    size_t off = 0;
    while (off < m) {
      off += g_dec.feed(raw + off, m - off);
      if (g_dec.frame() && g_dec.kind() == mesh::host::kKindPacket)
        g_client->on_packet(g_dec.payload(), g_dec.payload_len(), g_unix);
    }
  }
  // The client → the node: its messages, receipts and announces.
  g_client->tick(g_unix);
  for (auto& p : g_client->take_outgoing()) feed(node, p.data(), p.size(), mesh::host::kKindPacket, now);
}

}  // namespace ui
