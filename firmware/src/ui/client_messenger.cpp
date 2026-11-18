#include "client_messenger.h"

#include "../board_audio.h"

#include <Arduino.h>
#include <LittleFS.h>

#include <cstring>
#include <ctime>
#include <memory>

#include "board.h"
#include "../calls.h"
#include "client.h"
#include "hostlink.h"
#include "net.h"
#include "../wallclock.h"

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

bool g_calls_on = false;

class ClientMessenger final : public Messenger {
 private:
  std::string error_;

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
  bool call(const std::string& id) override {
    int i = g_client->find(id);
    error_.clear();
    if (i < 0) {
      error_ = "مخاطب پیدا نشد / no such contact";
    } else if (!g_calls_on) {
      error_ = "تماس روی این برد راه نیفتاد / calls did not start on this board";
    } else if (!net::wifi_up() && net::mode() != net::Mode::Setup) {
      error_ = "تماس به وای‌فای نیاز دارد / calls need Wi-Fi";
    } else if (!wallclock::trusted()) {
      error_ = "ساعت برد هنوز تنظیم نشده / the board's clock is not set yet";
    } else if (!calls::start(g_client->contacts()[size_t(i)].pub)) {
      error_ = "الان در تماس هستید / already in a call";
    }
    return error_.empty();
  }
  std::string call_error() override { return error_; }
  void hang_up() override { calls::hangup(); }
  bool answer() override { return calls::answer(); }
  void set_muted(bool m) override { calls::set_muted(m); }
  uint32_t call_revision() override { return calls::revision(); }
  bool take_video(const uint8_t** jpeg, size_t* n) override { return calls::take_video(jpeg, n); }
  CallView call_view() override {
    calls::View v = calls::view();
    CallView c;
    switch (v.state) {
      case mesh::call::State::Idle: c.state = CallView::State::None; break;
      case mesh::call::State::Outgoing: c.state = CallView::State::Outgoing; break;
      case mesh::call::State::Ringing: c.state = CallView::State::Ringing; break;
      case mesh::call::State::Connecting: c.state = CallView::State::Connecting; break;
      case mesh::call::State::Active: c.state = CallView::State::Active; break;
      case mesh::call::State::Ended: c.state = CallView::State::Ended; break;
    }
    c.incoming = v.incoming;
    c.code = v.code;
    c.reason = v.reason;
    c.missed = v.missed;
    c.muted = v.muted;
    c.peer_muted = v.peer_muted;
    c.video_offered = v.video_offered;
    c.video = v.video;
    c.path = v.path_cls;
    if (v.state == mesh::call::State::Active) c.seconds = (millis() - v.active_ms) / 1000;
    for (const auto& k : g_client->contacts())
      if (std::memcmp(k.pub.x, v.peer.x, 32) == 0) c.peer = k.label();
    if (c.peer.empty()) {
      uint8_t h[16];
      v.peer.hash(h);
      char hx[17];
      for (int j = 0; j < 8; j++) snprintf(hx + 2 * j, 3, "%02x", h[j]);
      c.peer = hx;
    }
    return c;
  }
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

bool calls_on() { return g_calls_on; }

std::string client_state() {
  if (!g_client) return "off";
  const auto& s = g_client->stats();
  char n[200];
  snprintf(n, sizeof n,
           "; messages for it %lu (opened %lu, unreadable %lu), sent %lu, delivered %lu, call signals %lu, contacts %u",
           (unsigned long)s.data_in, (unsigned long)s.opened, (unsigned long)s.unreadable, (unsigned long)s.sent,
           (unsigned long)s.receipts_in, (unsigned long)s.calls_in, unsigned(g_client->contacts().size()));
  std::string others;
  for (const auto& d : g_client->other_dests()) {
    char x[40];
    snprintf(x, sizeof x, "%s%02x%02x%02x%02x%02x%02x%02x%02x (%lu)", others.empty() ? "" : ", ", d.dest[0], d.dest[1],
             d.dest[2], d.dest[3], d.dest[4], d.dest[5], d.dest[6], d.dest[7], (unsigned long)d.n);
    others += x;
  }
  return (g_unix ? "on, announced as " + g_client->name() : std::string("on, waiting for the clock (NTP or relay)")) +
         ", id " + g_client->id() + n + (others.empty() ? "" : "; messages heard for other accounts: " + others);
}

Messenger* client_begin(const char* name) {
  uint8_t seed[mesh::kSeedSize];
  if (board::storage_read("/cl-seed", seed, sizeof seed) != sizeof seed) {
    board::random_bytes(seed, sizeof seed);  // the user identity, separate from the node's
    if (!board::storage_write("/cl-seed", seed, sizeof seed)) return nullptr;
  }
  g_client.reset(new mc::Client(seed, g_flash, [](uint8_t* b, size_t n) { board::random_bytes(b, n); }));
  memset(seed, 0, sizeof seed);
  // Calls (calls.h): signals in through the client, out through client_pump.
  g_calls_on = calls::begin(g_client->identity());
  g_client->set_call_handler([](const mesh::PublicKey& from, bool contact, const uint8_t* sig, size_t n) {
    calls::on_signal(from, contact, sig, n);
  });
  if (g_client->name().empty()) g_client->set_name(name);
  net::set_account(&g_account);  // the browser app uses this same account
  return &g_messenger;
}

void client_pump(mesh::node::Node& node, mesh::route::Millis now) {
  if (!g_client) return;
  g_unix = node.unix_time(now);
  // Ask the relay for our own account's messages, announce or not (the
  // node otherwise serves only destinations it heard announced).
  static mesh::route::Millis served_at = 0;
  if (!served_at || now - served_at > 10000) {
    served_at = now | 1;
    uint8_t dest[16];
    g_client->pub().hash(dest);
    node.serve(dest);
  }
  if (!g_attached || !node.host_attached(kPort)) {
    uint8_t hello[2] = {mesh::host::kOpHello, 0};  // attach; the stored backlog streams to us
    feed(node, hello, sizeof hello, mesh::host::kKindCommand, now);
    g_attached = true;
  }
  // The node → the client: everything it carries (packet frames), events dropped.
  const uint32_t opened = g_client->stats().opened;
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
  if (g_client->stats().opened > opened) audio::play(audio::Sound::Message);  // a new message for us
  // The call task's signals (content type 7, PRIORITY) → the client.
  {
    mesh::PublicKey to;
    static uint8_t sig[mesh::call::kMaxSignal];
    size_t n = 0;
    uint32_t t = g_unix ? g_unix : uint32_t(time(nullptr));
    while (calls::take_signal(to, sig, sizeof sig, &n)) g_client->send_call(to, sig, n, t);
  }
  // The client → the node: its messages, receipts and announces.
  g_client->tick(g_unix);
  for (auto& p : g_client->take_outgoing()) feed(node, p.data(), p.size(), mesh::host::kKindPacket, now);
}

}  // namespace ui
