# Roadmap

| Milestone | Deliverable | Done when |
|---|---|---|
| **M0 Spec** | protocol.md frozen as v0, `spec/test-vectors/*.json`, Persian text compression benchmark | vectors pass in Kotlin, Go and C++ codecs |
| **M1 Simulator** | `sim/`: Python discrete-event sim of phones, LoRa nodes, relays with mobility & churn | delivery ratio / latency / airtime reported for City A ↔ City B scenario |
| **M2 LoRa link** | firmware: radio, codec, router, store, hop ACK | text packet crosses A → repeater → B on real boards |
| **M3 Phone ↔ node** | Android core-protocol + transport-lora + minimal chat UI | two phones chat through two LoRa nodes |
| **M4 Phone mesh** | transport-ble epidemic sync, MeshService | message hops across 3 phones with no node |
| **M5 Relay** | Go relay server, transport-internet, gateway role in firmware | offline A → online phone → relay → online phone → offline B |
| **M6 Hardening** | panic wipe, stealth, APK sharing, Tor path, security review | external review findings addressed |
| **M7 Field test** | 10 km two-city test with rooftop repeaters | measured delivery report |

Order rationale: spec and simulator first, because routing mistakes are cheap to find in
simulation and expensive to find on rooftops.
