// The handheld's own messaging client (docs/handheld-proto.md §8): a user
// identity, contacts and conversations on the device, speaking the same
// protocol as the apps (docs/protocol.md): DATA end-to-end encrypted with
// Noise (e2e.h), fragmented to LoRa size, signed RECEIPTs, ANNOUNCEs with a
// nickname. It sees every packet the node carries (its own host port) and
// hands the node the packets it makes; the node carries them on LoRa,
// Bluetooth, USB and the relay. Pure C++: host-tested.
#pragma once

#include <cstddef>
#include <cstdint>
#include <functional>
#include <string>
#include <vector>

#include "e2e.h"
#include "identity.h"
#include "packet.h"

namespace mesh {
namespace client {

// Where the client keeps its files (LittleFS on the board, memory in tests).
class Storage {
 public:
  virtual ~Storage() = default;
  virtual bool read(const std::string& name, std::string& out) = 0;
  virtual bool write(const std::string& name, const std::string& data) = 0;
};

struct Contact {
  PublicKey pub;
  std::string name;        // from their announce or contact link; "" = unknown
  bool saved = false;      // messaged with (or added); else only heard nearby
  bool knows_me = false;   // they have our key (they wrote to us): Noise K
  uint32_t last = 0;       // Unix time of the last activity
  std::string id() const;  // 16 hex digits of the destination hash
  std::string label() const;
};

struct Message {
  bool mine = false;
  bool delivered = false;  // a signed receipt came back (mine only)
  uint32_t time = 0;
  uint8_t packet_id[8] = {};
  std::string text;
};

constexpr size_t kMaxContacts = 64;
constexpr size_t kMaxMessages = 100;  // per conversation, oldest dropped
constexpr uint32_t kAnnounceEvery = 30 * 60;

class Client {
 public:
  // seed: the user identity (64 bytes). random fills bytes (packet IDs, Noise e).
  Client(const uint8_t seed[kSeedSize], Storage& storage, std::function<void(uint8_t*, size_t)> random);

  const PublicKey& pub() const { return id_.pub(); }
  void set_name(const std::string& name);  // nickname in announces and the contact link
  const std::string& name() const { return name_; }
  // https://eu-1.mfadem.ir/add#<base64url(x||ed)>?n=<name>, what the apps add.
  std::string link() const;

  // A packet the node carries (raw): our DATA, receipts, announces.
  void on_packet(const uint8_t* raw, size_t n, uint32_t now);
  // Encrypts text to contact (index into contacts()); false + err if not.
  bool send_text(size_t contact, const std::string& text, uint32_t now, std::string* err = nullptr);
  // A call signal (content type 7) to peer, with the PRIORITY flag (ringing
  // must not wait behind queued messages). Contacts get Noise K when they
  // know us, anyone else Noise X (a REJECT to a stranger).
  bool send_call(const PublicKey& peer, const uint8_t* sig, size_t n, uint32_t now);
  // Call signals that arrive: from, whether they are a saved contact (calls
  // are for contacts only), the signal. Not stored, not a message.
  using CallHandler = std::function<void(const PublicKey& from, bool contact, const uint8_t* sig, size_t n)>;
  void set_call_handler(CallHandler h) { on_call_ = std::move(h); }
  // A saved contact's index, or -1.
  int contact_of(const PublicKey& pub) const;
  // The user identity (for the call handshake).
  const Identity& identity() const { return id_; }

  // Adds a contact from a link / meshmsg: URI; returns its index or -1.
  int add_contact(const std::string& link, uint32_t now);
  // Names a contact (keeps them as a saved contact).
  bool rename(size_t contact, const std::string& name);
  // Announces us (at most every kAnnounceEvery, and only once the time is known).
  void tick(uint32_t now);
  void announce_now() { last_announce_ = 0; }

  // Packets for the node to carry, encoded (raw); taken once.
  std::vector<std::vector<uint8_t>> take_outgoing();

  const std::vector<Contact>& contacts() const { return contacts_; }
  const std::vector<Message>& messages(size_t contact) const;
  int find(const std::string& id) const;
  // Since start, for the board log: DATA packets (or fragments) addressed to
  // us, messages opened, messages we could not open (not for this key, or
  // from someone we can't read yet), messages sent, call signals received.
  struct Stats {
    uint32_t data_in = 0, opened = 0, unreadable = 0, sent = 0, receipts_in = 0, calls_in = 0;
  };
  const Stats& stats() const { return stats_; }
  // Messages heard for other accounts, newest first (16 hex digits of the
  // destination, as Contact::id), with how many: shows a phone that writes
  // to an old account of this board.
  struct OtherDest {
    uint8_t dest[8];
    uint32_t n;
  };
  const std::vector<OtherDest>& other_dests() const { return others_; }
  std::string id() const;  // this account, 16 hex digits
  // Grows with every change the UI shows (new message, receipt, contact).
  uint32_t revision() const { return rev_; }

  // Parses a contact link / URI (exposed for tests).
  static bool parse_link(const std::string& s, PublicKey& pub, std::string& name);

 private:
  int upsert(const PublicKey& pub, uint32_t now);
  void add_message(size_t contact, const Message& m);
  void queue(const Packet& p);
  bool send_content(const PublicKey& to, bool known, uint8_t ct, const uint8_t* content, size_t n, bool priority,
                    uint32_t now, uint8_t packet_id[8], std::string* err);
  void on_data(const Packet& p, uint32_t now);
  bool on_message(const Packet& p, const e2e::Opened& o, uint32_t now);
  bool seen(const uint8_t id[8]);
  void load();
  void save_contacts();
  void save_messages(size_t contact);
  std::string file_for(size_t contact) const;

  Identity id_;
  uint8_t dest_[16] = {};
  Storage& storage_;
  std::function<void(uint8_t*, size_t)> random_;
  std::string name_;
  std::vector<Contact> contacts_;
  std::vector<std::vector<Message>> messages_;
  std::vector<std::vector<uint8_t>> out_;
  // Fragments being collected: packet ID → fragments' raw bytes.
  struct Partial {
    uint8_t id[8];
    uint32_t first;
    std::vector<std::vector<uint8_t>> frags;
  };
  std::vector<Partial> partial_;
  uint8_t seen_[64][8] = {};
  size_t seen_n_ = 0;
  uint32_t last_announce_ = 0;
  uint32_t rev_ = 1;
  Stats stats_;
  std::vector<OtherDest> others_;
  CallHandler on_call_;
};

std::string base64url(const uint8_t* b, size_t n);
bool unbase64url(const std::string& s, std::vector<uint8_t>& out);

}  // namespace client
}  // namespace mesh
