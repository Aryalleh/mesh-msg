package admin

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Aryalleh/mesh-msg/core/e2e"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/message"
	"github.com/Aryalleh/mesh-msg/core/trip"
)

// TestTripMapCrypto opens a report sealed by the Go core with the web map's
// JavaScript (needs node; skipped without it).
func TestTripMapCrypto(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	tr, _ := identity.Generate()
	member, _ := identity.Generate()
	r := trip.Report{Flags: trip.FlagSOS, Lat: 32.6160567, Lon: 44.0248793, Accuracy: 9, FixAge: 12, Battery: 71, Name: "زهرا"}
	pkts, err := message.BuildContent(member, tr.Public, e2e.ContentTrip, r.Encode(), message.Options{Now: time.Unix(1_800_000_000, 0)})
	if err != nil || len(pkts) != 1 {
		t.Fatalf("build: %v (%d packets)", err, len(pkts))
	}
	raw := pkts[0].MustEncode()
	other, _ := identity.Generate()
	dir := t.TempDir()
	script := `const T = require(` + jsString(filepath.Join(mustAbs(t, "."), "tripcrypto.js")) + `);
const [priv, pub, raw, otherPriv, otherPub] = process.argv.slice(2).map(T.unhex);
(async () => {
  const derived = T.hex(T.x25519(priv, T.BASE));
  const r = await T.openReport(priv, pub, raw);
  const wrong = await T.openReport(otherPriv, otherPub, raw);
  console.log(JSON.stringify({ derived, r, wrong }));
})().catch((e) => { console.error(e); process.exit(1); });`
	f := filepath.Join(dir, "t.js")
	os.WriteFile(f, []byte(script), 0o600)
	out, err := exec.Command(node, f, hex.EncodeToString(tr.XPrivate()), hex.EncodeToString(tr.X[:]), hex.EncodeToString(raw),
		hex.EncodeToString(other.XPrivate()), hex.EncodeToString(other.X[:])).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Derived string
		R       *struct {
			Member, Key, Name     string
			Sent                  int64
			Flags, Accuracy       int
			FixAge, Battery       int
			Lat, Lon              float64
		}
		Wrong any
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.Derived != hex.EncodeToString(tr.X[:]) {
		t.Fatalf("x25519 public %s, want %x", got.Derived, tr.X)
	}
	if got.R == nil || got.R.Member != member.Hash().String() || got.R.Key != hex.EncodeToString(member.Bytes()) ||
		got.R.Name != "زهرا" || got.R.Sent != 1_800_000_000 || got.R.Flags != trip.FlagSOS || got.R.Accuracy != 9 ||
		got.R.FixAge != 12 || got.R.Battery != 71 || got.R.Lat != 32.6160567 || got.R.Lon != 44.0248793 {
		t.Fatalf("report %+v", got.R)
	}
	if got.Wrong != nil {
		t.Fatal("another key opened the report")
	}
}

func jsString(s string) string { b, _ := json.Marshal(s); return string(b) }

func mustAbs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
