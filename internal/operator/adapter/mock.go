package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type MockConfig struct {
	LatencyMs    int      `json:"latency_ms"`
	FailRate     float64  `json:"fail_rate"`
	SuccessRate  *float64 `json:"success_rate"`
	HealthyAfter int      `json:"healthy_after"`
	ErrorType    string   `json:"error_type"`
	TimeoutMs    int      `json:"timeout_ms"`
}

type MockAdapter struct {
	mu        sync.Mutex
	config    MockConfig
	callCount int
	rng       *rand.Rand
}

func NewMockAdapter(raw json.RawMessage) (*MockAdapter, error) {
	var cfg MockConfig
	if raw != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			cfg = MockConfig{LatencyMs: 10}
		}
	}
	if cfg.LatencyMs == 0 {
		cfg.LatencyMs = 10
	}
	if cfg.SuccessRate == nil && cfg.FailRate > 0 {
		rate := (1 - cfg.FailRate) * 100
		cfg.SuccessRate = &rate
	}
	if cfg.SuccessRate == nil {
		rate := float64(100)
		cfg.SuccessRate = &rate
	}
	return &MockAdapter{
		config: cfg,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

func (m *MockAdapter) simulateLatency(ctx context.Context) (time.Time, error) {
	start := time.Now()
	latency := time.Duration(m.config.LatencyMs) * time.Millisecond
	select {
	case <-time.After(latency):
		return start, nil
	case <-ctx.Done():
		return start, ctx.Err()
	}
}

func (m *MockAdapter) shouldSucceed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rate := float64(100)
	if m.config.SuccessRate != nil {
		rate = *m.config.SuccessRate
	}
	if rate >= 100 {
		return true
	}
	if rate <= 0 {
		return false
	}
	return m.rng.Float64()*100 < rate
}

func (m *MockAdapter) incrementCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	return m.callCount
}

func (m *MockAdapter) HealthCheck(ctx context.Context) HealthResult {
	count := m.incrementCallCount()

	latency := time.Duration(m.config.LatencyMs) * time.Millisecond
	start := time.Now()

	select {
	case <-time.After(latency):
	case <-ctx.Done():
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     "context cancelled",
		}
	}

	m.mu.Lock()
	healthyAfter := m.config.HealthyAfter
	failRate := m.config.FailRate
	m.mu.Unlock()

	if healthyAfter > 0 && count <= healthyAfter {
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     "simulated startup failure",
		}
	}

	if failRate > 0 {
		bucket := int(1.0 / failRate)
		if bucket > 0 && count%bucket == 0 {
			return HealthResult{
				Success:   false,
				LatencyMs: int(time.Since(start).Milliseconds()),
				Error:     "simulated random failure",
			}
		}
	}

	return HealthResult{
		Success:   true,
		LatencyMs: int(time.Since(start).Milliseconds()),
	}
}

func (m *MockAdapter) ForwardAuth(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	start, err := m.simulateLatency(ctx)
	if err != nil {
		return nil, ErrAdapterTimeout
	}

	if m.shouldSucceed() {
		return &AuthResponse{
			Code:           AuthAccept,
			FramedIP:       "10.0.0.1",
			SessionTimeout: 3600,
			IdleTimeout:    300,
			FilterID:       "default-policy",
			Attributes: map[string]interface{}{
				"latency_ms": int(time.Since(start).Milliseconds()),
			},
		}, nil
	}

	return &AuthResponse{
		Code: AuthReject,
		Attributes: map[string]interface{}{
			"latency_ms":   int(time.Since(start).Milliseconds()),
			"reject_reason": m.config.ErrorType,
		},
	}, nil
}

func (m *MockAdapter) ForwardAcct(ctx context.Context, req AcctRequest) error {
	_, err := m.simulateLatency(ctx)
	if err != nil {
		return ErrAdapterTimeout
	}

	if !m.shouldSucceed() {
		return &AdapterError{
			ProtocolType: "mock",
			Err:          errSimulatedFailure,
		}
	}

	return nil
}

