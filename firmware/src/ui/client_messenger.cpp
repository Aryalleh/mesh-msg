#include "client_messenger.h"

#include <Arduino.h>
#include <LittleFS.h>

#include <memory>

#include "board.h"
#include "client.h"
#include "hostlink.h"
#include "net.h"

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

// ---- the same account for the browser app (net::Account, JSON) ----

// At most n bytes, never splitting a UTF-8 character.
std::string utf8_cut(const std::string& s, size_t n) {
  if (s.size() <= n) return s;
  while (n > 0 && (uint8_t(s[n]) & 0xC0) == 0x80) n--;
  return s.substr(0, n);
}

std::string js(const std::string& s) {
  std::string o = "\"";
  for (unsigned char c : s) {
    if (c == '"' || c == '\\') {
      o += '\\';
      o += char(c);
    } else if (c < 0x20) {
      char b[8];
      snprintf(b, sizeof b, "\\u%04x", c);
      o += b;
    } else {
      o += char(c);
    }
  }
  return o + "\"";
}

class BrowserAccount final : public net::Account {
 public:
  std::string me() override {
    uint8_t h[16];
    g_client->pub().hash(h);
    char hx[33];
    for (int i = 0; i < 16; i++) snprintf(hx + 2 * i, 3, "%02x", h[i]);
    return "{\"name\":" + js(g_client->name()) + ",\"link\":" + js(g_client->link()) + ",\"hash\":" + js(hx) + "}";
  }
  std::string chats() override {
    std::string o = "[";
    const auto& cs = g_client->contacts();
    for (size_t i = 0; i < cs.size(); i++) {
      if (i) o += ",";
      o += "{\"hash\":" + js(cs[i].id()) + ",\"name\":" + js(cs[i].label()) + ",\"saved\":" +
           (cs[i].saved ? "true" : "false") + ",\"last\":" + std::to_string(uint64_t(cs[i].last) * 1000);
      const auto& ms = g_client->messages(i);
      if (!ms.empty()) {  // the chat list's preview line
        const auto& m = ms.back();
        o += std::string(",\"preview\":{\"mine\":") + (m.mine ? "true" : "false") + ",\"text\":" +
             js(utf8_cut(m.text, 120)) + ",\"time\":" + std::to_string(uint64_t(m.time) * 1000) + "}";
      }
      o += "}";
    }
    return o + "]";
  }
  std::string messages(const std::string& hash) override {
    std::string o = "[";
    int i = g_client->find(hash);
    if (i >= 0) {
      bool first = true;
      for (const auto& m : g_client->messages(size_t(i))) {
        if (!first) o += ",";
        first = false;
        o += std::string("{\"mine\":") + (m.mine ? "true" : "false") + ",\"text\":" + js(m.text) +
             ",\"time\":" + std::to_string(uint64_t(m.time) * 1000) + ",\"status\":" +
             (m.delivered ? "\"delivered\"" : "\"sent\"") + "}";
      }
    }
    return o + "]";
  }
  bool send(const std::string& hash, const std::string& text) override {
    int i = g_client->find(hash);
    return i >= 0 && g_client->send_text(size_t(i), text, g_unix);
  }
  std::string add(const std::string& link, const std::string& name) override {
    int i = g_client->add_contact(link, g_unix);
    if (i < 0) return "";
    if (!name.empty()) g_client->rename(size_t(i), name);
    const auto& c = g_client->contacts()[size_t(i)];
    return "{\"hash\":" + js(c.id()) + ",\"name\":" + js(c.label()) + "}";
  }
  bool set_name(const std::string& name) override {
    g_client->set_name(name);
    return true;
  }
  uint32_t revision() override { return g_client->revision(); }
};

BrowserAccount g_account;

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
  net::set_account(&g_account);  // the browser app uses this same account
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
