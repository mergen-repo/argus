package job

import (
	"context"
	"encoding/json"

	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

type importSIMWriter interface {
	Create(ctx context.Context, tenantID uuid.UUID, p store.CreateSIMParams) (*store.SIM, error)
	TransitionState(ctx context.Context, simID uuid.UUID, targetState string, userID *uuid.UUID, triggeredBy string, reason interface{}, purgeRetentionDays int) (*store.SIM, error)
	SetIPAndPolicy(ctx context.Context, simID uuid.UUID, ipAddressID *uuid.UUID, policyVersionID *uuid.UUID) error
}

type importJobStore interface {
	UpdateProgress(ctx context.Context, jobID uuid.UUID, processed, failed, total int) error
	CheckCancelled(ctx context.Context, jobID uuid.UUID) (bool, error)
	Complete(ctx context.Context, jobID uuid.UUID, errorReport json.RawMessage, result json.RawMessage) error
}

type importOperatorReader interface {
	GetByCode(ctx context.Context, code string) (*store.Operator, error)
}

type importAPNReader interface {
	GetByName(ctx context.Context, tenantID, operatorID uuid.UUID, name string) (*store.APN, error)
}

type importIPPoolManager interface {
	List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int, apnIDFilter *uuid.UUID) ([]store.IPPool, string, error)
	ReserveStaticIP(ctx context.Context, poolID, simID uuid.UUID, addressV4 *string) (*store.IPAddress, error)
}

type importPolicyReader interface {
	ListReferencingAPN(ctx context.Context, tenantID uuid.UUID, apnName string, limit int, cursor string) ([]store.Policy, string, error)
}

type importEventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type importNotifier interface {
	Notify(ctx context.Context, req notification.NotifyRequest) error
}