func (m *MockAdapter) SendCoA(ctx context.Context, req CoARequest) error {
	_, err := m.simulateLatency(ctx)
	if err != nil {
		return ErrAdapterTimeout
	}

	if !m.shouldSucceed() {
		return &AdapterError{
			ProtocolType: "mock",
			Err:          errSimulatedFailure,
		}
	}

	return nil
}

func (m *MockAdapter) SendDM(ctx context.Context, req DMRequest) error {
	_, err := m.simulateLatency(ctx)
	if err != nil {
		return ErrAdapterTimeout
	}

	if !m.shouldSucceed() {
		return &AdapterError{
			ProtocolType: "mock",
			Err:          errSimulatedFailure,
		}
	}

	return nil
}

func (m *MockAdapter) Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticateResponse, error) {
	_, err := m.simulateLatency(ctx)
	if err != nil {
		return nil, ErrAdapterTimeout
	}

	if m.shouldSucceed() {
		m.mu.Lock()
		count := m.callCount
		m.mu.Unlock()
		return &AuthenticateResponse{
			Success:   true,
			Code:      AuthAccept,
			SessionID: fmt.Sprintf("mock-session-%s-%d", req.IMSI, count),
			Attributes: map[string]interface{}{
				"apn":      req.APN,
				"rat_type": req.RATType,
			},
		}, nil
	}

	return &AuthenticateResponse{
		Success: false,
		Code:    AuthReject,
		Attributes: map[string]interface{}{
			"reject_reason": m.config.ErrorType,
		},
	}, nil
}

func (m *MockAdapter) AccountingUpdate(ctx context.Context, req AccountingUpdateRequest) error {
	_, err := m.simulateLatency(ctx)
	if err != nil {
		return ErrAdapterTimeout
	}

	if !m.shouldSucceed() {
		return &AdapterError{
			ProtocolType: "mock",
			Err:          errSimulatedFailure,
		}
	}

	return nil
}

func (m *MockAdapter) FetchAuthVectors(ctx context.Context, imsi string, count int) ([]AuthVector, error) {
	_, err := m.simulateLatency(ctx)
	if err != nil {
		return nil, ErrAdapterTimeout
	}

	if count <= 0 {
		count = 1
	}

	vectors := make([]AuthVector, count)
	for i := 0; i < count; i++ {
		if i%2 == 0 {
			vectors[i] = mockGenerateTriplet(imsi, i)
		} else {
			vectors[i] = mockGenerateQuintet(imsi, i)
		}
	}

	return vectors, nil
}

func mockDeterministicBytes(seed []byte, index int, length int) []byte {
	input := make([]byte, len(seed)+1)
	copy(input, seed)
	input[len(seed)] = byte(index)
	h := sha256.Sum256(input)
	if length > 32 {
		length = 32
	}
	out := make([]byte, length)
	copy(out, h[:length])
	return out
}

func mockGenerateTriplet(imsi string, vectorIndex int) AuthVector {
	seed := sha256.Sum256([]byte("triplet-seed:" + imsi))
	offset := vectorIndex * 10
	return AuthVector{
		Type: VectorTypeTriplet,
		RAND: mockDeterministicBytes(seed[:], offset, 16),
		SRES: mockDeterministicBytes(seed[:], offset+1, 4),
		Kc:   mockDeterministicBytes(seed[:], offset+2, 8),
	}
}

func mockGenerateQuintet(imsi string, vectorIndex int) AuthVector {
	seed := sha256.Sum256([]byte("quintet-seed:" + imsi))
	offset := vectorIndex * 10
	return AuthVector{
		Type: VectorTypeQuintet,
		RAND: mockDeterministicBytes(seed[:], offset, 16),
		AUTN: mockDeterministicBytes(seed[:], offset+1, 16),
		XRES: mockDeterministicBytes(seed[:], offset+2, 8),
		CK:   mockDeterministicBytes(seed[:], offset+3, 16),
		IK:   mockDeterministicBytes(seed[:], offset+4, 16),
	}
}

func (m *MockAdapter) Type() string {
	return "mock"
}

var errSimulatedFailure = &simulatedError{msg: "simulated failure"}

type simulatedError struct {
	msg string
}

func (e *simulatedError) Error() string {
	return e.msg
}
