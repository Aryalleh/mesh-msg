// devboard serves the browser client the way a Mava board does, for
// development and the browser test (scripts/test-web.sh):
//
//	/app/      the built client (scripts/build-web.sh with GZIP_FILES=0)
//	/api/link  a stand-in for the board's host link: every packet one browser
//	           session sends reaches the other sessions, as over LoRa
//
// and runs a relay on a second port (cross-origin, so CORS is exercised).
//
//	go run ./web/cmd/devboard -dir /tmp/web -board 127.0.0.1:8080 -relay 127.0.0.1:8081
package main

import (
	"encoding/base64"
	"flag"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/Aryalleh/mesh-msg/core/hostlink"
	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/wire"
	"github.com/Aryalleh/mesh-msg/server/api"
	"github.com/Aryalleh/mesh-msg/server/relay"
)

type board struct {
	mu  sync.Mutex
	dec map[string]*hostlink.Decoder
	out map[string][]byte
}

func (b *board) link(w http.ResponseWriter, r *http.Request) {
	s := r.URL.Query().Get("s")
	body, _ := io.ReadAll(r.Body)
	in, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil || s == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dec[s] == nil {
		b.dec[s] = &hostlink.Decoder{}
	}
	for _, f := range b.dec[s].Feed(in) {
		if f.Kind != hostlink.KindPacket {
			continue
		}
		for other := range b.dec {
			if other != s {
				enc, _ := hostlink.Encode(hostlink.KindPacket, f.Payload)
				b.out[other] = append(b.out[other], enc...)
			}
		}
	}
	io.WriteString(w, base64.StdEncoding.EncodeToString(b.out[s]))
	b.out[s] = nil
}

func main() {
	dir := flag.String("dir", "firmware/data/web", "built client (uncompressed)")
	boardAddr := flag.String("board", "127.0.0.1:8080", "board address")
	relayAddr := flag.String("relay", "127.0.0.1:8081", "relay address ('' = none)")
	flag.Parse()
	if *relayAddr != "" {
		key, _ := identity.Generate()
		rl := relay.New(relay.Config{Name: "dev", Region: "global"})
		h := api.New(api.Config{Relay: rl, Identity: key, PowBits: 8, RateLimit: 1000,
			Relays: []wire.RelayEntry{{Name: "dev", Region: "global", URLs: []string{"http://" + *relayAddr}}}})
		go func() { log.Fatal(http.ListenAndServe(*relayAddr, h)) }()
		log.Printf("relay on http://%s", *relayAddr)
	}
	b := &board{dec: map[string]*hostlink.Decoder{}, out: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/link", b.link)
	mux.Handle("/app/", http.StripPrefix("/app/", http.FileServer(http.Dir(*dir))))
	log.Printf("board on http://%s/app/", *boardAddr)
	log.Fatal(http.ListenAndServe(*boardAddr, mux))
}
