package node

// Trip location sharing (core/trip, docs/trip.md): a member's phone sends
// its position to the trip's leaders every few minutes once the member has
// agreed; leaders see every member's last position.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/relayclient"
	"github.com/Aryalleh/mesh-msg/core/trip"
)

const (
	tripsKey = "trips"
	// tripCarryTTL: how long a member's device keeps reports it carries for
	// others (they are superseded every few minutes).
	tripCarryTTL = 6 * time.Hour
	// tripFast is the report interval in SOS and after a leader's request.
	tripFast = 60 * time.Second
	// tripLoRaEvery: ordinary reports go on LoRa at most this often (SOS and
	// answers always do).
	tripLoRaEvery = 15 * time.Minute
	tripTrack     = 200 // positions kept per member
)

// How often relays are asked for their trip card: after a card, and after
// "no trip" or an error.
var tripPoll, tripRetry = 10 * time.Minute, 2 * time.Minute

// tripState is what this device keeps about one trip.
type tripState struct {
	Signed    trip.Signed `json:"signed"`
	Seed      string      `json:"seed,omitempty"` // hex; only leaders hold it
	Consent   string      `json:"consent,omitempty"`
	SOS       bool        `json:"sos,omitempty"`
	FastUntil int64       `json:"fast_until,omitempty"`
	Last      int64       `json:"last,omitempty"`      // our last report (unix)
	LastLoRa  int64       `json:"last_lora,omitempty"` // our last report that may use LoRa
	LastID    string      `json:"last_id,omitempty"`   // its packet id (dropped when the next goes out)
	Stopped   int64       `json:"stopped,omitempty"`

	card trip.Card
	pub  identity.Public
	id   *identity.Identity // leaders
}

// TripMember is one member's latest position, as a leader sees it.
type TripMember struct {
	Hash     string       `json:"hash"`
	Key      string       `json:"key"` // hex X||Ed (to ask them for their position)
	Name     string       `json:"name"`
	Lat      float64      `json:"lat"`
	Lon      float64      `json:"lon"`
	Accuracy int          `json:"accuracy"`
	Battery  int          `json:"battery"`
	Fix      int64        `json:"fix"`  // unix time of the position
	Seen     int64        `json:"seen"` // unix time we got the newest report
	NoFix    bool         `json:"no_fix,omitempty"`
	SOS      bool         `json:"sos,omitempty"`
	Stopped  bool         `json:"stopped,omitempty"`
	Answer   bool         `json:"answer,omitempty"`
	Track    [][3]float64 `json:"track,omitempty"` // [lat, lon, unix], oldest first
}

// Trip is a trip as UIs see it.
type Trip struct {
	Hash      string        `json:"hash"`
	Name      string        `json:"name"`
	Leader    bool          `json:"leader"` // this account leads it (holds its key)
	Leaders   []trip.Leader `json:"leaders"`
	Every     int           `json:"every"`
	Ends      int64         `json:"ends"`
	Ended     bool          `json:"ended"`
	Relay     string        `json:"relay,omitempty"`
	Consent   string        `json:"consent"` // "" not asked yet, "yes", "no"
	Sharing   bool          `json:"sharing"`
	SOS       bool          `json:"sos"`
	FastUntil int64         `json:"fast_until,omitempty"`
	Interval  int           `json:"interval"` // seconds between reports now
	NextIn    int           `json:"next_in"`  // seconds until the next report is due (0 = now)
	Last      int64         `json:"last,omitempty"`
	Link      string        `json:"link"`
	MapKey    string        `json:"map_key,omitempty"` // leaders: for the relay's web map
	Members   int           `json:"members,omitempty"` // leaders
}

type tripBook struct {
	mu       sync.Mutex
	m        map[string]*tripState // by trip hash
	pos      map[string]map[string]*TripMember
	lastPoll map[string]time.Time // relay link → next card fetch due
}

