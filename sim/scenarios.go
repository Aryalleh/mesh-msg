package sim

import (
	"fmt"
	"time"
)

// Scenario builds a world and returns the traffic window and total duration.
type Scenario struct {
	Name  string
	Desc  string
	Build func(w *World) (sendFor, runFor time.Duration)
}

// Scenarios are the built-in experiments.
var Scenarios = []Scenario{
	{
		Name: "blackout",
		Desc: "Tehran under international blackout ↔ Berlin: BLE mesh, phones with domestic data as carrier gateways, IR regional relay, intermittent Starlink bridge to EU relay",
		Build: func(w *World) (time.Duration, time.Duration) {
			ir := w.AddRelay("ir-1", "ir")
			eu := w.AddRelay("eu-1", "global")
			var places []*Place
			for i := 0; i < 8; i++ {
				places = append(places, w.AddPlace(fmt.Sprintf("tehran-%d", i)))
			}
			var tehran, berlin []*Node
			for i := 0; i < 40; i++ {
				n := w.AddNode(fmt.Sprintf("teh%02d", i))
				n.EnableBLE()
				w.Wander(n, places, 40*time.Minute, 15*time.Minute)
				tehran = append(tehran, n)
			}
			// 4 phones get domestic mobile data on and off → carrier gateways.
			for _, n := range tehran[:4] {
				u := w.ConnectRelay(n, ir, true, 4096, time.Minute)
				w.Intermittent(2*time.Hour, 2*time.Hour, u.SetOnline)
			}
			for i := 0; i < 3; i++ {
				n := w.AddNode(fmt.Sprintf("ber%d", i))
				w.ConnectRelay(n, eu, false, 0, 30*time.Second).SetOnline(true)
				berlin = append(berlin, n)
			}
			// A PC with intermittent Starlink bridges the two relays.
			bridgeUp := false
			w.Intermittent(time.Hour, time.Hour, func(on bool) { bridgeUp = on })
			w.Bridge(ir, eu, time.Minute, func() bool { return bridgeUp })

			pick := func(ns []*Node) *Node { return ns[w.E.Rand.Intn(len(ns))] }
			sendFor := 18 * time.Hour
			w.E.Every(5*time.Minute, 5*time.Minute, func() {
				if w.E.Now().Sub(start(w)) > sendFor {
					return
				}
				w.Send(pick(tehran), pick(berlin), "tehran→berlin")
				w.Send(pick(berlin), pick(tehran), "berlin→tehran")
				a, b := pick(tehran), pick(tehran)
				if a != b {
					w.Send(a, b, "tehran→tehran")
				}
			})
			return sendFor, 24 * time.Hour
		},
	},
	{
		Name:  "two-cities-lora",
		Desc:  "Two cities 10 km apart with no internet at all: BLE meshes joined by LoRa client nodes and a rooftop repeater (SF9/125 kHz, 1% duty cycle)",
		Build: func(w *World) (time.Duration, time.Duration) { return twoCities(w, 0.01) },
	},
	{
		Name:  "two-cities-lora-10pct",
		Desc:  "Same as two-cities-lora on a sub-band allowing 10% duty cycle (e.g. EU 869.4–869.65 MHz)",
		Build: func(w *World) (time.Duration, time.Duration) { return twoCities(w, 0.10) },
	},
	{
		Name: "lora-joins-relays",
		Desc: "Two cities whose relays cannot reach each other; LoRa gateway nodes (relay ↔ air) at each relay join them through a rooftop repeater",
		Build: func(w *World) (time.Duration, time.Duration) {
			ra := w.AddRelay("relay-a", "a")
			rb := w.AddRelay("relay-b", "b")
			var usersA, usersB []*Node
			for i := 0; i < 10; i++ {
				a := w.AddNode(fmt.Sprintf("ua%02d", i))
				w.ConnectRelay(a, ra, false, 0, 30*time.Second).SetOnline(true)
				usersA = append(usersA, a)
				b := w.AddNode(fmt.Sprintf("ub%02d", i))
				w.ConnectRelay(b, rb, false, 0, 30*time.Second).SetOnline(true)
				usersB = append(usersB, b)
			}
			lp := LoRaParams{SF: 9, BWHz: 125e3, CR: 1, DutyCycle: 0.01, RangeM: 7000}
			ga := w.AddNode("lora-gw-a")
			w.AddRadio(ga, 0, 0, lp)
			w.ConnectRelay(ga, ra, true, 2048, time.Minute).SetOnline(true)
			gb := w.AddNode("lora-gw-b")
			w.AddRadio(gb, 12000, 0, lp)
			w.ConnectRelay(gb, rb, true, 2048, time.Minute).SetOnline(true)
			rep := w.AddNode("repeater")
			w.AddRadio(rep, 6000, 0, lp)

			pick := func(ns []*Node) *Node { return ns[w.E.Rand.Intn(len(ns))] }
			sendFor := 12 * time.Hour
			w.E.Every(10*time.Minute, 10*time.Minute, func() {
				if w.E.Now().Sub(start(w)) > sendFor {
					return
				}
				w.Send(pick(usersA), pick(usersB), "relayA→relayB")
				w.Send(pick(usersB), pick(usersA), "relayB→relayA")
			})
			return sendFor, 18 * time.Hour
		},
	},
}

func twoCities(w *World, duty float64) (time.Duration, time.Duration) {
	lp := LoRaParams{SF: 9, BWHz: 125e3, CR: 1, DutyCycle: duty, RangeM: 6000}
	var cityA, cityB []*Node
	for c, city := range []*[]*Node{&cityA, &cityB} {
		var places []*Place
		for i := 0; i < 4; i++ {
			places = append(places, w.AddPlace(fmt.Sprintf("city%c-%d", 'A'+c, i)))
		}
		for i := 0; i < 15; i++ {
			n := w.AddNode(fmt.Sprintf("%c%02d", 'a'+c, i))
			n.EnableBLE()
			w.Wander(n, places, 40*time.Minute, 15*time.Minute)
			*city = append(*city, n)
		}
		// A LoRa client node lives at place 0 (e.g. a shop window).
		node := w.AddNode(fmt.Sprintf("lora-%c", 'A'+c))
		node.EnableBLE()
		node.MoveTo(places[0])
		w.AddRadio(node, float64(c)*10000, 0, lp)
	}
	rep := w.AddNode("repeater")
	w.AddRadio(rep, 5000, 0, lp)

	pick := func(ns []*Node) *Node { return ns[w.E.Rand.Intn(len(ns))] }
	sendFor := 18 * time.Hour
	w.E.Every(10*time.Minute, 10*time.Minute, func() {
		if w.E.Now().Sub(start(w)) > sendFor {
			return
		}
		w.Send(pick(cityA), pick(cityB), "A→B")
		w.Send(pick(cityB), pick(cityA), "B→A")
	})
	return sendFor, 24 * time.Hour
}

var startTime = NewEngine(0).Now()

func start(*World) time.Time { return startTime }

// Run executes a scenario and returns its report.
func Run(s Scenario, seed int64) (*World, string) {
	w := NewWorld(seed)
	_, dur := s.Build(w)
	w.E.RunFor(dur)
	return w, w.Report(s.Name+" — "+s.Desc, dur)
}
