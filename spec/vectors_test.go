package spec

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// TestVectorsUpToDate fails if the committed vectors differ from what the
// current code produces (regenerate with go run ./spec/cmd/genvectors).
func TestVectorsUpToDate(t *testing.T) {
	committed, err := os.ReadFile("test-vectors/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	enc.Encode(Generate())
	if !bytes.Equal(committed, buf.Bytes()) {
		t.Fatal("spec/test-vectors/vectors.json is stale or the protocol changed: " +
			"regenerate with `go run ./spec/cmd/genvectors > spec/test-vectors/vectors.json` and update the firmware")
	}
}
