package relayclient

import (
	"context"
	"net/url"
	"strconv"

	"github.com/Aryalleh/mesh-msg/core/wire"
)

// Usage ledger (docs/server.md §6a): relays exchange signed entries of
// important uploads. Bodies are concatenated fixed-size entries
// (server/ledger); this client only moves the bytes.

// PushUsage sends entries; it returns how many were new to the relay.
func (c *Client) PushUsage(ctx context.Context, entries []byte) (int, error) {
	_, b, err := c.do(ctx, "POST", "/v1/usage", nil, entries, nil, 0)
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(string(b))
	return n, nil
}

// PullUsage fetches entries after cursor since.
func (c *Client) PullUsage(ctx context.Context, since int64) (entries []byte, next int64, more bool, err error) {
	resp, b, err := c.do(ctx, "GET", "/v1/usage", url.Values{"since": {strconv.FormatInt(since, 10)}}, nil, nil, 0)
	if err != nil {
		return nil, since, false, err
	}
	return b, cursorOf(resp, since), resp.Header.Get(wire.HeaderMore) == "1", nil
}
