// Bounded packet store for a node (docs/firmware.md §3 store/PacketStore).
// A CLIENT keeps what it hears for its phone while the phone is away; a
// GATEWAY keeps what it hears for its relay while the uplink is down. Each
// sink (phone, uplink) has its own "already has it" bit, so a reconnecting
// sink gets exactly the backlog it is missing, receipts first (as the
// backlog of core/router).
//
// Fixed-size slots, no heap. Every change to a slot is reported to an
// optional Persist hook as a fixed-size Record, so a flash backend is one
// file of kSlots records (LittleFS) and restore() reloads it after reboot.
// Times are node-uptime seconds; restored items restart their TTL from the
// restore time (a node has no reliable wall clock).
#pragma once

#include <cstddef>
#include <cstdint>

#include "packet.h"
#include "router.h"

namespace mesh {
namespace store {

using route::Key;
using route::Priority;

#ifndef MESH_STORE_SLOTS
#define MESH_STORE_SLOTS 64
#endif
constexpr size_t kSlots = MESH_STORE_SLOTS;

enum Sink : uint8_t {
  kSinkPhone = 1 << 0,
  kSinkUplink = 1 << 1,
};

enum class Origin : uint8_t { Air = 0, Phone = 1, Uplink = 2, Local = 3 };

constexpr uint32_t kRecordMagic = 0x4d534731;  // "MSG1"

// One slot as written to flash. Plain old data, fixed size.
struct Record {
  uint32_t magic;
  uint32_t seq;
  uint32_t expires_s;
  uint8_t origin;
  uint8_t sinks;  // Sink bits already holding the packet
  uint8_t len;
  uint8_t pad;
  uint8_t raw[kLoRaMTU];
};

class Persist {
 public:
  virtual void save(size_t slot, const Record& r) = 0;
  virtual void erase(size_t slot) = 0;

 protected:
  ~Persist() = default;
};

enum class Put : uint8_t { Stored, Duplicate, Invalid, Rejected };

struct Item {
  const uint8_t* raw;  // valid until the store changes
  size_t len;
  uint32_t seq;
  Origin origin;
  uint8_t sinks;
  Packet packet;  // body aliases raw
};

struct Stats {
  uint32_t stored = 0, dup = 0, evicted = 0, expired = 0, deleted = 0, quota = 0;
};

class Store {
 public:
  // per_dest_max caps DATA slots per destination (0 = kSlots / 4), so one
  // busy or abusive destination cannot fill the store.
  explicit Store(Persist* persist = nullptr, size_t per_dest_max = 0);

  // put stores raw (an encoded packet, no link header). When full, expired
  // items go first, then the least valuable: oldest ANNOUNCE / SUMMARY, then
  // oldest DATA; RECEIPTs (small, they free everyone's queues) go last.
  // `sinks` marks sinks that already have it (e.g. kSinkUplink for a packet
  // that came from the uplink).
  Put put(const uint8_t* raw, size_t n, Origin origin, uint8_t sinks, uint32_t now_s, uint32_t ttl_s);
  bool has(const Key& k) const;
  bool get(const Key& k, Item& out) const;

  // delete_data removes every DATA fragment with packet ID acked.
  size_t delete_data(const uint8_t acked[8]);
  size_t expire(uint32_t now_s);

  // next_for returns the most urgent item sink does not have yet: RECEIPT,
  // then priority DATA, DATA, ANNOUNCE, SUMMARY; oldest first within each.
  bool next_for(uint8_t sink, Item& out) const;
  void mark(uint32_t seq, uint8_t sink);
  // A sink disconnected and lost its state (e.g. a new phone paired).
  void reset_sink(uint8_t sink);

  // restore loads a record read back from flash into its slot. Records that
  // are malformed, or duplicate one already loaded, are rejected.
  bool restore(size_t slot, const Record& r, uint32_t now_s, uint32_t ttl_s);

  size_t size() const;
  const Stats& stats() const { return stats_; }

 private:
  struct Slot {
    bool used = false;
    Key key;
    Type type = Type::Data;
    Priority prio = Priority::Data;
    uint8_t dest[16] = {};
    Record rec{};
  };
  int find(const Key& k) const;
  int victim(uint32_t now_s, Priority incoming) const;
  int free_slot() const;
  void drop(int i);
  void save(int i);
  static bool fill(Slot& s, const uint8_t* raw, size_t n);
  void item(int i, Item& out) const;

  Persist* persist_;
  size_t per_dest_max_;
  Slot s_[kSlots];
  uint32_t seq_ = 1;
  Stats stats_;
};

}  // namespace store
}  // namespace mesh
