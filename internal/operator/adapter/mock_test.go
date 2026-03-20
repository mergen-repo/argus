package adapter

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestMockAuthenticateSuccess(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":1,"success_rate":100}`)
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	resp, err := m.Authenticate(context.Background(), AuthenticateRequest{
		IMSI: "001010000000001",
		APN:  "internet",
	})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Code != AuthAccept {
		t.Errorf("code = %q, want %q", resp.Code, AuthAccept)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestMockAuthenticateReject(t *testing.T) {
	rate := float64(0)
	cfg, _ := json.Marshal(MockConfig{
		LatencyMs:   1,
		SuccessRate: &rate,
		ErrorType:   "auth_failed",
	})
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	resp, err := m.Authenticate(context.Background(), AuthenticateRequest{
		IMSI: "001010000000001",
	})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.Code != AuthReject {
		t.Errorf("code = %q, want %q", resp.Code, AuthReject)
	}
}

func TestMockAuthenticateConfigurableRate(t *testing.T) {
	rate := float64(50)
	cfg, _ := json.Marshal(MockConfig{
		LatencyMs:   1,
		SuccessRate: &rate,
	})
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	successCount := 0
	total := 200
	for i := 0; i < total; i++ {
		resp, err := m.Authenticate(context.Background(), AuthenticateRequest{
			IMSI: "001010000000001",
		})
		if err != nil {
			t.Fatalf("authenticate iteration %d: %v", i, err)
		}
		if resp.Success {
			successCount++
		}
	}

	successRate := float64(successCount) / float64(total) * 100
	if successRate < 20 || successRate > 80 {
		t.Errorf("success rate = %.1f%%, expected roughly 50%% (got %d/%d)", successRate, successCount, total)
	}
}

func TestMockAccountingUpdateSuccess(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":1,"success_rate":100}`)
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	err = m.AccountingUpdate(context.Background(), AccountingUpdateRequest{
		IMSI:       "001010000000001",
		SessionID:  "test-session",
		StatusType: AcctStart,
	})
	if err != nil {
		t.Errorf("accounting update: %v", err)
	}
}

func TestMockAccountingUpdateReject(t *testing.T) {
	rate := float64(0)
	cfg, _ := json.Marshal(MockConfig{
		LatencyMs:   1,
		SuccessRate: &rate,
	})
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	err = m.AccountingUpdate(context.Background(), AccountingUpdateRequest{
		IMSI:       "001010000000001",
		SessionID:  "test-session",
		StatusType: AcctInterim,
	})
	if err == nil {
		t.Error("expected error for 0% success rate")
	}
}

func TestMockFetchAuthVectorsTriplets(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":1}`)
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	vectors, err := m.FetchAuthVectors(context.Background(), "001010000000001", 3)
	if err != nil {
		t.Fatalf("fetch vectors: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("vector count = %d, want 3", len(vectors))
	}

	for i, v := range vectors {
		if i%2 == 0 {
			if v.Type != VectorTypeTriplet {
				t.Errorf("vector[%d].Type = %q, want %q", i, v.Type, VectorTypeTriplet)
			}
			if len(v.RAND) != 16 {
				t.Errorf("vector[%d].RAND len = %d, want 16", i, len(v.RAND))
			}
			if len(v.SRES) != 4 {
				t.Errorf("vector[%d].SRES len = %d, want 4", i, len(v.SRES))
			}
			if len(v.Kc) != 8 {
				t.Errorf("vector[%d].Kc len = %d, want 8", i, len(v.Kc))
			}
		} else {
			if v.Type != VectorTypeQuintet {
				t.Errorf("vector[%d].Type = %q, want %q", i, v.Type, VectorTypeQuintet)
			}
			if len(v.RAND) != 16 {
				t.Errorf("vector[%d].RAND len = %d, want 16", i, len(v.RAND))
			}
			if len(v.AUTN) != 16 {
				t.Errorf("vector[%d].AUTN len = %d, want 16", i, len(v.AUTN))
			}
			if len(v.XRES) != 8 {
				t.Errorf("vector[%d].XRES len = %d, want 8", i, len(v.XRES))
			}
			if len(v.CK) != 16 {
				t.Errorf("vector[%d].CK len = %d, want 16", i, len(v.CK))
			}
			if len(v.IK) != 16 {
				t.Errorf("vector[%d].IK len = %d, want 16", i, len(v.IK))
			}
		}
	}
}

func TestMockFetchAuthVectorsQuintets(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":1}`)
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	vectors, err := m.FetchAuthVectors(context.Background(), "001010000000001", 2)
	if err != nil {
		t.Fatalf("fetch vectors: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("vector count = %d, want 2", len(vectors))
	}

	if vectors[0].Type != VectorTypeTriplet {
		t.Errorf("vector[0].Type = %q, want %q", vectors[0].Type, VectorTypeTriplet)
	}
	if vectors[1].Type != VectorTypeQuintet {
		t.Errorf("vector[1].Type = %q, want %q", vectors[1].Type, VectorTypeQuintet)
	}

	if len(vectors[1].RAND) != 16 {
		t.Errorf("quintet RAND len = %d, want 16", len(vectors[1].RAND))
	}
	if len(vectors[1].AUTN) != 16 {
		t.Errorf("quintet AUTN len = %d, want 16", len(vectors[1].AUTN))
	}
	if len(vectors[1].XRES) != 8 {
		t.Errorf("quintet XRES len = %d, want 8", len(vectors[1].XRES))
	}
	if len(vectors[1].CK) != 16 {
		t.Errorf("quintet CK len = %d, want 16", len(vectors[1].CK))
	}
	if len(vectors[1].IK) != 16 {
		t.Errorf("quintet IK len = %d, want 16", len(vectors[1].IK))
	}
}

func TestMockFetchAuthVectorsDeterministic(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":1}`)
	m1, _ := NewMockAdapter(cfg)
	m2, _ := NewMockAdapter(cfg)

	imsi := "001010000000001"
	v1, err := m1.FetchAuthVectors(context.Background(), imsi, 3)
	if err != nil {
		t.Fatalf("fetch vectors 1: %v", err)
	}
	v2, err := m2.FetchAuthVectors(context.Background(), imsi, 3)
	if err != nil {
		t.Fatalf("fetch vectors 2: %v", err)
	}

	for i := range v1 {
		if v1[i].Type != v2[i].Type {
			t.Errorf("vector[%d] type mismatch: %q vs %q", i, v1[i].Type, v2[i].Type)
		}
		if string(v1[i].RAND) != string(v2[i].RAND) {
			t.Errorf("vector[%d] RAND mismatch", i)
		}
	}
}

func TestMockHealthCheckAlwaysSucceeds(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":1}`)
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	result := m.HealthCheck(context.Background())
	if !result.Success {
		t.Errorf("health check failed: %s", result.Error)
	}
}

func TestMockAdapterTimeout(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":500}`)
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = m.Authenticate(ctx, AuthenticateRequest{IMSI: "001010000000001"})
	if err != ErrAdapterTimeout {
		t.Errorf("expected ErrAdapterTimeout, got %v", err)
	}
}

func TestMockAdapterConcurrent(t *testing.T) {
	cfg := json.RawMessage(`{"latency_ms":1,"success_rate":80}`)
	m, err := NewMockAdapter(cfg)
	if err != nil {
		t.Fatalf("create mock: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			m.Authenticate(context.Background(), AuthenticateRequest{IMSI: "001010000000001"})
		}()
		go func() {
			defer wg.Done()
			m.FetchAuthVectors(context.Background(), "001010000000001", 2)
		}()
		go func() {
			defer wg.Done()
			m.AccountingUpdate(context.Background(), AccountingUpdateRequest{
				IMSI:       "001010000000001",
				SessionID:  "test",
				StatusType: AcctInterim,
			})
		}()
	}
	wg.Wait()
}
