package hostlink

import "time"

// Airtime is the SX126x time on air for an n-byte LoRa payload (8-symbol
// preamble, explicit header, CRC on, low data rate optimisation above 16 ms
// symbols) — the same integer formula as firmware mac::airtime_us.
func (p Preset) Airtime(n int) time.Duration {
	tsym := (int64(1) << p.SF) * 1_000_000_000 / int64(p.BWHz) // ns
	de := int64(0)
	if tsym > 16_000_000 {
		de = 1
	}
	preamble := tsym * 49 / 4
	num := 8*int64(n) - 4*int64(p.SF) + 28 + 16
	den := 4 * (int64(p.SF) - 2*de)
	nsym := int64(0)
	if num > 0 {
		nsym = (num + den - 1) / den * int64(p.CR+4)
	}
	return time.Duration((preamble + (8+nsym)*tsym) / 1000 * 1000)
}

// SendInterval is how long a host should wait after handing the node an
// n-byte packet so that it never feeds more than the region's duty-cycle
// budget allows (the node enforces the budget itself; pacing keeps its
// queue short instead of overflowing it).
func SendInterval(c NodeConfig, n int) time.Duration {
	if int(c.Preset) >= len(Presets) || int(c.Region) >= len(Regions) {
		c = DefaultNodeConfig()
	}
	air := Presets[c.Preset].Airtime(n + 1) // + link header
	d := time.Duration(int64(air) * 1_000_000 / int64(Regions[c.Region].DutyPPM))
	if d < 50*time.Millisecond {
		d = 50 * time.Millisecond
	}
	return d
}
