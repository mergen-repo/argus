package alertstate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

func DedupKey(tenantID uuid.UUID, alertType, source string, simID, operatorID, apnID *uuid.UUID) string {
	entityTriple := "-"
	switch {
	case simID != nil:
		entityTriple = "sim:" + simID.String()
	case operatorID != nil:
		entityTriple = "op:" + operatorID.String()
	case apnID != nil:
		entityTriple = "apn:" + apnID.String()
	}
	raw := fmt.Sprintf("%s|%s|%s|%s", tenantID.String(), alertType, source, entityTriple)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
