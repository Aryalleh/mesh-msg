package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"github.com/Aryalleh/mesh-msg/core/node"
)

func loraCmd(args []string) error {
	if len(args) > 0 && args[0] == "plans" {
		return loraPlans()
	}
	if len(args) > 0 && args[0] == "set" {
		fs := flag.NewFlagSet("lora set", flag.ContinueOnError)
		name := fs.String("node", "lora", "attached node name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		patch, err := parsePatch(fs.Args())
		if err != nil {
			return err
		}
		return call("POST", "/lora/"+url.PathEscape(*name)+"/config", patch, nil)
	}
	var nodes []node.LoRaState
	if err := call("GET", "/lora", nil, &nodes); err != nil {
		return err
	}
	if len(nodes) == 0 {
		fmt.Fprintln(out, "no LoRa node attached (set [lora] enabled = true and port in the meshd config)")
		return nil
	}
	for _, l := range nodes {
		printLoRa(l)
	}
	return nil
}

func loraPlans() error {
	var plans struct {
		Regions []hostlink.Region `json:"regions"`
		Presets []hostlink.Preset `json:"presets"`
	}
	if err := call("GET", "/lora/plans", nil, &plans); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REGION\tDEFAULT MHz\tRANGE MHz\tMAX dBm\tDUTY\tNOTE")
	for _, r := range plans.Regions {
		fmt.Fprintf(tw, "%s\t%.3f\t%.4f-%.4f\t%d\t%g %%\t%s\n", r.Name, float64(r.FreqHz)/1e6,
			float64(r.MinHz)/1e6, float64(r.MaxHz)/1e6, r.MaxDBm, float64(r.DutyPPM)/1e4, r.Note)
	}
	fmt.Fprintln(tw, "\nPRESET\tSF\tBW kHz\tCR")
	for _, p := range plans.Presets {
		fmt.Fprintf(tw, "%s\t%d\t%d\t4/%d\n", p.Name, p.SF, p.BWHz/1000, p.CR+4)
	}
	tw.Flush()
	fmt.Fprintln(out, "\nThese are starting points, not legal advice: check your local rules before transmitting.")
	return nil
}

func printLoRa(l node.LoRaState) {
	state := "connected"
	if !l.Connected {
		state = "disconnected"
		if l.Error != "" {
			state += " (" + l.Error + ")"
		}
	}
	fmt.Fprintf(out, "%s: %s\n", l.Name, state)
	if l.Node != "" {
		fmt.Fprintf(out, "  node      %s\n", l.Node)
	}
	if c := l.Config; c != nil {
		roles := []string{"client", "repeater", "gateway"}
		role, region, preset := "?", "?", "?"
		if int(c.Role) < len(roles) {
			role = roles[c.Role]
		}
		freq := c.FreqHz
		if int(c.Region) < len(hostlink.Regions) {
			region = hostlink.Regions[c.Region].Name
			if freq == 0 {
				freq = hostlink.Regions[c.Region].FreqHz
			}
		}
		if int(c.Preset) < len(hostlink.Presets) {
			preset = hostlink.Presets[c.Preset].Name
		}
		fmt.Fprintf(out, "  config    %s, %s %.3f MHz, %s, %d dBm, name %q, announce every %d min\n",
			role, region, float64(freq)/1e6, preset, c.TxDBm, c.Name, c.AnnounceMin)
	}
	if s := l.Status; s != nil {
		fmt.Fprintf(out, "  radio     %d neighbours, rx %d, tx %d frames, air %d/%d ms this hour, %d queued, %d stored\n",
			s.Neighbours, s.RxFrames, s.TxFrames, s.AirUsedMs, s.AirBudgetMs, s.Queued, s.Stored)
		if s.BatteryMv > 0 {
			fmt.Fprintf(out, "  battery   %.2f V\n", float64(s.BatteryMv)/1000)
		}
		fmt.Fprintf(out, "  uptime    %s, firmware v%d\n", time.Duration(s.UptimeS)*time.Second, s.FirmwareVersion)
	}
	fmt.Fprintf(out, "  host link sent %d, received %d, waiting %d, bad frames %d\n", l.Sent, l.Received, l.Queued, l.BadFrames)
}

// parsePatch turns key=value arguments into a node config patch.
func parsePatch(kv []string) (node.LoRaPatch, error) {
	var p node.LoRaPatch
	if len(kv) == 0 {
		return p, errors.New("usage: mesh lora set key=value… (role, region, preset, freq, power, name, announce)")
	}
	for _, a := range kv {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return p, fmt.Errorf("%q: want key=value", a)
		}
		switch k {
		case "role":
			v = strings.ToLower(v)
			p.Role = &v
		case "region":
			v = strings.ToUpper(v)
			p.Region = &v
		case "preset":
			v = strings.ToUpper(v)
			p.Preset = &v
		case "freq":
			mhz, err := strconv.ParseFloat(v, 64)
			if err != nil || mhz < 100 || mhz > 1100 {
				return p, fmt.Errorf("freq %q: want MHz, e.g. 869.525", v)
			}
			hz := uint32(mhz*1e6 + 0.5)
			p.FreqHz = &hz
		case "power":
			d, err := strconv.Atoi(v)
			if err != nil || d < -9 || d > 30 {
				return p, fmt.Errorf("power %q: want dBm", v)
			}
			dbm := int8(d)
			p.TxDBm = &dbm
		case "name":
			p.Name = &v
		case "announce":
			m, err := strconv.Atoi(v)
			if err != nil || m < 0 || m > 65535 {
				return p, fmt.Errorf("announce %q: want minutes (0 = off)", v)
			}
			mins := uint16(m)
			p.AnnounceMin = &mins
		default:
			return p, fmt.Errorf("unknown setting %q", k)
		}
	}
	return p, nil
}
