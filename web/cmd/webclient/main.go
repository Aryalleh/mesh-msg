//go:build js && wasm

// The browser client (docs/web-access.md): web/lite compiled to WebAssembly,
// exposed to the page as `window.mava`. Built by scripts/build-web.sh and
// served by the Mava board from LittleFS; also usable from any web server.
//
//	mava.start(name, relaysCSV, boardURL) → our URI   (boardURL "" = same origin, "-" = none)
//	mava.uri(), mava.link(), mava.hash()
//	mava.contacts() / mava.messages(hash)     → JSON
//	mava.addContact(uri, name)                → JSON contact, or throws
//	mava.removeContact(hash)
//	mava.send(hash, text)                     → JSON message, or throws
//	mava.setName(name)
//	mava.onEvent(fn)                          fn(json) on message / status / contact / link
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"syscall/js"
	"time"

	"github.com/Aryalleh/mesh-msg/web/lite"
)

const stateKey = "mava.state"

type localStore struct{}

func (localStore) Load() ([]byte, error) {
	v := js.Global().Get("localStorage").Call("getItem", stateKey)
	if v.IsNull() || v.IsUndefined() {
		return nil, nil
	}
	return []byte(v.String()), nil
}

func (localStore) Save(b []byte) error {
	js.Global().Get("localStorage").Call("setItem", stateKey, string(b))
	return nil
}

var (
	client  *lite.Client
	handler js.Value
)

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func throw(err error) any {
	return js.Global().Get("Error").New(err.Error())
}

// fn wraps a Go function; an error result is thrown in JavaScript.
func fn(f func(args []js.Value) (any, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any {
		v, err := f(args)
		if err != nil {
			js.Global().Get("console").Call("warn", err.Error())
			panic(throw(err))
		}
		return v
	})
}

func arg(args []js.Value, i int) string {
	if i < len(args) && args[i].Type() == js.TypeString {
		return args[i].String()
	}
	return ""
}

func start(args []js.Value) (any, error) {
	if client != nil {
		return client.URI(), nil
	}
	c, err := lite.Open(localStore{}, arg(args, 0))
	if err != nil {
		return nil, err
	}
	client = c
	c.OnEvent(func(e lite.Event) {
		if handler.Type() == js.TypeFunction {
			handler.Invoke(jsonOf(e))
		}
	})
	ctx := context.Background()
	if board := arg(args, 2); board != "-" {
		var s [8]byte
		_, _ = rand.Read(s[:])
		b := lite.NewBoard(c, board, hex.EncodeToString(s[:]))
		c.AddTransport(b)
		go b.Run(ctx)
	}
	for _, u := range strings.Split(arg(args, 1), ",") {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		r, err := lite.NewRelay(c, u)
		if err != nil {
			continue
		}
		c.AddTransport(r)
		go r.Run(ctx)
	}
	go func() {
		for range time.Tick(time.Minute) {
			c.Tick()
		}
	}()
	return c.URI(), nil
}

func main() {
	api := map[string]any{
		"start":    fn(start),
		"uri":      fn(func([]js.Value) (any, error) { return client.URI(), nil }),
		"link":     fn(func([]js.Value) (any, error) { return client.Link(), nil }),
		"hash":     fn(func([]js.Value) (any, error) { return client.Hash(), nil }),
		"contacts": fn(func([]js.Value) (any, error) { return jsonOf(client.Contacts()), nil }),
		"messages": fn(func(a []js.Value) (any, error) { return jsonOf(client.Messages(arg(a, 0))), nil }),
		"addContact": fn(func(a []js.Value) (any, error) {
			c, err := client.AddContact(arg(a, 0), arg(a, 1))
			return jsonOf(c), err
		}),
		"removeContact": fn(func(a []js.Value) (any, error) { return nil, client.RemoveContact(arg(a, 0)) }),
		"send": fn(func(a []js.Value) (any, error) {
			m, err := client.Send(arg(a, 0), arg(a, 1))
			return jsonOf(m), err
		}),
		"setName": fn(func(a []js.Value) (any, error) { return nil, client.SetName(arg(a, 0)) }),
		"onEvent": fn(func(a []js.Value) (any, error) {
			if len(a) > 0 {
				handler = a[0]
			}
			return nil, nil
		}),
	}
	js.Global().Set("mava", js.ValueOf(api))
	js.Global().Call("dispatchEvent", js.Global().Get("Event").New("mava-ready"))
	select {}
}
