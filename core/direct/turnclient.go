package direct

import (
	"context"
	"errors"
)

func (a *Agent) allocateTURN(ctx context.Context) (Candidate, error) {
	return Candidate{}, errors.New("direct: TURN client not built yet")
}
