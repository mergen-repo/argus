package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

func BenchmarkRADIUSPacketBuild(b *testing.B) {
	secret := []byte("benchsecret123")
	imsi := "286010000000001"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := radius.New(radius.CodeAccessRequest, secret)
		rfc2865.UserName_SetString(req, imsi)
		rfc2865.NASIPAddress_Set(req, net.ParseIP("10.0.0.1").To4())

		accept := req.Response(radius.CodeAccessAccept)
		rfc2865.FramedIPAddress_Set(accept, net.ParseIP("10.0.1.100").To4())
		rfc2865.SessionTimeout_Set(accept, rfc2865.SessionTimeout(86400))
		rfc2865.IdleTimeout_Set(accept, rfc2865.IdleTimeout(3600))
		rfc2865.FilterID_SetString(accept, "default")

		_ = accept
	}
}

func BenchmarkRADIUSPacketResponse(b *testing.B) {
	secret := []byte("benchsecret123")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := radius.New(radius.CodeAccessRequest, secret)
		rfc2865.UserName_SetString(req, "286010000000001")

		resp := req.Response(radius.CodeAccessAccept)
		_ = resp
	}
}

func BenchmarkRADIUSRejectBuild(b *testing.B) {
	secret := []byte("benchsecret123")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := radius.New(radius.CodeAccessRequest, secret)
		reject := req.Response(radius.CodeAccessReject)
		rfc2865.ReplyMessage_SetString(reject, "SIM_NOT_FOUND")
		_ = reject
	}
}

func BenchmarkRADIUSAcctPacketBuild(b *testing.B) {
	secret := []byte("benchsecret123")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := radius.New(radius.CodeAccountingRequest, secret)
		rfc2865.UserName_SetString(req, "286010000000001")
		rfc2866.AcctSessionID_SetString(req, "acct-session-12345")
		rfc2866.AcctInputOctets_Set(req, rfc2866.AcctInputOctets(1048576))
		rfc2866.AcctOutputOctets_Set(req, rfc2866.AcctOutputOctets(2097152))

		resp := req.Response(radius.CodeAccountingResponse)
		_ = resp
	}
}

func BenchmarkSIMCacheLookup(b *testing.B) {
	ctx := context.Background()
	cache := newMockSIMCache(10000)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		imsi := cache.randomIMSI()
		_, _ = cache.GetByIMSI(ctx, imsi)
	}
}

func BenchmarkSIMCacheLookupParallel(b *testing.B) {
	ctx := context.Background()
	cache := newMockSIMCache(10000)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			imsi := cache.randomIMSI()
			_, _ = cache.GetByIMSI(ctx, imsi)
		}
	})
}

func BenchmarkSessionCreate(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sess := &session.Session{
			ID:            uuid.New().String(),
			SimID:         uuid.New().String(),
			TenantID:      uuid.New().String(),
			OperatorID:    uuid.New().String(),
			IMSI:          "286010000000001",
			APN:           "internet",
			NASIP:         "10.0.0.1",
			AcctSessionID: "acct-12345",
			FramedIP:      "10.0.1.100",
			SessionState:  "active",
			BytesIn:       0,
			BytesOut:      0,
			StartedAt:     time.Now().UTC(),
			LastInterimAt: time.Now().UTC(),
		}
		_ = sess
	}
}

func BenchmarkSessionMarshalJSON(b *testing.B) {
	sess := &session.Session{
		ID:            uuid.New().String(),
		SimID:         uuid.New().String(),
		TenantID:      uuid.New().String(),
		OperatorID:    uuid.New().String(),
		IMSI:          "286010000000001",
		APN:           "internet",
		NASIP:         "10.0.0.1",
		AcctSessionID: "acct-12345",
		FramedIP:      "10.0.1.100",
		SessionState:  "active",
		BytesIn:       1048576,
		BytesOut:      2097152,
		StartedAt:     time.Now().UTC(),
		LastInterimAt: time.Now().UTC(),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(sess)
		_ = data
	}
}

func BenchmarkSessionUnmarshalJSON(b *testing.B) {
	sess := &session.Session{
		ID:            uuid.New().String(),
		SimID:         uuid.New().String(),
		TenantID:      uuid.New().String(),
		OperatorID:    uuid.New().String(),
		IMSI:          "286010000000001",
		APN:           "internet",
		SessionState:  "active",
		BytesIn:       1048576,
		BytesOut:      2097152,
		StartedAt:     time.Now().UTC(),
		LastInterimAt: time.Now().UTC(),
	}
	data, _ := json.Marshal(sess)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var s session.Session
		_ = json.Unmarshal(data, &s)
	}
}

