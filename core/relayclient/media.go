package relayclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/pow"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// MediaMaxBytes is the largest blob the relay takes (0: no media), from its
// info (fetched if needed).
func (c *Client) MediaMaxBytes(ctx context.Context) int {
	in, err := c.Info(ctx)
	if err != nil || in.MediaMaxMB <= 0 {
		return 0
	}
	return in.MediaMaxMB << 20
}

// KnownMediaMaxBytes is like MediaMaxBytes from the cached info only.
func (c *Client) KnownMediaMaxBytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.info == nil {
		return 0
	}
	return c.info.MediaMaxMB << 20
}

func (c *Client) mediaReq(ctx context.Context, method, id string, body io.Reader) (*http.Request, error) {
	u := *c.base
	u.Path += "/v1/media/" + id
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if c.opt.Origin != "" {
		req.Header.Set(wire.HeaderOrigin, c.opt.Origin)
	}
	return req, nil
}

// MediaAccountsOnly tells (from cached info) whether the relay takes media
// only from its listed accounts.
func (c *Client) MediaAccountsOnly() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info != nil && c.info.MediaAccountsOnly
}

// PutMedia uploads a blob; readers is how many downloads the relay waits
// for before deleting it. as signs the upload for relays that take media
// only from their accounts (it is not sent to other relays).
func (c *Client) PutMedia(ctx context.Context, id string, blob []byte, readers int, as *identity.Identity) error {
	in, err := c.Info(ctx)
	if err != nil {
		return err
	}
	if in.MediaMaxMB <= 0 {
		return errors.New("relay: no media on this relay")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := c.mediaReq(ctx, "PUT", id, bytes.NewReader(blob))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(blob))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(wire.HeaderReaders, strconv.Itoa(readers))
	if in.MediaAccountsOnly {
		if as == nil {
			return errors.New("relay: media only for its accounts")
		}
		req.Header.Set(wire.HeaderAccount, hex.EncodeToString(as.Public.Bytes()))
		req.Header.Set(wire.HeaderSignature, hex.EncodeToString(as.SignDomain(wire.DomainMediaUpload, []byte(id))))
	}
	if in.PowBits > 0 {
		req.Header.Set(wire.HeaderPoW, strconv.FormatUint(pow.Solve(wire.MediaPoWBody(id), in.PowBits), 10))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &StatusError{resp.StatusCode, strings.TrimSpace(string(b))}
	}
	return nil
}

// GetMedia downloads a blob from byte offset from into w and returns the
// blob's total size. A 404 is a *StatusError.
func (c *Client) GetMedia(ctx context.Context, id string, from int64, w io.Writer) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := c.mediaReq(ctx, "GET", id, nil)
	if err != nil {
		return 0, err
	}
	if from > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK && from > 0:
		return 0, errors.New("relay: range not honoured")
	case resp.StatusCode/100 != 2:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, &StatusError{resp.StatusCode, strings.TrimSpace(string(b))}
	}
	total := from + resp.ContentLength
	if resp.ContentLength < 0 {
		return 0, errors.New("relay: no length")
	}
	n, err := io.Copy(w, resp.Body)
	if err == nil && from+n != total {
		err = io.ErrUnexpectedEOF
	}
	return total, err
}
