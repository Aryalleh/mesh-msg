package hostlink

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Command payload: op(1) | req(1) | args. Event payload: ev(1) | req(1) | data,
// where req echoes the command's request ID (0 for unsolicited events).
const (
	OpHello       byte = 0x01 // args: [unix_time u32 BE]; node replies Status, then streams its backlog
	OpGetStatus   byte = 0x02
	OpGetConfig   byte = 0x03
	OpSetConfig   byte = 0x04 // args: config TLVs; applied and persisted, radio retuned
	OpGetIdentity byte = 0x05
	OpResync      byte = 0x06 // host lost its state: resend the whole stored backlog
	OpReboot      byte = 0x07
	OpWipe        byte = 0x08 // args: "WIPE"; erases store, config and node identity

	EvStatus   byte = 0x81
	EvConfig   byte = 0x82 // data: config TLVs
	EvIdentity byte = 0x83 // data: node public identity (64 bytes)
	EvAck      byte = 0x84 // data: op(1) | status(1)
	EvLog      byte = 0x85 // data: UTF-8 text
)

// Ack status codes.
const (
	AckOK         byte = 0
	AckBadArgs    byte = 1
	AckUnknownOp  byte = 2
	AckNotAllowed byte = 3
)

// WipeConfirm must be the argument of OpWipe.
var WipeConfirm = []byte("WIPE")

// Command builds a command payload.
func Command(op, req byte, args []byte) []byte {
	return append([]byte{op, req}, args...)
}

// Message is a parsed command or event payload.
type Message struct {
	Op   byte
	Req  byte
	Data []byte
}

// ParseMessage splits a command or event payload.
func ParseMessage(b []byte) (Message, error) {
	if len(b) < 2 {
		return Message{}, errors.New("hostlink: short message")
	}
	return Message{Op: b[0], Req: b[1], Data: b[2:]}, nil
}

// Roles (docs/firmware.md §2).
const (
	RoleClient   byte = 0
	RoleRepeater byte = 1
	RoleGateway  byte = 2
)

// Radio presets (docs/firmware.md §4).
const (
	PresetFast     byte = 0
	PresetBalanced byte = 1
	PresetLong     byte = 2
)

// Preset describes a LoRa modulation preset.
type Preset struct {
	ID   byte   `json:"id"`
	Name string `json:"name"`
	SF   uint8  `json:"sf"`
	BWHz uint32 `json:"bw_hz"`
	CR   uint8  `json:"cr"` // 1..4 → 4/5..4/8
}

// Presets is indexed by preset ID.
var Presets = []Preset{
	{PresetFast, "FAST", 7, 250000, 1},
	{PresetBalanced, "BALANCED", 9, 125000, 1},
	{PresetLong, "LONG", 11, 125000, 4},
}

// Region is a frequency plan. These are defaults to start from, not legal
// advice: band edges, power and duty-cycle limits differ between countries
// and change; the operator must check local regulations before transmitting.
type Region struct {
	ID      byte   `json:"id"`
	Name    string `json:"name"`
	FreqHz  uint32 `json:"freq_hz"` // default channel
	MinHz   uint32 `json:"min_hz"`  // allowed range for the channel centre
	MaxHz   uint32 `json:"max_hz"`
	MaxDBm  int8   `json:"max_dbm"`  // transmit power cap (conducted, before antenna gain)
	DutyPPM uint32 `json:"duty_ppm"` // airtime budget per hour, 10000 = 1 %
	Note    string `json:"note"`
}

