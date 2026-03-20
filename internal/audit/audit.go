package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

type Auditor interface {
	CreateEntry(ctx context.Context, p CreateEntryParams) (*Entry, error)
}

type Entry struct {
	ID            int64           `json:"id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	UserID        *uuid.UUID      `json:"user_id"`
	APIKeyID      *uuid.UUID      `json:"api_key_id"`
	Action        string          `json:"action"`
	EntityType    string          `json:"entity_type"`
	EntityID      string          `json:"entity_id"`
	BeforeData    json.RawMessage `json:"before_data"`
	AfterData     json.RawMessage `json:"after_data"`
	Diff          json.RawMessage `json:"diff"`
	IPAddress     *string         `json:"ip_address"`
	UserAgent     *string         `json:"user_agent"`
	CorrelationID *uuid.UUID      `json:"correlation_id"`
	Hash          string          `json:"hash"`
	PrevHash      string          `json:"prev_hash"`
	CreatedAt     time.Time       `json:"created_at"`
}

type CreateEntryParams struct {
	TenantID      uuid.UUID
	UserID        *uuid.UUID
	APIKeyID      *uuid.UUID
	Action        string
	EntityType    string
	EntityID      string
	BeforeData    json.RawMessage
	AfterData     json.RawMessage
	IPAddress     *string
	UserAgent     *string
	CorrelationID *uuid.UUID
}

type AuditEvent struct {
	TenantID      uuid.UUID       `json:"tenant_id"`
	UserID        *uuid.UUID      `json:"user_id,omitempty"`
	APIKeyID      *uuid.UUID      `json:"api_key_id,omitempty"`
	Action        string          `json:"action"`
	EntityType    string          `json:"entity_type"`
	EntityID      string          `json:"entity_id"`
	BeforeData    json.RawMessage `json:"before_data,omitempty"`
	AfterData     json.RawMessage `json:"after_data,omitempty"`
	IPAddress     *string         `json:"ip_address,omitempty"`
	UserAgent     *string         `json:"user_agent,omitempty"`
	CorrelationID *uuid.UUID      `json:"correlation_id,omitempty"`
}

type VerifyResult struct {
	Verified       bool   `json:"verified"`
	EntriesChecked int    `json:"entries_checked"`
	FirstInvalid   *int64 `json:"first_invalid"`
}

func ComputeHash(entry Entry, prevHash string) string {
	userID := "system"
	if entry.UserID != nil {
		userID = entry.UserID.String()
	}

	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		entry.TenantID.String(),
		userID,
		entry.Action,
		entry.EntityType,
		entry.EntityID,
		entry.CreatedAt.Format(time.RFC3339Nano),
		prevHash,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func ComputeDiff(before, after json.RawMessage) json.RawMessage {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}

	var beforeMap map[string]interface{}
	var afterMap map[string]interface{}

	if len(before) > 0 {
		if err := json.Unmarshal(before, &beforeMap); err != nil {
			beforeMap = nil
		}
	}
	if len(after) > 0 {
		if err := json.Unmarshal(after, &afterMap); err != nil {
			afterMap = nil
		}
	}

	if beforeMap == nil && afterMap == nil {
		return nil
	}

	diff := make(map[string]interface{})

	if beforeMap == nil {
		for k, v := range afterMap {
			diff[k] = map[string]interface{}{"from": nil, "to": v}
		}
	} else if afterMap == nil {
		for k, v := range beforeMap {
			diff[k] = map[string]interface{}{"from": v, "to": nil}
		}
	} else {
		allKeys := make(map[string]bool)
		for k := range beforeMap {
			allKeys[k] = true
		}
		for k := range afterMap {
			allKeys[k] = true
		}

		sortedKeys := make([]string, 0, len(allKeys))
		for k := range allKeys {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)

		for _, k := range sortedKeys {
			bVal, bOK := beforeMap[k]
			aVal, aOK := afterMap[k]

			if !bOK {
				diff[k] = map[string]interface{}{"from": nil, "to": aVal}
			} else if !aOK {
				diff[k] = map[string]interface{}{"from": bVal, "to": nil}
			} else {
				bJSON, _ := json.Marshal(bVal)
				aJSON, _ := json.Marshal(aVal)
				if string(bJSON) != string(aJSON) {
					diff[k] = map[string]interface{}{"from": bVal, "to": aVal}
				}
			}
		}
	}

	if len(diff) == 0 {
		return nil
	}

	result, err := json.Marshal(diff)
	if err != nil {
		return nil
	}
	return result
}

func VerifyChain(entries []Entry) *VerifyResult {
	result := &VerifyResult{
		Verified:       true,
		EntriesChecked: len(entries),
	}

	if len(entries) <= 1 {
		return result
	}

	for i := 1; i < len(entries); i++ {
		prev := entries[i-1]
		curr := entries[i]

		if curr.PrevHash != prev.Hash {
			result.Verified = false
			result.FirstInvalid = &curr.ID
			return result
		}

		expectedHash := ComputeHash(curr, prev.Hash)
		if curr.Hash != expectedHash {
			result.Verified = false
			result.FirstInvalid = &curr.ID
			return result
		}
	}

	return result
}
