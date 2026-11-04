# meshsim — network simulator

Runs the **real** `core/router` and `server/relay` code in virtual time:

- phones moving between places (Bluetooth mesh when co-located)
- LoRa radios: Semtech time-on-air, half-duplex, sliding 1-hour duty-cycle
  budget, priority queue, announce airtime cap (10%), neighbour-aware
  implicit-ACK retransmission
- relays, carrier gateways, intermittent bridges

```bash
go run ./sim/cmd/meshsim -list
go run ./sim/cmd/meshsim -s blackout -seed 7
go run ./sim/cmd/meshsim            # all scenarios (~3 min)
```

## Results (seed 1)

| Scenario | Direction | Delivered | Median | p90 |
|---|---|---|---|---|
| **blackout** — 40 phones in Tehran, 4 with on/off domestic data (carrier gateways), IR relay, Starlink bridge online ~50% of the time, 3 users in Berlin | Tehran → Berlin | 100% | 37 min | 1 h 47 m |
| | Berlin → Tehran | 100% | 46 min | 1 h 42 m |
| | Tehran → Tehran | 100% | 32 min | 1 h 17 m |
| **two-cities-lora** — 2 × 15 phones, one LoRa client node per city, rooftop repeater, no internet, SF9, 1% duty | A → B | 99% | 1 h 05 m | 2 h 01 m |
| | B → A | 100% | 1 h 20 m | 2 h 27 m |
| **two-cities-lora-10pct** — same, 10% duty sub-band | A → B / B → A | 97% / 100% | ~1 h 05 m | ~2 h |
| **lora-joins-relays** — two relays that can't reach each other, a LoRa gateway node at each, rooftop repeater | A → B | 100% | 10 min | 11 min |
| | B → A | 99% | 11 min | 12 min |

## What the simulations taught us (and changed in the design)

1. **Single LoRa transmissions get lost** to half-duplex collisions (a node
   transmitting can't hear). Without retries, *lora-joins-relays* delivered
   0%. Implicit-ACK retransmission fixed it.
2. **Naive retries waste airtime**: a leaf node re-broadcasting a frame its
   only neighbour sent never hears an ACK. Fix: a 1-byte **LoRa link header**
   with the sender's short ID, so nodes know their neighbours and retry only
   when a neighbour that doesn't have the frame should rebroadcast it.
   (LoRa packet MTU is now 254; one frame carries 175 bytes of text.)
3. **Announces would saturate a 1% duty cycle**: ~30 phones announcing every
   30 min exceed the budget. Announces are capped to 10% of LoRa airtime and
   dropped first.
4. **Carrier gateways must wait** before pulling a relay's undelivered
   traffic (default 10 min), so online recipients fetch directly and LoRa
   isn't spent on packets that never needed it. Carriers also stop at their
   quota instead of skipping packets.
5. In the two-cities case latency is dominated by **people walking** to where
   the LoRa node is, not by radio capacity: more client nodes (or phones
   carrying their own) cut latency more than a bigger duty cycle.

## Limits of the model

Collisions between two transmitters at a third receiver are not modelled
(only half-duplex), BLE is all-to-all within a place, and links are lossless
otherwise. Treat absolute numbers as optimistic; use the scenarios to compare
designs.
