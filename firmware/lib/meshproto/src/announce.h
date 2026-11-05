// Signed control packets a node can check without decrypting anything
// (mirrors core/announce): ANNOUNCE ("this identity exists") and RECEIPT
// ("this packet was delivered").
#pragma once

#include <cstddef>
#include <cstdint>

#include "identity.h"
#include "packet.h"

namespace mesh {

constexpr size_t kMaxAppData = 64;
constexpr size_t kAnnounceFixed = 64 + 4 + 1;  // keys || time || app-data length
constexpr size_t kMaxAnnounceBody = kAnnounceFixed + kMaxAppData + kSigSize;
constexpr size_t kReceiptBody = 8 + kSigSize;

// App-data TLV tags and flags.
constexpr uint8_t kTagName = 1;
constexpr uint8_t kTagFlags = 2;
constexpr uint8_t kFlagDelegate = 1 << 0;
constexpr uint8_t kFlagNode = 1 << 1;
constexpr uint8_t kFlagRelay = 1 << 2;

struct AppData {
  char name[kMaxAppData] = {};  // NUL-terminated; senders cap names at 32 bytes
  uint8_t flags = 0;
};

struct Announce {
  PublicKey pub;
  uint32_t time = 0;  // Unix seconds
  AppData app;
};

enum class AnnounceErr : uint8_t {
  Ok = 0,
  NotAnnounce,
  NotReceipt,
  Length,
  DestMismatch,   // keys / signer do not hash to the destination
  BadSignature,
  BadAppData,
  IdMismatch,     // receipt packet ID is not receipt_id(acked)
  TooSmall,       // caller's buffer too small
};

// parse_announce verifies an ANNOUNCE packet: the keys must hash to the
// destination and the signature must be valid.
AnnounceErr parse_announce(const Packet& p, Announce& out);

// build_announce signs an ANNOUNCE for id. The body is written to body_buf
// (at least kMaxAnnounceBody bytes); out.body points at it.
AnnounceErr build_announce(const Identity& id, uint32_t now, const AppData& app, uint8_t max_hops,
                           const uint8_t packet_id[8], uint8_t* body_buf, size_t cap, Packet& out);

// receipt_id is the deterministic packet ID of the receipt for acked:
// SHA-256("MESHMSG-RCPT-ID" || acked)[:8].
void receipt_id(const uint8_t acked[8], uint8_t out[8]);

// receipt_acked returns the acknowledged packet ID without verifying the
// signature (relays and nodes use it to clear stored packets).
AnnounceErr receipt_acked(const Packet& p, uint8_t acked[8]);

// verify_receipt checks a receipt against the recipient's public identity.
AnnounceErr verify_receipt(const Packet& p, const PublicKey& signer, uint8_t acked[8]);

// build_receipt signs the RECEIPT for a packet delivered to id. body_buf must
// hold kReceiptBody bytes.
AnnounceErr build_receipt(const Identity& id, const uint8_t acked[8], uint8_t max_hops, uint8_t* body_buf,
                          size_t cap, Packet& out);

}  // namespace mesh
