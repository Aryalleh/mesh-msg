package direct

import (
	"context"
	"errors"
)

// mapPort asks the router to forward an outside port to port (PCP, then
// NAT-PMP, then UPnP IGD). release removes the mapping. See portmap_*.go.
func mapPort(ctx context.Context, n Net, port uint16) (Candidate, func(), error) {
	return Candidate{}, nil, errors.New("direct: port mapping not built yet")
}