func BenchmarkPolicyEvaluateSimple(b *testing.B) {
	evaluator := dsl.NewEvaluator()
	policy := &dsl.CompiledPolicy{
		Name:    "iot-default",
		Version: "1",
		Match: dsl.CompiledMatch{
			Conditions: []dsl.CompiledMatchCondition{
				{Field: "apn", Op: "eq", Value: "internet"},
			},
		},
		Rules: dsl.CompiledRules{
			Defaults: map[string]interface{}{
				"bandwidth_down": float64(10000000),
				"bandwidth_up":   float64(5000000),
				"priority":       int64(5),
			},
		},
	}

	ctx := dsl.SessionContext{
		APN:      "internet",
		Operator: "turkcell",
		RATType:  "lte",
		Usage:    500000,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = evaluator.Evaluate(ctx, policy)
	}
}

func BenchmarkPolicyEvaluateComplex(b *testing.B) {
	evaluator := dsl.NewEvaluator()
	policy := &dsl.CompiledPolicy{
		Name:    "iot-complex",
		Version: "1",
		Match: dsl.CompiledMatch{
			Conditions: []dsl.CompiledMatchCondition{
				{Field: "apn", Op: "in", Values: []interface{}{"internet", "m2m.global", "iot.1nce"}},
				{Field: "operator", Op: "in", Values: []interface{}{"turkcell", "vodafone", "turk_telekom"}},
			},
		},
		Rules: dsl.CompiledRules{
			Defaults: map[string]interface{}{
				"bandwidth_down": float64(10000000),
				"bandwidth_up":   float64(5000000),
				"priority":       int64(5),
				"max_sessions":   int64(3),
			},
			WhenBlocks: []dsl.CompiledWhenBlock{
				{
					Condition: &dsl.CompiledCondition{
						Field: "usage",
						Op:    "gt",
						Value: int64(1073741824),
					},
					Assignments: map[string]interface{}{
						"bandwidth_down": float64(2000000),
						"bandwidth_up":   float64(1000000),
					},
					Actions: []dsl.CompiledAction{
						{Type: "throttle", Params: map[string]interface{}{"rate": float64(2000000)}},
						{Type: "notify", Params: map[string]interface{}{"event_type": "usage_exceeded"}},
					},
				},
				{
					Condition: &dsl.CompiledCondition{
						Op: "and",
						Left: &dsl.CompiledCondition{
							Field: "rat_type",
							Op:    "eq",
							Value: "5g_nr",
						},
						Right: &dsl.CompiledCondition{
							Field: "max_sessions",
							Op:    "eq",
							Value: int64(1),
						},
					},
					Assignments: map[string]interface{}{
						"bandwidth_down": float64(100000000),
						"bandwidth_up":   float64(50000000),
						"priority":       int64(1),
					},
				},
				{
					Condition: &dsl.CompiledCondition{
						Field:  "time_of_day",
						Op:     "in",
						Values: []interface{}{"00:00-06:00"},
					},
					Assignments: map[string]interface{}{
						"bandwidth_down": float64(50000000),
					},
				},
			},
		},
		Charging: &dsl.CompiledCharging{
			Model:        "per_mb",
			RatePerMB:    0.01,
			BillingCycle: "monthly",
			Quota:        5368709120,
			RATMultiplier: map[string]float64{
				"5g_nr": 1.5,
				"lte":   1.0,
				"3g":    0.8,
			},
		},
	}

	ctx := dsl.SessionContext{
		APN:           "internet",
		Operator:      "turkcell",
		RATType:       "lte",
		Usage:         2147483648,
		TimeOfDay:     "14:30",
		DayOfWeek:     "monday",
		SessionCount:  2,
		BandwidthUsed: 5000000,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = evaluator.Evaluate(ctx, policy)
	}
}

func BenchmarkPolicyEvaluateParallel(b *testing.B) {
	evaluator := dsl.NewEvaluator()
	policy := &dsl.CompiledPolicy{
		Name:    "parallel-test",
		Version: "1",
		Match: dsl.CompiledMatch{
			Conditions: []dsl.CompiledMatchCondition{
				{Field: "apn", Op: "eq", Value: "internet"},
			},
		},
		Rules: dsl.CompiledRules{
			Defaults: map[string]interface{}{
				"bandwidth_down": float64(10000000),
			},
			WhenBlocks: []dsl.CompiledWhenBlock{
				{
					Condition: &dsl.CompiledCondition{
						Field: "usage",
						Op:    "gt",
						Value: int64(1073741824),
					},
					Assignments: map[string]interface{}{
						"bandwidth_down": float64(2000000),
					},
				},
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := dsl.SessionContext{
			APN:      "internet",
			Operator: "turkcell",
			RATType:  "lte",
			Usage:    500000,
		}
		for pb.Next() {
			_, _ = evaluator.Evaluate(ctx, policy)
		}
	})
}

func BenchmarkPolicyCacheLookup(b *testing.B) {
	cache := newMockPolicyCache(100)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		vid := cache.randomVersionID()
		_, _ = cache.get(vid)
	}
}

