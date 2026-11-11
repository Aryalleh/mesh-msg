//go:build tools

package meshcore

// Keeps golang.org/x/mobile in go.mod so `gomobile bind` uses a pinned version.
import _ "golang.org/x/mobile/bind"
