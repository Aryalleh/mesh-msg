package api

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MediaPolicy is the important-media rules of a relay (docs/server.md §6a),
// set in relay.json and changed at once from the admin panel.
type MediaPolicy struct {
	PerDay           int       `json:"media_important_per_day"`   // files per account per UTC day
	Hours            int       `json:"media_important_hours"`     // keep time
	NewAccountDays   int       `json:"media_new_account_days"`    // accounts younger than this…
	NewAccountPerDay int       `json:"media_new_account_per_day"` // …get this allowance
	PowBaseBits      int       `json:"media_pow_base_bits"`
	PowMaxBits       int       `json:"media_pow_max_bits"`
	AgeTiers         []AgeTier `json:"media_pow_age_tiers"` // extra bits for young accounts
}

// AgeTier: accounts younger than Days need Bits more work.
type AgeTier struct {
	Days int `json:"days"`
	Bits int `json:"bits"`
}

// DefaultMediaPolicy is the documented default.
func DefaultMediaPolicy() MediaPolicy {
	return MediaPolicy{
		PerDay: 5, Hours: 168, NewAccountDays: 3, NewAccountPerDay: 1,
		PowBaseBits: 20, PowMaxBits: 28,
		AgeTiers: []AgeTier{{3, 3}, {30, 2}, {180, 1}},
	}
}

// Check reports a setting out of range.
func (p MediaPolicy) Check() error {
	in := func(name string, v, lo, hi int) error {
		if v < lo || v > hi {
			return fmt.Errorf("%s must be %d…%d", name, lo, hi)
		}
		return nil
	}
	for _, err := range []error{
		in("media_important_per_day", p.PerDay, 1, 1000),
		in("media_important_hours", p.Hours, 1, 24*30),
		in("media_new_account_days", p.NewAccountDays, 0, 365),
		in("media_new_account_per_day", p.NewAccountPerDay, 0, p.PerDay),
		in("media_pow_base_bits", p.PowBaseBits, 0, 30),
		in("media_pow_max_bits", p.PowMaxBits, p.PowBaseBits, 32),
	} {
		if err != nil {
			return err
		}
	}
	if len(p.AgeTiers) > 8 {
		return fmt.Errorf("at most 8 age tiers")
	}
	for _, t := range p.AgeTiers {
		if t.Days < 1 || t.Days > 3650 || t.Bits < 0 || t.Bits > 8 {
			return fmt.Errorf("age tier %d:%d: days 1…3650, bits 0…8", t.Days, t.Bits)
		}
	}
	return nil
}

// Allowance is the files per day for an account of this age.
func (p MediaPolicy) Allowance(age time.Duration) int {
	if age < time.Duration(p.NewAccountDays)*24*time.Hour {
		return p.NewAccountPerDay
	}
	return p.PerDay
}

// Bits is the proof-of-work an upload needs: base + age bits + one per file
// the account uploaded today (network-wide) + load bits, capped.
func (p MediaPolicy) Bits(age time.Duration, usedToday, loadBits int) int {
	b := p.PowBaseBits + usedToday + loadBits
	tiers := slices.Clone(p.AgeTiers)
	slices.SortFunc(tiers, func(a, b AgeTier) int { return a.Days - b.Days })
	for _, t := range tiers {
		if age < time.Duration(t.Days)*24*time.Hour {
			b += t.Bits
			break
		}
	}
	return min(b, p.PowMaxBits)
}

// FormatTiers writes tiers as "3:3, 30:2, 180:1" (days:bits).
func FormatTiers(ts []AgeTier) string {
	var parts []string
	for _, t := range ts {
		parts = append(parts, fmt.Sprintf("%d:%d", t.Days, t.Bits))
	}
	return strings.Join(parts, ", ")
}

// ParseTiers reads "3:3, 30:2, 180:1".
func ParseTiers(s string) ([]AgeTier, error) {
	ts := []AgeTier{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		d, b, ok := strings.Cut(part, ":")
		days, err1 := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(d), "d"))
		bits, err2 := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(b), "+"))
		if !ok || err1 != nil || err2 != nil {
			return nil, fmt.Errorf("age tiers: %q is not days:bits", part)
		}
		ts = append(ts, AgeTier{days, bits})
	}
	return ts, nil
}