// Regions is indexed by region ID. Region 0 is the default build setting.
var Regions = []Region{
	{0, "EU_868", 869525000, 869462500, 869587500, 27, 100000,
		"ETSI EN 300 220 sub-band 869.40-869.65 MHz: 500 mW e.r.p., 10 % duty cycle"},
	{1, "EU_868_1PCT", 868100000, 868062500, 868537500, 14, 10000,
		"ETSI 868.0-868.6 MHz: 25 mW e.r.p., 1 % duty cycle"},
	{2, "EU_433", 433875000, 433112500, 434727500, 10, 100000,
		"ETSI 433.05-434.79 MHz: 10 mW e.r.p., 10 % duty cycle"},
	{3, "US_915", 906875000, 902062500, 927937500, 30, 1000000,
		"FCC part 15.247, 902-928 MHz; single-channel use: keep dwell time short"},
	{4, "AU_915", 916000000, 915062500, 927937500, 30, 1000000,
		"ACMA LIPD class licence, 915-928 MHz"},
	{5, "IN_865", 866000000, 865062500, 866937500, 30, 1000000,
		"India 865-867 MHz licence-exempt band"},
	{6, "AS_923", 923200000, 920062500, 924937500, 16, 100000,
		"AS923 countries vary (some require listen-before-talk); check locally"},
}

// RegionByName finds a region.
func RegionByName(name string) (Region, bool) {
	for _, r := range Regions {
		if r.Name == name {
			return r, true
		}
	}
	return Region{}, false
}

// Config TLV tags: tag(1) | len(1) | value.
const (
	TagRole        byte = 1 // u8
	TagRegion      byte = 2 // u8
	TagPreset      byte = 3 // u8
	TagFreqHz      byte = 4 // u32 BE (0 = region default)
	TagTxDBm       byte = 5 // i8
	TagName        byte = 6 // UTF-8, ≤ 32 bytes
	TagAnnounceMin byte = 7 // u16 BE, minutes between own announces (0 = off)
)

// MaxNameLen bounds TagName.
const MaxNameLen = 32

// NodeConfig is the node's persistent configuration.
type NodeConfig struct {
	Role        byte
	Region      byte
	Preset      byte
	FreqHz      uint32
	TxDBm       int8
	Name        string
	AnnounceMin uint16
}

// DefaultNodeConfig is what a freshly flashed node uses.
func DefaultNodeConfig() NodeConfig {
	return NodeConfig{Role: RoleClient, Region: 0, Preset: PresetBalanced, TxDBm: 14, AnnounceMin: 30}
}

// Validate checks ranges against the region plan.
func (c NodeConfig) Validate() error {
	if c.Role > RoleGateway {
		return fmt.Errorf("hostlink: bad role %d", c.Role)
	}
	if int(c.Region) >= len(Regions) {
		return fmt.Errorf("hostlink: bad region %d", c.Region)
	}
	if int(c.Preset) >= len(Presets) {
		return fmt.Errorf("hostlink: bad preset %d", c.Preset)
	}
	r := Regions[c.Region]
	if c.FreqHz != 0 && (c.FreqHz < r.MinHz || c.FreqHz > r.MaxHz) {
		return fmt.Errorf("hostlink: %d Hz outside %s (%d-%d)", c.FreqHz, r.Name, r.MinHz, r.MaxHz)
	}
	if c.TxDBm > r.MaxDBm || c.TxDBm < -9 {
		return fmt.Errorf("hostlink: %d dBm outside -9..%d for %s", c.TxDBm, r.MaxDBm, r.Name)
	}
	if len(c.Name) > MaxNameLen {
		return errors.New("hostlink: name too long")
	}
	return nil
}

// EncodeTLV encodes every field.
func (c NodeConfig) EncodeTLV() []byte {
	var b []byte
	b = append(b, TagRole, 1, c.Role, TagRegion, 1, c.Region, TagPreset, 1, c.Preset)
	b = append(b, TagFreqHz, 4)
	b = binary.BigEndian.AppendUint32(b, c.FreqHz)
	b = append(b, TagTxDBm, 1, byte(c.TxDBm))
	b = append(b, TagName, byte(len(c.Name)))
	b = append(b, c.Name...)
	b = append(b, TagAnnounceMin, 2)
	b = binary.BigEndian.AppendUint16(b, c.AnnounceMin)
	return b
}

