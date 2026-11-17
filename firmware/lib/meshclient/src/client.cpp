#include "client.h"

#include <cctype>
#include <cstring>

#include "announce.h"
#include "e2e.h"
#include "textenc.h"

namespace mesh {
namespace client {
namespace {

const char kLinkBase[] = "https://eu-1.mfadem.ir/add#";
const char kScheme[] = "meshmsg:";
// Largest content that fits in kMaxFragments LoRa frames with first-contact overhead (as core/message).
constexpr size_t kMaxContent = kMaxFragments * kFragBodySize - e2e::kOverheadX - 32 - e2e::kInnerHeader;
constexpr uint32_t kPartialTtl = 600;

std::string hex(const uint8_t* b, size_t n) {
  static const char d[] = "0123456789abcdef";
  std::string s;
  for (size_t i = 0; i < n; i++) {
    s += d[b[i] >> 4];
    s += d[b[i] & 15];
  }
  return s;
}

void put32(std::string& s, uint32_t v) {
  for (int i = 3; i >= 0; i--) s += char(v >> (8 * i));
}
uint32_t get32(const uint8_t* p) { return uint32_t(p[0]) << 24 | uint32_t(p[1]) << 16 | uint32_t(p[2]) << 8 | p[3]; }

std::string url_escape(const std::string& s) {
  static const char d[] = "0123456789ABCDEF";
  std::string o;
  for (unsigned char c : s) {
    if (std::isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~') {
      o += char(c);
    } else if (c == ' ') {
      o += '+';
    } else {
      o += '%';
      o += d[c >> 4];
      o += d[c & 15];
    }
  }
  return o;
}

std::string url_unescape(const std::string& s) {
  std::string o;
  for (size_t i = 0; i < s.size(); i++) {
    if (s[i] == '+') {
      o += ' ';
    } else if (s[i] == '%' && i + 2 < s.size() && std::isxdigit(uint8_t(s[i + 1])) && std::isxdigit(uint8_t(s[i + 2]))) {
      o += char(std::stoi(s.substr(i + 1, 2), nullptr, 16));
      i += 2;
    } else {
      o += s[i];
    }
  }
  return o;
}

}  // namespace

std::string base64url(const uint8_t* b, size_t n) {
  static const char a[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
  std::string s;
  for (size_t i = 0; i < n; i += 3) {
    uint32_t v = uint32_t(b[i]) << 16 | (i + 1 < n ? uint32_t(b[i + 1]) << 8 : 0) | (i + 2 < n ? b[i + 2] : 0);
    s += a[v >> 18 & 63];
    s += a[v >> 12 & 63];
    if (i + 1 < n) s += a[v >> 6 & 63];
    if (i + 2 < n) s += a[v & 63];
  }
  return s;
}

bool unbase64url(const std::string& s, std::vector<uint8_t>& out) {
  out.clear();
  uint32_t v = 0;
  int bits = 0;
  for (char c : s) {
    int x = c >= 'A' && c <= 'Z' ? c - 'A' : c >= 'a' && c <= 'z' ? c - 'a' + 26 : c >= '0' && c <= '9' ? c - '0' + 52
            : c == '-' ? 62 : c == '_' ? 63 : -1;
    if (x < 0) return false;
    v = v << 6 | uint32_t(x);
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out.push_back(uint8_t(v >> bits));
    }
  }
  return bits < 6;  // no stray 6-bit group
}

std::string Contact::id() const {
  uint8_t h[16];
  pub.hash(h);
  return hex(h, 8);
}

std::string Contact::label() const { return name.empty() ? "…" + id().substr(0, 8) : name; }

Client::Client(const uint8_t seed[kSeedSize], Storage& storage, std::function<void(uint8_t*, size_t)> random)
    : id_(Identity::from_seed(seed)), storage_(storage), random_(std::move(random)) {
  id_.pub().hash(dest_);
  load();
}

void Client::set_name(const std::string& name) {
  name_ = name.substr(0, 32);
  storage_.write("name", name_);
  last_announce_ = 0;  // tell people the new name
}

std::string Client::link() const {
  uint8_t b[kPublicSize];
  std::memcpy(b, id_.pub().x, 32);
  std::memcpy(b + 32, id_.pub().ed, 32);
  std::string s = kLinkBase + base64url(b, sizeof b);
  if (!name_.empty()) s += "?n=" + url_escape(name_);
  return s;
}

bool Client::parse_link(const std::string& in, PublicKey& pub, std::string& name) {
  std::string s = in;
  while (!s.empty() && std::isspace(uint8_t(s.back()))) s.pop_back();
  while (!s.empty() && std::isspace(uint8_t(s.front()))) s.erase(0, 1);
  std::string rest;
  if (s.rfind("https://", 0) == 0 || s.rfind("http://", 0) == 0) {
    size_t h = s.find('#');
    if (h == std::string::npos) return false;
    std::string page = s.substr(0, h);
    while (!page.empty() && page.back() == '/') page.pop_back();
    if (page.size() < 4 || page.compare(page.size() - 4, 4, "/add") != 0) return false;
    rest = s.substr(h + 1);
  } else if (s.rfind(kScheme, 0) == 0) {
    rest = s.substr(sizeof kScheme - 1);
  } else {
    return false;
  }
  size_t q = rest.find('?');
  std::vector<uint8_t> key;
  if (!unbase64url(rest.substr(0, q), key) || key.size() != kPublicSize) return false;
  pub = PublicKey::from_bytes(key.data());
  name.clear();
  if (q != std::string::npos) {
    std::string query = rest.substr(q + 1);
    size_t at = 0;
    while (at <= query.size()) {
      size_t amp = query.find('&', at);
      std::string kv = query.substr(at, amp == std::string::npos ? std::string::npos : amp - at);
      if (kv.rfind("n=", 0) == 0) name = url_unescape(kv.substr(2)).substr(0, 32);
      if (amp == std::string::npos) break;
      at = amp + 1;
    }
  }
  return true;
}

int Client::find(const std::string& id) const {
  for (size_t i = 0; i < contacts_.size(); i++)
    if (contacts_[i].id() == id) return int(i);
  return -1;
}

const std::vector<Message>& Client::messages(size_t contact) const {
  static const std::vector<Message> none;
  return contact < messages_.size() ? messages_[contact] : none;
}

int Client::upsert(const PublicKey& pub, uint32_t now) {
  for (size_t i = 0; i < contacts_.size(); i++)
    if (std::memcmp(contacts_[i].pub.x, pub.x, 32) == 0 && std::memcmp(contacts_[i].pub.ed, pub.ed, 32) == 0)
      return int(i);
  if (contacts_.size() >= kMaxContacts) {
    // Full: forget the oldest one we never talked to.
    int oldest = -1;
    for (size_t i = 0; i < contacts_.size(); i++)
      if (!contacts_[i].saved && (oldest < 0 || contacts_[i].last < contacts_[size_t(oldest)].last)) oldest = int(i);
    if (oldest < 0) return -1;
    contacts_.erase(contacts_.begin() + oldest);
    messages_.erase(messages_.begin() + oldest);
  }
  Contact c;
  c.pub = pub;
  c.last = now;
  contacts_.push_back(c);
  messages_.emplace_back();
  rev_++;
  return int(contacts_.size() - 1);
}

int Client::add_contact(const std::string& link, uint32_t now) {
  PublicKey pub;
  std::string name;
  if (!parse_link(link, pub, name) || std::memcmp(pub.x, id_.pub().x, 32) == 0) return -1;
  int i = upsert(pub, now);
  if (i < 0) return -1;
  Contact& c = contacts_[size_t(i)];
  c.saved = true;
  if (!name.empty()) c.name = name;
  rev_++;
  save_contacts();
  return i;
}

void Client::add_message(size_t contact, const Message& m) {
  auto& list = messages_[contact];
  if (list.size() >= kMaxMessages) list.erase(list.begin());
  list.push_back(m);
  rev_++;
  save_messages(contact);
}

void Client::queue(const Packet& p) {
  std::vector<uint8_t> raw(p.size());
  size_t n = 0;
  if (encode(p, raw.data(), raw.size(), &n) != Err::Ok) return;
  raw.resize(n);
  out_.push_back(std::move(raw));
}

std::vector<std::vector<uint8_t>> Client::take_outgoing() {
  std::vector<std::vector<uint8_t>> o;
  o.swap(out_);
  return o;
}

bool Client::send_text(size_t ci, const std::string& text, uint32_t now, std::string* err) {
  auto fail = [&](const char* why) {
    if (err) *err = why;
    return false;
  };
  if (ci >= contacts_.size()) return fail("no such contact");
  if (text.empty()) return fail("empty");
  std::string content;
  uint8_t ct = textenc::encode(text, content);
  if (content.size() > kMaxContent) return fail("too long");
  Contact& c = contacts_[ci];
  Packet p;
  p.type = Type::Data;
  p.max_hops = kDefaultMaxHops;
  random_(p.id, 8);
  c.pub.hash(p.dest);
  uint8_t eph[32];
  random_(eph, sizeof eph);
  std::vector<uint8_t> body(content.size() + e2e::kOverheadX + 32 + e2e::kInnerHeader);
  size_t n = e2e::seal(id_, c.pub, c.knows_me, p.id, eph, ct, now, reinterpret_cast<const uint8_t*>(content.data()),
                       content.size(), body.data(), body.size());
  std::memset(eph, 0, sizeof eph);
  if (!n) return fail("encryption failed");
  p.body = body.data();
  p.body_len = n;
  size_t count = fragment_count(p, kLoRaMTU);
  if (!count) return fail("too long");
  for (size_t i = 0; i < count; i++) {
    Packet f;
    if (fragment(p, kLoRaMTU, i, f) != Err::Ok) return fail("fragment");
    queue(f);
  }
  Message m;
  m.mine = true;
  m.time = now;
  std::memcpy(m.packet_id, p.id, 8);
  m.text = text;
  c.saved = true;
  c.last = now;
  add_message(ci, m);
  save_contacts();
  return true;
}

bool Client::seen(const uint8_t id[8]) {
  for (size_t i = 0; i < seen_n_ && i < 64; i++)
    if (std::memcmp(seen_[i], id, 8) == 0) return true;
  std::memcpy(seen_[seen_n_ % 64], id, 8);
  seen_n_++;
  return false;
}

void Client::on_packet(const uint8_t* raw, size_t n, uint32_t now) {
  Packet p;
  if (decode(raw, n, p) != Err::Ok) return;
  switch (p.type) {
    case Type::Announce: {
      Announce a;
      if (parse_announce(p, a) != AnnounceErr::Ok) return;
      if (std::memcmp(p.dest, dest_, 16) == 0 || (a.app.flags & kFlagNode)) return;  // us, or a board
      int i = upsert(a.pub, now);
      if (i < 0) return;
      Contact& c = contacts_[size_t(i)];
      std::string nm(a.app.name);
      if (!nm.empty() && nm != c.name) {
        c.name = nm;
        rev_++;
        if (c.saved) save_contacts();
      }
      if (!c.saved) c.last = now;
      return;
    }
    case Type::Receipt: {
      uint8_t acked[8];
      if (receipt_acked(p, acked) != AnnounceErr::Ok) return;
      for (size_t ci = 0; ci < messages_.size(); ci++) {
        for (Message& m : messages_[ci]) {
          if (!m.mine || m.delivered || std::memcmp(m.packet_id, acked, 8) != 0) continue;
          uint8_t a2[8];
          if (verify_receipt(p, contacts_[ci].pub, a2) != AnnounceErr::Ok) return;
          m.delivered = true;
          rev_++;
          save_messages(ci);
          return;
        }
      }
      return;
    }
    case Type::Data: {
      if (std::memcmp(p.dest, dest_, 16) != 0) return;
      if (!p.fragmented()) return on_data(p, now);
      // Collect fragments until the set is complete.
      for (size_t i = 0; i < partial_.size();)
        if (now - partial_[i].first > kPartialTtl) partial_.erase(partial_.begin() + long(i));
        else i++;
      Partial* pt = nullptr;
      for (auto& x : partial_)
        if (std::memcmp(x.id, p.id, 8) == 0) pt = &x;
      if (!pt) {
        if (partial_.size() >= 8) partial_.erase(partial_.begin());
        partial_.push_back(Partial{});
        pt = &partial_.back();
        std::memcpy(pt->id, p.id, 8);
        pt->first = now;
      }
      for (auto& f : pt->frags) {
        Packet q;
        if (decode(f.data(), f.size(), q) == Err::Ok && q.frag_index == p.frag_index) return;  // duplicate
      }
      pt->frags.emplace_back(raw, raw + n);
      if (pt->frags.size() < p.frag_count) return;
      std::vector<Packet> fs(pt->frags.size());
      for (size_t i = 0; i < fs.size(); i++) decode(pt->frags[i].data(), pt->frags[i].size(), fs[i]);
      std::vector<uint8_t> body(kMaxFragments * kFragBodySize);
      Packet full;
      Err e = reassemble(fs.data(), fs.size(), body.data(), body.size(), full);
      if (e == Err::Ok) on_data(full, now);
      for (size_t i = 0; i < partial_.size(); i++)
        if (std::memcmp(partial_[i].id, p.id, 8) == 0) {
          partial_.erase(partial_.begin() + long(i));
          break;
        }
      return;
    }
    default:
      return;
  }
}

void Client::on_data(const Packet& p, uint32_t now) {
  bool dup = seen(p.id);
  if (!dup) {
    std::vector<PublicKey> pubs;
    for (auto& c : contacts_) pubs.push_back(c.pub);
    std::vector<uint8_t> plain(p.body_len);
    e2e::Opened o;
    if (!e2e::open(id_, pubs.data(), pubs.size(), p.id, p.body, p.body_len, plain.data(), plain.size(), o)) return;
    int ci = o.contact >= 0 ? o.contact : upsert(o.from, now);
    if (ci < 0) return;
    Contact& c = contacts_[size_t(ci)];
    c.saved = true;
    c.knows_me = true;  // they wrote to us: they have our key
    c.last = now;
    if (o.content_type == e2e::kText || o.content_type == e2e::kTextCompact) {
      Message m;
      m.time = o.time ? o.time : now;
      std::memcpy(m.packet_id, p.id, 8);
      if (textenc::decode(o.content_type, o.content, o.len, m.text)) add_message(size_t(ci), m);
    }
    save_contacts();
    rev_++;
  }
  // The signed receipt: the sender's tick turns, and nodes holding the message purge it.
  uint8_t buf[kReceiptBody];
  Packet r;
  if (build_receipt(id_, p.id, kDefaultMaxHops, buf, sizeof buf, r) == AnnounceErr::Ok) queue(r);
}

void Client::tick(uint32_t now) {
  if (now < 1700000000u) return;  // no clock yet: an announce must carry the time
  if (last_announce_ && now - last_announce_ < kAnnounceEvery) return;
  AppData app;
  std::strncpy(app.name, name_.c_str(), 32);
  app.flags = kFlagDelegate;
  uint8_t pid[8], buf[kMaxAnnounceBody];
  random_(pid, 8);
  Packet a;
  if (build_announce(id_, now, app, kDefaultMaxHops, pid, buf, sizeof buf, a) != AnnounceErr::Ok) return;
  queue(a);
  last_announce_ = now;
}

// ---- files ----

std::string Client::file_for(size_t contact) const { return "m-" + contacts_[contact].id(); }

void Client::save_contacts() {
  std::string s;
  for (const Contact& c : contacts_) {
    if (!c.saved) continue;  // people only heard nearby are not kept
    s.append(reinterpret_cast<const char*>(c.pub.x), 32);
    s.append(reinterpret_cast<const char*>(c.pub.ed), 32);
    s += char((c.saved ? 1 : 0) | (c.knows_me ? 2 : 0));
    put32(s, c.last);
    s += char(c.name.size());
    s += c.name;
  }
  storage_.write("contacts", s);
}

void Client::save_messages(size_t ci) {
  std::string s;
  for (const Message& m : messages_[ci]) {
    s += char((m.mine ? 1 : 0) | (m.delivered ? 2 : 0));
    put32(s, m.time);
    s.append(reinterpret_cast<const char*>(m.packet_id), 8);
    size_t n = m.text.size() > 0xFFFF ? 0xFFFF : m.text.size();
    s += char(n >> 8);
    s += char(n);
    s.append(m.text, 0, n);
  }
  storage_.write(file_for(ci), s);
}

void Client::load() {
  storage_.read("name", name_);
  std::string s;
  if (!storage_.read("contacts", s)) return;
  const uint8_t* b = reinterpret_cast<const uint8_t*>(s.data());
  size_t i = 0;
  while (i + 70 <= s.size() && contacts_.size() < kMaxContacts) {
    Contact c;
    std::memcpy(c.pub.x, b + i, 32);
    std::memcpy(c.pub.ed, b + i + 32, 32);
    c.saved = b[i + 64] & 1;
    c.knows_me = b[i + 64] & 2;
    c.last = get32(b + i + 65);
    size_t nl = b[i + 69];
    if (i + 70 + nl > s.size()) break;
    c.name.assign(s, i + 70, nl);
    i += 70 + nl;
    contacts_.push_back(c);
    messages_.emplace_back();
    std::string m;
    if (!storage_.read(file_for(contacts_.size() - 1), m)) continue;
    const uint8_t* mb = reinterpret_cast<const uint8_t*>(m.data());
    size_t k = 0;
    while (k + 15 <= m.size()) {
      Message msg;
      msg.mine = mb[k] & 1;
      msg.delivered = mb[k] & 2;
      msg.time = get32(mb + k + 1);
      std::memcpy(msg.packet_id, mb + k + 5, 8);
      size_t tl = size_t(mb[k + 13]) << 8 | mb[k + 14];
      if (k + 15 + tl > m.size()) break;
      msg.text.assign(m, k + 15, tl);
      k += 15 + tl;
      messages_.back().push_back(msg);
    }
  }
}

}  // namespace client
}  // namespace mesh
