# Threat Model (v0)

## Users and setting

People in a region under internet shutdown and surveillance (e.g. Iran, 2019 / 2022 / 2025
shutdowns). Some have intermittent VPN access; most do not. Adversary is a state with
control of national ISPs, mobile networks, app stores in-country, and police powers.

## Adversary capabilities → mitigations

| Capability | Risk | Mitigation |
|---|---|---|
| Read traffic on relays / LoRa / BLE | message content | Noise E2E encryption; nodes & servers hold ciphertext only |
| Observe who talks to whom | social graph | sender hidden inside ciphertext; only dest_hash visible; Tor path to relays; v1: rotating per-epoch addresses |
| Join a private group / read public chats | group content | a private group is a member list: each message is end-to-end encrypted separately to every member (Noise X), so only current members can read new messages and the owner can remove people; a removed member keeps what they already received; the owner decides who is in (members trust the owner's list); relays see one ordinary message per member, so a sender's fan-out (how many members) is visible in traffic volume; public chats (#global, regional) are readable by anyone by design and the UIs say so; every public chat message is signed, so no one can post in another member's name |
| Block relay servers | Tier 2 loss | multiple federated relays, CDN fronting, Tor onion, relay lists spread over the mesh; Tier 0/1 don't depend on servers |
| Radio direction finding of LoRa | locate & seize repeaters | low duty cycle, short frames, repeaters hold no plaintext/contacts, remote wipe; operators accept the physical risk knowingly |
| Detect app via BLE advertising | identify users in a crowd | the advertisement is a fixed service UUID plus a random per-start session ID (nothing tied to the identity), but it does reveal that the app is running; no hiding option (stealth mode was removed: it kept phones from finding each other) — residual risk, turn Bluetooth off to hide |
| Device seizure / forced unlock | contacts, history | SQLCipher DB, app PIN, duress PIN → panic wipe, disappearing messages |
| Malicious app builds | backdoored APK | reproducible builds, signing-key fingerprint shown in app and verifiable offline |
| Spam / flooding the mesh | DoS of scarce LoRa airtime | per-source rate limits, airtime budgets, PoW on relays, priority queue, hop limits |
| Fake announces / route poisoning | misdirect traffic | announces signed; routes are hints only — store-and-forward copies continue via other paths |
| Replay | duplicate messages | packet_id dedup cache + timestamp window |
| Seize / compel an **in-country regional relay** | logs of connecting IPs (domestic IPs map to registered SIMs), shutdown | relay is untrusted & disposable; no logs, in-memory gateway state, short retention; several relays on different providers; addresses rotate via signed relay lists over the mesh |
| DPI on domestic network flags connections to a known relay | identify gateways | relay traffic is plain HTTPS on an innocuous domain; rotate addresses; gateway role is opt-in and its risk is explained in the UI |
| Detect bridge operators (VPN / Starlink / unfiltered SIM traffic patterns) | arrest of bridge operators | bridges are opt-in with clear risk warning; data budgets keep traffic small; batching + jitter; operators decide their own exposure |
| A call partner learns your network address | location of the caller / callee | calls go over a direct path only, so each side sees the other's IP addresses; calls only from contacts by default, the first call screen warns; relays never see audio |
| Malicious relay or bridge | drop / delay / inject packets | E2E auth stops injection; store-and-forward over many paths survives dropping; receipts reveal non-delivery |

## Explicit non-goals (v0)

- Hiding that a user runs the app from someone holding their unlocked phone.
- Resisting a global passive adversary correlating all radio + internet timing.
- Anonymity of LoRa repeater operators against physical search.

## Rules for contributors

- No custom cryptography; use Noise and maintained libraries.
- No telemetry, analytics, crash reporters that phone home.
- No plaintext or contact data on LoRa nodes or servers, ever.