func BenchmarkPolicyCacheLookupParallel(b *testing.B) {
	cache := newMockPolicyCache(100)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			vid := cache.randomVersionID()
			_, _ = cache.get(vid)
		}
	})
}

func BenchmarkSIMJSONMarshal(b *testing.B) {
	sim := &store.SIM{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		ICCID:      "89860000000000000001",
		IMSI:       "286010000000001",
		SimType:    "physical",
		State:      "active",
		Metadata:   json.RawMessage(`{"plan":"iot-basic","tier":"standard"}`),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(sim)
		_ = data
	}
}

func BenchmarkSIMJSONUnmarshal(b *testing.B) {
	sim := &store.SIM{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		ICCID:      "89860000000000000001",
		IMSI:       "286010000000001",
		SimType:    "physical",
		State:      "active",
		Metadata:   json.RawMessage(`{"plan":"iot-basic","tier":"standard"}`),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	data, _ := json.Marshal(sim)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var s store.SIM
		_ = json.Unmarshal(data, &s)
	}
}

type mockSIMCacheEntry struct {
	imsi string
	sim  *store.SIM
}

type mockSIMCache struct {
	entries []mockSIMCacheEntry
	byIMSI  map[string]*store.SIM
}

func newMockSIMCache(n int) *mockSIMCache {
	c := &mockSIMCache{
		entries: make([]mockSIMCacheEntry, n),
		byIMSI:  make(map[string]*store.SIM, n),
	}
	for i := 0; i < n; i++ {
		imsi := fmt.Sprintf("28601%010d", i)
		sim := &store.SIM{
			ID:         uuid.New(),
			TenantID:   uuid.New(),
			OperatorID: uuid.New(),
			ICCID:      fmt.Sprintf("8986%016d", i),
			IMSI:       imsi,
			State:      "active",
			SimType:    "physical",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		c.entries[i] = mockSIMCacheEntry{imsi: imsi, sim: sim}
		c.byIMSI[imsi] = sim
	}
	return c
}

func (c *mockSIMCache) randomIMSI() string {
	return c.entries[rand.Intn(len(c.entries))].imsi
}

func (c *mockSIMCache) GetByIMSI(_ context.Context, imsi string) (*store.SIM, error) {
	if sim, ok := c.byIMSI[imsi]; ok {
		return sim, nil
	}
	return nil, store.ErrSIMNotFound
}

type mockPolicyCacheEntry struct {
	versionID uuid.UUID
	policy    *dsl.CompiledPolicy
}

type mockPolicyCache struct {
	entries []mockPolicyCacheEntry
	byID    map[uuid.UUID]*dsl.CompiledPolicy
}

func newMockPolicyCache(n int) *mockPolicyCache {
	c := &mockPolicyCache{
		entries: make([]mockPolicyCacheEntry, n),
		byID:    make(map[uuid.UUID]*dsl.CompiledPolicy, n),
	}
	for i := 0; i < n; i++ {
		vid := uuid.New()
		cp := &dsl.CompiledPolicy{
			Name:    fmt.Sprintf("policy-%d", i),
			Version: "1",
			Match: dsl.CompiledMatch{
				Conditions: []dsl.CompiledMatchCondition{
					{Field: "apn", Op: "eq", Value: "internet"},
				},
			},
			Rules: dsl.CompiledRules{
				Defaults: map[string]interface{}{
					"bandwidth_down": float64(10000000),
				},
			},
		}
		c.entries[i] = mockPolicyCacheEntry{versionID: vid, policy: cp}
		c.byID[vid] = cp
	}
	return c
}

func (c *mockPolicyCache) randomVersionID() uuid.UUID {
	return c.entries[rand.Intn(len(c.entries))].versionID
}

func (c *mockPolicyCache) get(vid uuid.UUID) (*dsl.CompiledPolicy, bool) {
	p, ok := c.byID[vid]
	return p, ok
}

var _ = zerolog.Nop