func (n *Node) loadTrips() {
	n.tr = &tripBook{m: map[string]*tripState{}, pos: map[string]map[string]*TripMember{}, lastPoll: map[string]time.Time{}}
	v, ok := n.db.get(tripsKey)
	if !ok {
		return
	}
	var list []*tripState
	if err := json.Unmarshal([]byte(v), &list); err != nil {
		n.cfg.Logf("node: trips: %v", err)
		return
	}
	for _, t := range list {
		if err := t.load(); err != nil {
			n.cfg.Logf("node: trip: %v", err)
			continue
		}
		h := t.pub.Hash().String()
		n.tr.m[h] = t
		n.tripHost(t)
		if p, ok := n.db.get("trippos:" + h); ok {
			var pm map[string]*TripMember
			if json.Unmarshal([]byte(p), &pm) == nil {
				n.tr.pos[h] = pm
			}
		}
	}
}

func (t *tripState) load() error {
	c, err := t.Signed.Open()
	if err != nil {
		return err
	}
	t.card = c
	t.pub, _ = c.Public()
	if t.Seed != "" {
		b, err := hex.DecodeString(t.Seed)
		if err != nil {
			return err
		}
		id, err := identity.FromSeed(b)
		if err != nil || id.Hash() != t.pub.Hash() {
			return errors.New("trip: stored key does not match the card")
		}
		t.id = id
	}
	return nil
}

// tripHost registers a trip with the router: leaders open its reports;
// members keep the reports they carry only a few hours.
func (n *Node) tripHost(t *tripState) {
	if t.id != nil {
		n.router.AddInbox(t.id)
		n.router.AddKnown(t.pub)
	} else {
		n.router.SetDestTTL(t.pub.Hash(), tripCarryTTL)
	}
}

