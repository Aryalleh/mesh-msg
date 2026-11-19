package relayclient

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/Aryalleh/mesh-msg/core/identity"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"github.com/Aryalleh/mesh-msg/core/trip"
	"github.com/Aryalleh/mesh-msg/core/wire"
)

// ErrNoTrip means the relay has no trip running.
var ErrNoTrip = errors.New("relay: no trip")

// Trip fetches the relay's current trip card (GET /v1/trip). The caller
// still verifies it (trip.Signed.Open).
func (c *Client) Trip(ctx context.Context) (trip.Signed, error) {
	_, b, err := c.do(ctx, "GET", "/v1/trip", nil, nil, nil, 0)
	var se *StatusError
	if errors.As(err, &se) && se.Code == 404 {
		return trip.Signed{}, ErrNoTrip
	}
	if err != nil {
		return trip.Signed{}, err
	}
	return trip.ParseSigned(string(b))
}

func tripHeaders(as *identity.Identity, domain string, what []byte) map[string]string {
	day := wire.Day(time.Now())
	return map[string]string{
		wire.HeaderAccount:   hex.EncodeToString(as.Public.Bytes()),
		wire.HeaderDay:       strconv.FormatUint(uint64(day), 10),
		wire.HeaderSignature: hex.EncodeToString(as.SignDomain(domain, wire.TripSigBody(day, what))),
	}
}

// PutTrip publishes a trip card on the relay (PUT /v1/trip), signed by the
// account as: only the relay's admins may, and every leader must be one.
func (c *Client) PutTrip(ctx context.Context, as *identity.Identity, s trip.Signed) error {
	d := s.Digest()
	_, _, err := c.do(ctx, "PUT", "/v1/trip", nil, s.JSON(), tripHeaders(as, wire.DomainTripPut, d[:]), 0)
	return err
}

// DeleteTrip ends the relay's trip (DELETE /v1/trip?trip=<hash>).
func (c *Client) DeleteTrip(ctx context.Context, as *identity.Identity, tripHash packet.Hash) error {
	q := map[string][]string{"trip": {tripHash.String()}}
	_, _, err := c.do(ctx, "DELETE", "/v1/trip", q, nil, tripHeaders(as, wire.DomainTripDelete, tripHash[:]), 0)
	return err
}
