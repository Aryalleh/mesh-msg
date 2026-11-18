// The handheld's messaging client: two clients talk through a "network" that
// hands each one the other's packets.
#include <cstring>
#include <map>
#include <random>

#include "check.h"
#include "client.h"
#include "packet.h"

namespace mc = mesh::client;

namespace {

struct MemStore : mc::Storage {
  std::map<std::string, std::string> files;
  bool read(const std::string& n, std::string& out) override {
    auto it = files.find(n);
    if (it == files.end()) return false;
    out = it->second;
    return true;
  }
  bool write(const std::string& n, const std::string& d) override {
    files[n] = d;
    return true;
  }
};

std::mt19937 g_rng(7);
void rnd(uint8_t* b, size_t n) {
  for (size_t i = 0; i < n; i++) b[i] = uint8_t(g_rng());
}

std::vector<uint8_t> seed(uint8_t k) {
  std::vector<uint8_t> s(64);
  for (size_t i = 0; i < 64; i++) s[i] = uint8_t(k * 3 + i);
  return s;
}

// Delivers everything a sent to b (and returns how many packets).
size_t deliver(mc::Client& from, mc::Client& to, uint32_t now) {
  auto out = from.take_outgoing();
  for (auto& p : out) to.on_packet(p.data(), p.size(), now);
  return out.size();
}

constexpr uint32_t T = 1767225600;

void two_clients() {
  MemStore sa, sb;
  auto ka = seed(1), kb = seed(2);
  mc::Client a(ka.data(), sa, rnd), b(kb.data(), sb, rnd);
  a.set_name("Ali");
  b.set_name("Sara");

  // B's link, as shown in its QR code; A adds B.
  std::string link = b.link();
  CHECK(link.rfind("https://eu-1.mfadem.ir/add#", 0) == 0 && link.find("?n=Sara") != std::string::npos);
  int bi = a.add_contact(link, T);
  CHECK(bi == 0 && a.contacts()[0].name == "Sara" && a.contacts()[0].saved);
  CHECK(a.add_contact("not a link", T) == -1 && a.add_contact(a.link(), T) == -1);
  uint32_t r0 = a.revision();
  CHECK(a.rename(0, "Sara J") && a.contacts()[0].name == "Sara J" && a.revision() > r0);
  CHECK(a.rename(0, "Sara") && !a.rename(9, "x"));

  // First message: Noise X (B doesn't know A); B learns A and answers with Noise K.
  CHECK(a.send_text(0, "سلام سارا", T));
  CHECK(deliver(a, b, T + 1) == 1);
  CHECK(b.contacts().size() == 1 && b.contacts()[0].knows_me && b.messages(0).size() == 1);
  CHECK(b.messages(0)[0].text == "سلام سارا" && !b.messages(0)[0].mine && b.messages(0)[0].time == T);
  // B's receipt turns A's tick.
  CHECK(deliver(b, a, T + 2) == 1);
  CHECK(a.messages(0)[0].mine && a.messages(0)[0].delivered);

  // A long Persian answer is fragmented and reassembled (out of order too).
  std::string longtext;
  for (int i = 0; i < 40; i++) longtext += "پیام بلند از سارا ";
  CHECK(b.send_text(0, longtext, T + 3));
  auto frags = b.take_outgoing();
  CHECK(frags.size() > 1);
  for (size_t i = frags.size(); i-- > 0;) a.on_packet(frags[i].data(), frags[i].size(), T + 4);
  a.on_packet(frags[0].data(), frags[0].size(), T + 4);  // a duplicate fragment: ignored
  CHECK(a.messages(0).size() == 2 && a.messages(0)[1].text == longtext);
  CHECK(a.contacts()[0].knows_me);
  CHECK(deliver(a, b, T + 5) == 1);  // receipt
  CHECK(b.messages(0).back().delivered);

  // The same message again (another path): shown once, receipt sent again.
  for (auto& f : frags) a.on_packet(f.data(), f.size(), T + 6);
  CHECK(a.messages(0).size() == 2 && a.take_outgoing().size() == 1);

  // Announces: a stranger nearby shows up by name; boards (node flag) don't.
  MemStore sc;
  auto kc = seed(3);
  mc::Client c(kc.data(), sc, rnd);
  c.set_name("Reza");
  c.tick(T);
  CHECK(deliver(c, a, T) == 1);
  CHECK(a.contacts().size() == 2 && a.contacts()[1].name == "Reza" && !a.contacts()[1].saved);
  c.tick(T + 60);  // not again so soon
  CHECK(c.take_outgoing().empty());
  mc::Client noclock(kc.data(), sc, rnd);
  noclock.tick(100);  // no time yet: no announce
  CHECK(noclock.take_outgoing().empty());

  // Not for us / tampered: nothing happens.
  CHECK(a.send_text(0, "for Sara only", T + 7));
  auto pk = a.take_outgoing();
  c.on_packet(pk[0].data(), pk[0].size(), T + 7);
  CHECK(c.contacts().empty() || c.messages(0).empty());
  pk[0].back() ^= 1;
  size_t before = b.messages(0).size();
  b.on_packet(pk[0].data(), pk[0].size(), T + 7);
  CHECK(b.messages(0).size() == before);

  // Everything survives a restart; strangers heard nearby are not kept.
  uint32_t rev = a.revision();
  CHECK(rev > 1);
  mc::Client a2(ka.data(), sa, rnd);
  CHECK(a2.name() == "Ali" && a2.contacts().size() == 1 && a2.contacts()[0].name == "Sara");
  CHECK(a2.contacts()[0].knows_me && a2.messages(0).size() == 3 && a2.messages(0)[0].delivered);
  CHECK(a2.messages(0)[1].text == longtext && a2.find(a2.contacts()[0].id()) == 0);
}

// Call signals (content type 7): handed to the call handler with the
// sender and whether they are a saved contact; never messages, never make a
// stranger a contact; sent with PRIORITY.
void call_signals() {
  MemStore sa, sb, sc;
  auto ka = seed(4), kb = seed(5), kc = seed(6);
  mc::Client a(ka.data(), sa, rnd), b(kb.data(), sb, rnd), c(kc.data(), sc, rnd);
  CHECK(a.add_contact(b.link(), T) == 0);
  CHECK(a.send_text(0, "hi", T));
  deliver(a, b, T);
  deliver(b, a, T);  // receipt
  struct Got {
    mesh::PublicKey from;
    bool contact = false;
    std::vector<uint8_t> sig;
    int n = 0;
  } gb, ga;
  b.set_call_handler([&](const mesh::PublicKey& f, bool k, const uint8_t* d, size_t n) {
    gb.from = f, gb.contact = k, gb.sig.assign(d, d + n), gb.n++;
  });
  a.set_call_handler([&](const mesh::PublicKey& f, bool k, const uint8_t* d, size_t n) {
    ga.from = f, ga.contact = k, ga.sig.assign(d, d + n), ga.n++;
  });
  const uint8_t sig[] = {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 0, 9, 9};
  CHECK(a.send_call(b.pub(), sig, sizeof sig, T + 1));
  auto out = a.take_outgoing();
  CHECK(out.size() == 1);
  mesh::Packet p;
  CHECK(mesh::decode(out[0].data(), out[0].size(), p) == mesh::Err::Ok && p.priority());
  size_t msgs = b.messages(0).size();
  b.on_packet(out[0].data(), out[0].size(), T + 1);
  CHECK(gb.n == 1 && gb.contact && std::memcmp(gb.from.x, a.pub().x, 32) == 0);
  CHECK(gb.sig == std::vector<uint8_t>(sig, sig + sizeof sig));
  CHECK(b.messages(0).size() == msgs);
  CHECK(b.take_outgoing().size() == 1);  // the receipt
  b.on_packet(out[0].data(), out[0].size(), T + 1);  // the same packet again: once
  CHECK(gb.n == 1);
  // A stranger calls: handed over as not a contact, and stays a stranger.
  CHECK(c.send_call(a.pub(), sig, sizeof sig, T + 2));
  deliver(c, a, T + 2);
  CHECK(ga.n == 1 && !ga.contact && a.contact_of(c.pub()) < 0 && a.contact_of(b.pub()) == 0);
  for (auto& k : a.contacts()) CHECK(std::memcmp(k.pub.x, c.pub().x, 32) != 0 || !k.saved);
}

void links_and_base64() {
  std::vector<uint8_t> b(64);
  for (size_t i = 0; i < b.size(); i++) b[i] = uint8_t(i * 7);
  std::string s = mc::base64url(b.data(), b.size());
  CHECK(s.size() == 86 && s.find('=') == std::string::npos);
  std::vector<uint8_t> back;
  CHECK(mc::unbase64url(s, back) && back == b);
  CHECK(!mc::unbase64url("ab+c", back));
  // The meshmsg: form, a name with spaces and Persian, another host.
  mesh::PublicKey pub;
  std::string name;
  CHECK(mc::Client::parse_link("meshmsg:" + s + "?n=%D8%B9%D9%84%DB%8C+%D8%B1", pub, name) && name == "علی ر");
  CHECK(mc::Client::parse_link(" https://relay.example/add/#" + s + " ", pub, name) && name.empty());
  CHECK(!mc::Client::parse_link("https://relay.example/x#" + s, pub, name));
  CHECK(!mc::Client::parse_link("meshmsg:" + s.substr(1), pub, name));
}

}  // namespace

void run_client() {
  call_signals();
  two_clients();
  links_and_base64();
}
