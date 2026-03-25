package diameter

import (
	"context"

	"github.com/btopcu/argus/internal/store"
)

type SIMResolver interface {
	GetByIMSI(ctx context.Context, imsi string) (*store.SIM, error)
}