// saveTrips writes the trip list; call with n.tr.mu held.
func (n *Node) saveTrips() {
	list := make([]*tripState, 0, len(n.tr.m))
	for _, h := range sortedKeys(n.tr.m) {
		list = append(list, n.tr.m[h])
	}
	b, _ := json.Marshal(list)
	if err := n.db.set(tripsKey, string(b)); err != nil {
		n.cfg.Logf("node: save trips: %v", err)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (t *tripState) sharing(now time.Time) bool {
	return t.Consent == "yes" && t.Stopped == 0 && !t.card.Ended(now)
}

func (t *tripState) interval(now time.Time) time.Duration {
	if t.SOS || now.Unix() < t.FastUntil {
		return tripFast
	}
	return time.Duration(t.card.Every) * time.Second
}

func (n *Node) tripView(h string, t *tripState, now time.Time) Trip {
	v := Trip{
		Hash: h, Name: t.card.Name, Leader: t.id != nil, Leaders: t.card.Leaders, Every: t.card.Every,
		Ends: t.card.Ends, Ended: t.card.Ended(now), Relay: t.card.Relay, Consent: t.Consent,
		Sharing: t.sharing(now), SOS: t.SOS, FastUntil: t.FastUntil, Last: t.Last, Link: t.Signed.URI(),
	}
	iv := t.interval(now)
	v.Interval = int(iv / time.Second)
	if v.Sharing {
		v.NextIn = max(0, int(time.Unix(t.Last, 0).Add(iv).Sub(now)/time.Second))
	}
	if t.id != nil {
		v.MapKey = trip.MapKey(t.id)
		v.Members = len(n.tr.pos[h])
	}
	return v
}

// Trips lists the trips this device knows, newest end first.
func (n *Node) Trips() []Trip {
	now := time.Now()
	n.tr.mu.Lock()
	defer n.tr.mu.Unlock()
	out := make([]Trip, 0, len(n.tr.m))
	for _, h := range sortedKeys(n.tr.m) {
		out = append(out, n.tripView(h, n.tr.m[h], now))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ends > out[j].Ends })
	return out
}

func (n *Node) findTrip(ref string) (string, *tripState, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if t, ok := n.tr.m[ref]; ok {
		return ref, t, nil
	}
	for h, t := range n.tr.m {
		if len(ref) >= 6 && strings.HasPrefix(h, ref) {
			return h, t, nil
		}
	}
	return "", nil, fmt.Errorf("no trip %q", ref)
}

// OfferTrip takes a signed trip card (from a relay or a meshtrip: link). A
// new trip waits for the member's answer (TripConsent); a newer card of a
// known trip replaces the old one and keeps the answer.
func (n *Node) OfferTrip(s trip.Signed) (Trip, error) {
	t := &tripState{Signed: s}
	if err := t.load(); err != nil {
		return Trip{}, err
	}
	now := time.Now()
	if t.card.Ended(now) {
		return Trip{}, errors.New("this trip has ended")
	}
	h := t.pub.Hash().String()
	n.tr.mu.Lock()
	old, known := n.tr.m[h]
	changed := !known
	if known {
		if t.card.Made > old.card.Made {
			old.Signed, old.card = s, t.card
			changed = true
		}
		t = old
	} else {
		n.tr.m[h] = t
		n.tripHost(t)
	}
	if changed {
		n.saveTrips()
	}
	v := n.tripView(h, t, now)
	n.tr.mu.Unlock()
	if changed {
		n.emit(Event{Type: "trip", Info: h})
	}
	return v, nil
}

// JoinTrip takes a meshtrip: link (or the card's JSON).
func (n *Node) JoinTrip(link string) (Trip, error) {
	s, err := trip.ParseSigned(link)
	if err != nil {
		return Trip{}, err
	}
	return n.OfferTrip(s)
}

// TripConsent records the member's answer. Yes starts sharing (the platform
// sends reports when TripReport says one is due); no sends nothing.
func (n *Node) TripConsent(ref string, yes bool) error {
	n.tr.mu.Lock()
	h, t, err := n.findTrip(ref)
	if err != nil {
		n.tr.mu.Unlock()
		return err
	}
	if yes {
		if t.card.Ended(time.Now()) {
			n.tr.mu.Unlock()
			return errors.New("this trip has ended")
		}
		t.Consent, t.Stopped, t.Last = "yes", 0, 0
	} else {
		t.Consent = "no"
	}
	n.saveTrips()
	n.tr.mu.Unlock()
	n.emit(Event{Type: "trip", Info: h})
	return nil
}

// TripStop stops sharing and tells the leaders (a last report flagged
// "stopped", without a position).
func (n *Node) TripStop(ref string) error {
	n.tr.mu.Lock()
	h, t, err := n.findTrip(ref)
	if err != nil {
		n.tr.mu.Unlock()
		return err
	}
	was := t.sharing(time.Now())
	t.Consent, t.SOS, t.FastUntil, t.Stopped = "no", false, 0, time.Now().Unix()
	n.saveTrips()
	n.tr.mu.Unlock()
	if was {
		n.sendTripReport(h, trip.Report{Flags: trip.FlagStopped | trip.FlagNoFix, Battery: -1}, true)
	}
	n.emit(Event{Type: "trip", Info: h})
	return nil
}

// TripSOS turns "I'm lost" on or off: reports every minute, always on LoRa,
// and the leaders are alerted.
func (n *Node) TripSOS(ref string, on bool) error {
	n.tr.mu.Lock()
	h, t, err := n.findTrip(ref)
	if err != nil {
		n.tr.mu.Unlock()
		return err
	}
	if on && !t.sharing(time.Now()) {
		n.tr.mu.Unlock()
		return errors.New("share your location first")
	}
	t.SOS = on
	if on {
		t.Last = 0 // due now
	}
	n.saveTrips()
	n.tr.mu.Unlock()
	n.emit(Event{Type: "trip", Info: h})
	return nil
}

// TripReport sends a location report for trip ref if one is due (or force).
// The platform calls it with a fresh position (hasFix false: none yet). It
// returns whether a report went out.
func (n *Node) TripReport(ref string, lat, lon float64, accuracy, fixAge, battery int, hasFix, force bool) (bool, error) {
	now := time.Now()
	n.tr.mu.Lock()
	h, t, err := n.findTrip(ref)
	if err != nil {
		n.tr.mu.Unlock()
		return false, err
	}
	if !t.sharing(now) {
		n.tr.mu.Unlock()
		return false, nil
	}
	due := now.Sub(time.Unix(t.Last, 0)) >= t.interval(now)-5*time.Second
	if !due && !force {
		n.tr.mu.Unlock()
		return false, nil
	}
	r := trip.Report{Lat: lat, Lon: lon, Accuracy: accuracy, FixAge: fixAge, Battery: battery, Name: n.cfg.Name}
	if t.SOS {
		r.Flags |= trip.FlagSOS
	}
	if now.Unix() < t.FastUntil {
		r.Flags |= trip.FlagAnswer
	}
	if !hasFix {
		r.Flags |= trip.FlagNoFix
	}
	n.tr.mu.Unlock()
	return n.sendTripReport(h, r, false), nil
}

func (n *Node) sendTripReport(h string, r trip.Report, final bool) bool {
	now := time.Now()
	n.tr.mu.Lock()
	t, ok := n.tr.m[h]
	if !ok {
		n.tr.mu.Unlock()
		return false
	}
	lora := final || r.Flags&(trip.FlagSOS|trip.FlagAnswer) != 0 || now.Sub(time.Unix(t.LastLoRa, 0)) >= tripLoRaEvery
	prev := t.LastID
	pub := t.pub
	n.tr.mu.Unlock()
	id, err := n.router.SendContentWith(n.id, pub, e2e.ContentTrip, r.Encode(), message.Options{NoLoRa: !lora, Priority: r.Flags&trip.FlagSOS != 0})
	if err != nil {
		n.cfg.Logf("node: trip report: %v", err)
		return false
	}
	if p, err := packet.ParseID(prev); err == nil {
		n.router.Forget(p) // the newer report supersedes it
	}
	n.tr.mu.Lock()
	if t, ok := n.tr.m[h]; ok {
		t.Last, t.LastID = now.Unix(), id.String()
		if lora {
			t.LastLoRa = now.Unix()
		}
		n.saveTrips()
	}
	n.tr.mu.Unlock()
	return true
}

// ---- leaders ----

// relayFor finds a relay client by link name (with or without "relay:"),
// configured name or URL; links may not be running yet right after start.
func (n *Node) relayFor(ref string) (*relayclient.Client, error) {
	ref = strings.TrimSpace(ref)
	same := func(u string) bool { return strings.TrimRight(u, "/") == strings.TrimRight(ref, "/") }
	for _, l := range n.allRelayLinks() {
		if l.name == ref || l.name == "relay:"+ref || same(l.c.URL()) {
			return l.c, nil
		}
	}
	for _, rc := range n.cfg.Relays {
		if rc.Name == ref || "relay:"+rc.Name == ref || same(rc.URL) {
			return relayclient.New(rc.URL, relayclient.Options{Via: rc.Via, Origin: "node-" + n.id.Hash().String()[:12]})
		}
	}
	return nil, fmt.Errorf("no relay %q", ref)
}

func explainTripRelayError(err error) error {
	var se *relayclient.StatusError
	if errors.As(err, &se) {
		switch se.Code {
		case 403:
			return fmt.Errorf("the relay refused: %s", se.Msg)
		case 404, 405:
			return errors.New("this relay does not run trips (update it)")
		}
		return fmt.Errorf("the relay refused: %s", se.Msg)
	}
	return err
}

// CreateTrip starts a trip on relay (where this account must be an admin):
// a new trip key, this account as its first leader. Nothing is saved unless
// the relay takes the card.
func (n *Node) CreateTrip(ctx context.Context, relay, name string, every time.Duration, ends time.Time) (Trip, error) {
	rc, err := n.relayFor(relay)
	if err != nil {
		return Trip{}, err
	}
	id, err := identity.Generate()
	if err != nil {
		return Trip{}, err
	}
	now := time.Now()
	c := trip.Card{
		V: 1, Name: strings.TrimSpace(name), Key: hex.EncodeToString(id.Bytes()),
		Leaders: []trip.Leader{{ID: n.id.Hash().String(), Name: n.cfg.Name}},
		Every:   int(every / time.Second), Ends: ends.Unix(), Relay: rc.URL(), Made: now.Unix(),
	}
	s, err := trip.Sign(id, c)
	if err != nil {
		return Trip{}, err
	}
	if err := rc.PutTrip(ctx, n.id, s); err != nil {
		return Trip{}, explainTripRelayError(err)
	}
	t := &tripState{Signed: s, Seed: hex.EncodeToString(id.Seed())}
	if err := t.load(); err != nil {
		return Trip{}, err
	}
	h := id.Hash().String()
	n.tr.mu.Lock()
	n.tr.m[h] = t
	n.tripHost(t)
	n.saveTrips()
	v := n.tripView(h, t, now)
	n.tr.mu.Unlock()
	n.router.Announce() // so the relay lets us fetch the trip's mailbox
	n.emit(Event{Type: "trip", Info: h})
	return v, nil
}

// AddTripLeader makes a contact a leader too: the card lists them (the relay
// checks they are one of its admins) and they get the trip's key in an
// end-to-end encrypted message.
func (n *Node) AddTripLeader(ctx context.Context, ref, contact string) (Trip, error) {
	c, err := n.findContact(contact)
	if err != nil {
		return Trip{}, err
	}
	n.tr.mu.Lock()
	h, t, err := n.findTrip(ref)
	if err == nil && t.id == nil {
		err = errors.New("only a leader can add leaders")
	}
	if err != nil {
		n.tr.mu.Unlock()
		return Trip{}, err
	}
	card := t.card
	if !card.IsLeader(c.Hash) {
		card.Leaders = append(append([]trip.Leader(nil), card.Leaders...), trip.Leader{ID: c.Hash, Name: c.Name})
	}
	card.Made = max(time.Now().Unix(), card.Made+1)
	id := t.id
	n.tr.mu.Unlock()
	s, err := trip.Sign(id, card)
	if err != nil {
		return Trip{}, err
	}
	if err := n.publishTrip(ctx, card.Relay, s); err != nil {
		return Trip{}, err
	}
	n.tr.mu.Lock()
	t.Signed, t.card = s, card
	n.saveTrips()
	v := n.tripView(h, t, time.Now())
	n.tr.mu.Unlock()
	if _, err := n.router.SendContent(n.id, c.pub, e2e.ContentTrip, trip.Lead(id, s), n.heardFrom(c.Hash), true); err != nil {
		return v, err
	}
	n.emit(Event{Type: "trip", Info: h})
	return v, nil
}

func (n *Node) publishTrip(ctx context.Context, relay string, s trip.Signed) error {
	rc, err := n.relayFor(relay)
	if err != nil {
		return err
	}
	return explainTripRelayError(rc.PutTrip(ctx, n.id, s))
}

// EndTrip takes the trip off its relay (leaders) and forgets it here. A
// member forgets it (after telling the leaders, if sharing).
func (n *Node) EndTrip(ctx context.Context, ref string) error {
	n.tr.mu.Lock()
	h, t, err := n.findTrip(ref)
	n.tr.mu.Unlock()
	if err != nil {
		return err
	}
	if t.id != nil {
		if rc, err := n.relayFor(t.card.Relay); err == nil {
			if err := rc.DeleteTrip(ctx, n.id, t.pub.Hash()); err != nil {
				var se *relayclient.StatusError
				if !errors.As(err, &se) || se.Code != 404 {
					return explainTripRelayError(err)
				}
			}
		}
	} else if t.sharing(time.Now()) {
		n.TripStop(h)
	}
	n.tr.mu.Lock()
	delete(n.tr.m, h)
	delete(n.tr.pos, h)
	n.saveTrips()
	n.tr.mu.Unlock()
	n.db.del("trippos:" + h)
	n.router.RemoveInbox(t.pub.Hash())
	n.router.SetDestTTL(t.pub.Hash(), 0)
	n.emit(Event{Type: "trip", Info: h})
	return nil
}

// TripMembers lists members' latest positions (leaders), SOS first, then by
// name.
func (n *Node) TripMembers(ref string) ([]TripMember, error) {
	n.tr.mu.Lock()
	defer n.tr.mu.Unlock()
	h, t, err := n.findTrip(ref)
	if err != nil {
		return nil, err
	}
	if t.id == nil {
		return nil, errors.New("only leaders see members")
	}
	out := make([]TripMember, 0, len(n.tr.pos[h]))
	for _, m := range n.tr.pos[h] {
		mm := *m
		if c, err := n.findContact(m.Hash); err == nil && c.Name != "" {
			mm.Name = c.Name
		}
		out = append(out, mm)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SOS != out[j].SOS {
			return out[i].SOS
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// LocateTripMember asks a member to report now and every minute for the
// next half hour (leaders).
func (n *Node) LocateTripMember(ref, member string) error {
	n.tr.mu.Lock()
	h, t, err := n.findTrip(ref)
	if err == nil && t.id == nil {
		err = errors.New("only leaders can ask")
	}
	var m *TripMember
	if err == nil {
		if m = n.tr.pos[h][strings.ToLower(member)]; m == nil {
			err = fmt.Errorf("no member %q in this trip", member)
		}
	}
	var pub identity.Public
	if err == nil {
		var b []byte
		if b, err = hex.DecodeString(m.Key); err == nil {
			pub, err = identity.PublicFromBytes(b)
		}
	}
	var tripHash packet.Hash
	if err == nil {
		tripHash = t.pub.Hash()
	}
	n.tr.mu.Unlock()
	if err != nil {
		return err
	}
	n.router.AddKnown(pub)
	_, err = n.router.SendContent(n.id, pub, e2e.ContentTrip, trip.Locate(tripHash, 30), n.heardFrom(pub.Hash().String()), true)
	return err
}

// ---- incoming ----

// onTripContent handles e2e.ContentTrip: reports to a trip we lead (local is
// the trip's identity), locate requests and leader hand-overs to us.
func (n *Node) onTripContent(local *identity.Identity, m *message.Received) {
	switch trip.Kind(m.Content) {
	case trip.KindReport:
		if local.Hash() == n.id.Hash() {
			return
		}
		r, err := trip.DecodeReport(m.Content)
		if err != nil {
			return
		}
		n.onTripReport(local.Hash().String(), m, r)
	case trip.KindLocate:
		th, minutes, err := trip.DecodeLocate(m.Content)
		if err != nil {
			return
		}
		h := th.String()
		n.tr.mu.Lock()
		t, ok := n.tr.m[h]
		if !ok || !t.card.IsLeader(m.From.Hash().String()) || !t.sharing(time.Now()) {
			n.tr.mu.Unlock()
			return // only the trip's leaders may ask, and only while we share
		}
		t.FastUntil = time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
		t.Last = 0 // report now
		n.saveTrips()
		n.tr.mu.Unlock()
		n.emit(Event{Type: "trip", Info: h})
	case trip.KindLead:
		id, s, c, err := trip.DecodeLead(m.Content)
		if err != nil || !c.IsLeader(m.From.Hash().String()) || !c.IsLeader(n.id.Hash().String()) {
			return // from a leader, naming us as one
		}
		h := id.Hash().String()
		n.tr.mu.Lock()
		t, ok := n.tr.m[h]
		if !ok {
			t = &tripState{}
			n.tr.m[h] = t
		}
		if !ok || c.Made >= t.card.Made {
			t.Signed = s
		}
		t.Seed = hex.EncodeToString(id.Seed())
		if err := t.load(); err != nil {
			delete(n.tr.m, h)
			n.tr.mu.Unlock()
			return
		}
		n.router.SetDestTTL(t.pub.Hash(), 0)
		n.tripHost(t)
		n.saveTrips()
		n.tr.mu.Unlock()
		n.router.Announce()
		n.emit(Event{Type: "trip", Info: h})
	}
}

// TripAlert is the Info (JSON) of a "trip_sos" event.
type TripAlert struct {
	Trip   string `json:"trip"`
	Name   string `json:"name"` // trip name
	Member string `json:"member"`
	Who    string `json:"who"`
	SOS    bool   `json:"sos"`
}

func (n *Node) onTripReport(h string, m *message.Received, r trip.Report) {
	from := m.From.Hash().String()
	n.tr.mu.Lock()
	t, ok := n.tr.m[h]
	if !ok {
		n.tr.mu.Unlock()
		return
	}
	pm := n.tr.pos[h]
	if pm == nil {
		pm = map[string]*TripMember{}
		n.tr.pos[h] = pm
	}
	mem := pm[from]
	if mem == nil {
		mem = &TripMember{Hash: from, Key: hex.EncodeToString(m.From.Bytes())}
		pm[from] = mem
	}
	sent := m.Sent.Unix()
	fix := sent - int64(r.FixAge)
	wasSOS := mem.SOS
	if sent >= mem.Seen || mem.Seen == 0 { // the newest report decides the state
		mem.Seen = sent
		if r.Name != "" {
			mem.Name = r.Name
		}
		mem.SOS = r.Flags&trip.FlagSOS != 0
		mem.Stopped = r.Flags&trip.FlagStopped != 0
		mem.Answer = r.Flags&trip.FlagAnswer != 0
		if r.Flags&trip.FlagNoFix == 0 {
			mem.Lat, mem.Lon, mem.Accuracy, mem.Fix, mem.NoFix = r.Lat, r.Lon, r.Accuracy, fix, false
		} else if mem.Fix == 0 {
			mem.NoFix = true
		}
		if r.Battery >= 0 {
			mem.Battery = r.Battery
		}
	}
	if r.Flags&trip.FlagNoFix == 0 {
		mem.Track = insertTrack(mem.Track, [3]float64{r.Lat, r.Lon, float64(fix)})
	}
	b, _ := json.Marshal(pm)
	name := t.card.Name
	alert := mem.SOS && !wasSOS
	who := mem.Name
	n.tr.mu.Unlock()
	n.db.set("trippos:"+h, string(b))
	n.emit(Event{Type: "trip", Info: h})
	if alert {
		a, _ := json.Marshal(TripAlert{Trip: h, Name: name, Member: from, Who: who, SOS: true})
		n.emit(Event{Type: "trip_sos", Info: string(a)})
	}
}

// insertTrack adds a point in time order (reports can arrive late and out of
// order through the mesh), skipping duplicates, keeping the newest tripTrack.
func insertTrack(tr [][3]float64, p [3]float64) [][3]float64 {
	i := sort.Search(len(tr), func(i int) bool { return tr[i][2] >= p[2] })
	if i < len(tr) && tr[i] == p {
		return tr
	}
	tr = append(tr, [3]float64{})
	copy(tr[i+1:], tr[i:])
	tr[i] = p
	if len(tr) > tripTrack {
		tr = tr[len(tr)-tripTrack:]
	}
	return tr
}

// pollTrip asks a relay for its trip card now and then (relay links call it
// whenever they are up); force asks now.
func (n *Node) pollTrip(ctx context.Context, l *relayLink, force bool) {
	n.tr.mu.Lock()
	if !force && time.Now().Before(n.tr.lastPoll[l.name]) {
		n.tr.mu.Unlock()
		return
	}
	n.tr.lastPoll[l.name] = time.Now().Add(tripRetry)
	n.tr.mu.Unlock()
	s, err := l.c.Trip(ctx)
	if err != nil {
		if !errors.Is(err, relayclient.ErrNoTrip) {
			n.cfg.Logf("%s: trip: %v", l.name, err)
		}
		return
	}
	n.tr.mu.Lock()
	n.tr.lastPoll[l.name] = time.Now().Add(tripPoll)
	n.tr.mu.Unlock()
	if _, err := n.OfferTrip(s); err != nil {
		n.cfg.Logf("%s: trip card: %v", l.name, err)
	}
}

// RefreshTrips asks every connected relay for its trip card now (the trips
// screen calls it when it opens).
func (n *Node) RefreshTrips(ctx context.Context) {
	var wg sync.WaitGroup
	for _, l := range n.allRelayLinks() {
		if !l.isUp() {
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); n.pollTrip(ctx, l, true) }()
	}
	wg.Wait()
}

// leaderTrips returns the identities of the trips we lead (to fetch their
// relay mailboxes).
func (n *Node) leaderTrips() []*identity.Identity {
	n.tr.mu.Lock()
	defer n.tr.mu.Unlock()
	var out []*identity.Identity
	for _, h := range sortedKeys(n.tr.m) {
		if t := n.tr.m[h]; t.id != nil {
			out = append(out, t.id)
		}
	}
	return out
}

// TripRelay is a relay a leader can start a trip on.
type TripRelay struct {
	Name string `json:"name"` // link name without "relay:"
	URL  string `json:"url"`
	Up   bool   `json:"up"`
}

// TripRelays lists the relays this node uses (a trip runs on one of them;
// only its admins can start it there).
func (n *Node) TripRelays() []TripRelay {
	var out []TripRelay
	for _, l := range n.allRelayLinks() {
		out = append(out, TripRelay{Name: strings.TrimPrefix(l.name, "relay:"), URL: l.c.URL(), Up: l.isUp()})
	}
	return out
}

// isTripDest reports whether dest is a trip this device knows (its packets
// get no receipts).
func (n *Node) isTripDest(dest packet.Hash) bool {
	n.tr.mu.Lock()
	defer n.tr.mu.Unlock()
	_, ok := n.tr.m[dest.String()]
	return ok
}