// ApplyTLV updates c from TLVs (a partial set is fine; unknown tags are
// skipped) and validates the result. c is unchanged on error.
func (c *NodeConfig) ApplyTLV(b []byte) error {
	n := *c
	for len(b) > 0 {
		if len(b) < 2 || len(b) < 2+int(b[1]) {
			return errors.New("hostlink: truncated TLV")
		}
		tag, v := b[0], b[2:2+int(b[1])]
		b = b[2+int(b[1]):]
		want := map[byte]int{TagRole: 1, TagRegion: 1, TagPreset: 1, TagFreqHz: 4, TagTxDBm: 1, TagAnnounceMin: 2}
		if w, ok := want[tag]; ok && len(v) != w {
			return fmt.Errorf("hostlink: tag %d length %d", tag, len(v))
		}
		switch tag {
		case TagRole:
			n.Role = v[0]
		case TagRegion:
			n.Region = v[0]
		case TagPreset:
			n.Preset = v[0]
		case TagFreqHz:
			n.FreqHz = binary.BigEndian.Uint32(v)
		case TagTxDBm:
			n.TxDBm = int8(v[0])
		case TagName:
			n.Name = string(v)
		case TagAnnounceMin:
			n.AnnounceMin = binary.BigEndian.Uint16(v)
		}
	}
	if err := n.Validate(); err != nil {
		return err
	}
	*c = n
	return nil
}

// StatusSize is the length of an EvStatus data block.
const StatusSize = 36

// Status is the EvStatus data (big-endian, fixed layout).
type Status struct {
	FirmwareVersion uint8  `json:"firmware_version"`
	Role            byte   `json:"role"`
	Region          byte   `json:"region"`
	Preset          byte   `json:"preset"`
	FreqHz          uint32 `json:"freq_hz"`
	TxDBm           int8   `json:"tx_dbm"`
	ShortID         uint8  `json:"short_id"`
	UptimeS         uint32 `json:"uptime_s"`
	Stored          uint16 `json:"stored"`
	Queued          uint8  `json:"queued"`
	Neighbours      uint8  `json:"neighbours"`
	AirUsedMs       uint32 `json:"air_used_ms"`
	AirBudgetMs     uint32 `json:"air_budget_ms"`
	RxFrames        uint32 `json:"rx_frames"`
	TxFrames        uint32 `json:"tx_frames"`
	BatteryMv       uint16 `json:"battery_mv"` // 0 = unknown / mains
}

// Encode returns the 36-byte status block.
func (s Status) Encode() []byte {
	b := make([]byte, 0, StatusSize)
	b = append(b, s.FirmwareVersion, s.Role, s.Region, s.Preset)
	b = binary.BigEndian.AppendUint32(b, s.FreqHz)
	b = append(b, byte(s.TxDBm), s.ShortID)
	b = binary.BigEndian.AppendUint32(b, s.UptimeS)
	b = binary.BigEndian.AppendUint16(b, s.Stored)
	b = append(b, s.Queued, s.Neighbours)
	b = binary.BigEndian.AppendUint32(b, s.AirUsedMs)
	b = binary.BigEndian.AppendUint32(b, s.AirBudgetMs)
	b = binary.BigEndian.AppendUint32(b, s.RxFrames)
	b = binary.BigEndian.AppendUint32(b, s.TxFrames)
	b = binary.BigEndian.AppendUint16(b, s.BatteryMv)
	return b
}

// ParseStatus decodes a status block.
func ParseStatus(b []byte) (Status, error) {
	if len(b) != StatusSize {
		return Status{}, fmt.Errorf("hostlink: status is %d bytes, want %d", len(b), StatusSize)
	}
	u32 := binary.BigEndian.Uint32
	return Status{
		FirmwareVersion: b[0], Role: b[1], Region: b[2], Preset: b[3],
		FreqHz: u32(b[4:]), TxDBm: int8(b[8]), ShortID: b[9], UptimeS: u32(b[10:]),
		Stored: binary.BigEndian.Uint16(b[14:]), Queued: b[16], Neighbours: b[17],
		AirUsedMs: u32(b[18:]), AirBudgetMs: u32(b[22:]), RxFrames: u32(b[26:]), TxFrames: u32(b[30:]),
		BatteryMv: binary.BigEndian.Uint16(b[34:]),
	}, nil
}
