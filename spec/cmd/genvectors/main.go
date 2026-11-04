// Command genvectors prints spec/test-vectors/vectors.json.
package main

import (
	"encoding/json"
	"os"

	"github.com/Aryalleh/mesh-msg/spec"
)

func main() {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(spec.Generate()); err != nil {
		panic(err)
	}
}
