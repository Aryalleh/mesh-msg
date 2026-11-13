package relayclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
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

// signMedia sets the account signature headers of a media request.
func signMedia(req *http.Request, as *identity.Identity, domain string, body []byte, day uint32) {
	req.Header.Set(wire.HeaderAccount, hex.EncodeToString(as.Public.Bytes()))
	req.Header.Set(wire.HeaderDay, strconv.FormatUint(uint64(day), 10))
	req.Header.Set(wire.HeaderSignature, hex.EncodeToString(as.SignDomain(domain, body)))
}

// IsAllowanceUsed tells whether an upload error means the account's daily
// allowance of important files on that relay is used up.
func IsAllowanceUsed(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusTooManyRequests && strings.Contains(se.Msg, "allowance")
}

// PutMedia uploads an important file's blob, signed by as (relays count
// each account's files per day); readers is how many downloads the relay
// waits for before deleting it.
func (c *Client) PutMedia(ctx context.Context, id string, blob []byte, readers int, as *identity.Identity) error {
	if as == nil {
		return errors.New("relay: media uploads are signed by an account")
	}
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
	// The work the relay wants now (it grows with today's uploads) and the
	// relay's day, which the upload is signed for.
	q, err := c.MediaQuota(ctx, as)
	if err != nil {
		return err
	}
	day := uint32(q.Resets/86400) - 1
	signMedia(req, as, wire.DomainMediaUpload, wire.MediaSigBody(id, day), day)
	powBody := wire.MediaPoWBody(id, as.Public.Hash().String(), day)
	bits := q.PowBits
	for try := 0; ; try++ {
		if bits > 0 {
			req.Header.Set(wire.HeaderPoW, strconv.FormatUint(pow.Solve(powBody, bits), 10))
		}
		req.Body = io.NopCloser(bytes.NewReader(blob))
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode == http.StatusPaymentRequired && try == 0 {
			if nb, err := strconv.Atoi(resp.Header.Get(wire.HeaderPoWBits)); err == nil && nb > bits && nb <= 32 {
				bits = nb // another upload raised it meanwhile
				continue
			}
		}
		if resp.StatusCode/100 != 2 {
			return &StatusError{resp.StatusCode, strings.TrimSpace(string(b))}
		}
		return nil
	}
}

// MediaQuota asks the relay how many important files the account may still
// upload today.
func (c *Client) MediaQuota(ctx context.Context, as *identity.Identity) (wire.MediaQuota, error) {
	var q wire.MediaQuota
	req, err := c.mediaReq(ctx, "GET", "quota", nil)
	if err != nil {
		return q, err
	}
	day := wire.Day(time.Now())
	signMedia(req, as, wire.DomainMediaQuota, wire.QuotaSigBody(day), day)
	resp, err := c.http.Do(req)
	if err != nil {
		return q, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return q, &StatusError{resp.StatusCode, strings.TrimSpace(string(b))}
	}
	return q, json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&q)
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

// TurnCredentials asks the relay for TURN credentials; only the relay's
// admins get them (403 otherwise, 404 when TURN is off).
func (c *Client) TurnCredentials(ctx context.Context, as *identity.Identity) (wire.TurnCredentials, error) {
	var tc wire.TurnCredentials
	u := *c.base
	u.Path += "/v1/turn/credentials"
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), nil)
	if err != nil {
		return tc, err
	}
	day := wire.Day(time.Now())
	signMedia(req, as, wire.DomainTurnCredentials, wire.QuotaSigBody(day), day)
	resp, err := c.http.Do(req)
	if err != nil {
		return tc, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return tc, &StatusError{resp.StatusCode, resp.Status}
	}
	return tc, json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&tc)
}

// Host is the relay's host name or address (for its UDP services).
func (c *Client) Host() string { return c.base.Hostname() }
