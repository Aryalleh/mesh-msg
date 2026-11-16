# Mava over Meshtastic

Mava uses Meshtastic as a transport/relay, not as the owner of Mava messages.

Mava owns end-to-end encryption, logical transfer IDs, chunk ordering/reassembly,
and duplicate suppression. Meshtastic owns radio framing, hop/rebroadcast
behavior, and its own packet/node IDs.

## Envelope

The transport envelope is 17 bytes:
- 0..1 magic: MV
- 2 version: 1
- 3 kind: DATA=1, ACK=2, CANCEL=3
- 4 flags
- 5..12 transfer_id
- 13..14 chunk_index
- 15..16 total_chunks
- 17.. body

The current Meshtastic API exposes a 233-byte application DATA payload limit,
so this leaves 216 bytes for Mava transfer data. The implementation rejects
larger payloads.

## Large transfers

A logical Mava body is split into independent chunks. The existing Mava
packet.Packet 16-fragment limit remains a per-packet limit and is no longer
the logical size limit for a transfer.

A receiver stores chunks by transfer_id and chunk_index and can reassemble
after all chunks arrive. Selective ACKs use a 16-bit bitmap.

## Meshtastic compatibility

The adapter should send this binary envelope as a dedicated Meshtastic
application portnum. A normal Meshtastic node does not need to parse Mava.
Relay nodes intended to carry Mava traffic must not use CORE_PORTNUMS_ONLY,
because that setting restricts rebroadcasting to standard Meshtastic portnums.

The envelope is not a cryptographic boundary. Mava's existing E2E encryption
remains authoritative. Meshtastic packet IDs are never used as Mava transfer IDs.

## Integration boundary

This commit adds the reusable transfer/envelope layer and strict parsing. It
does not embed a full Meshtastic protobuf implementation into Mava firmware.
The next adapter step is wiring this envelope to a Meshtastic radio interface
and registering a Mava application portnum.
