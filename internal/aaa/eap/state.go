package eap

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	StateIdentity  SessionState = "identity"
	StateMethodNeg SessionState = "method_negotiation"
	StateSIMStart  SessionState = "sim_start"
	StateChallenge SessionState = "challenge"
	StateSuccess   SessionState = "success"
	StateFailure   SessionState = "failure"

	DefaultStateTTL = 30 * time.Second

	mskStashTTL        = 10 * time.Second
	mskStashSweepEvery = 30 * time.Second
)

type SessionState string

type AuthVectorProvider interface {
	GetSIMTriplets(ctx context.Context, imsi string) (*SIMTriplets, error)
	GetAKAQuintets(ctx context.Context, imsi string) (*AKAQuintets, error)
}

type SIMTriplets struct {
	RAND [3][16]byte
	SRES [3][4]byte
	Kc   [3][8]byte
}

type AKAQuintets struct {
	RAND [16]byte
	AUTN [16]byte
	XRES []byte
	CK   [16]byte
	IK   [16]byte
}

type SIMTypeLookupFunc func(ctx context.Context, imsi string) (string, error)

type EAPSession struct {
	ID         string       `json:"id"`
	IMSI       string       `json:"imsi"`
	State      SessionState `json:"state"`
	Method     MethodType   `json:"method"`
	Identifier uint8        `json:"identifier"`
	CreatedAt  time.Time    `json:"created_at"`
	ExpiresAt  time.Time    `json:"expires_at"`

	SIMStartData *SIMStartData     `json:"sim_start_data,omitempty"`
	SIMData      *SIMChallengeData `json:"sim_data,omitempty"`
	AKAData      *AKAChallengeData `json:"aka_data,omitempty"`
}

type SIMStartData struct {
	NonceMT         [16]byte `json:"nonce_mt"`
	SelectedVersion uint16   `json:"selected_version"`
}

type SIMChallengeData struct {
	RAND [3][16]byte `json:"rand"`
	SRES [3][4]byte  `json:"sres"`
	Kc   [3][8]byte  `json:"kc"`
	MSK  []byte      `json:"msk,omitempty"`
}

type AKAChallengeData struct {
	RAND [16]byte `json:"rand"`
	AUTN [16]byte `json:"autn"`
	XRES []byte   `json:"xres"`
	CK   [16]byte `json:"ck"`
	IK   [16]byte `json:"ik"`
	MSK  []byte   `json:"msk,omitempty"`
}

type StateStore interface {
	Save(ctx context.Context, session *EAPSession) error
	Get(ctx context.Context, id string) (*EAPSession, error)
	Delete(ctx context.Context, id string) error
	GetAndDelete(ctx context.Context, id string) (*EAPSession, error)
}

type MethodHandler interface {
	Type() MethodType
	HandleResponse(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error)
	StartChallenge(ctx context.Context, session *EAPSession, provider AuthVectorProvider) (*Packet, error)
}

type msksEntry struct {
	msk       []byte
	createdAt time.Time
}

type StateMachine struct {
	store         StateStore
	provider      AuthVectorProvider
	methods       map[MethodType]MethodHandler
	simTypeLookup SIMTypeLookupFunc
	logger        zerolog.Logger
	mu            sync.RWMutex

	msks      sync.Map
	sweepStop chan struct{}
	sweepOnce sync.Once
}

func NewStateMachine(store StateStore, provider AuthVectorProvider, logger zerolog.Logger) *StateMachine {
	sm := &StateMachine{
		store:     store,
		provider:  provider,
		methods:   make(map[MethodType]MethodHandler),
		logger:    logger.With().Str("component", "eap_state_machine").Logger(),
		sweepStop: make(chan struct{}),
	}
	go sm.sweepMSKStash()
	return sm
}

func (sm *StateMachine) sweepMSKStash() {
	ticker := time.NewTicker(mskStashSweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-sm.sweepStop:
			return
		case now := <-ticker.C:
			sm.msks.Range(func(key, value interface{}) bool {
				entry, ok := value.(msksEntry)
				if !ok {
					sm.msks.Delete(key)
					return true
				}
				if now.Sub(entry.createdAt) > mskStashTTL {
					sm.msks.Delete(key)
				}
				return true
			})
		}
	}
}

func (sm *StateMachine) Stop() {
	sm.sweepOnce.Do(func() {
		close(sm.sweepStop)
	})
}

func (sm *StateMachine) SetSIMTypeLookup(fn SIMTypeLookupFunc) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.simTypeLookup = fn
}

func (sm *StateMachine) RegisterMethod(handler MethodHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.methods[handler.Type()] = handler
	sm.logger.Info().Str("method", handler.Type().String()).Msg("EAP method registered")
}

func (sm *StateMachine) SupportedMethods() []MethodType {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	methods := make([]MethodType, 0, len(sm.methods))
	for m := range sm.methods {
		methods = append(methods, m)
	}
	return methods
}

func (sm *StateMachine) ProcessPacket(ctx context.Context, sessionID string, raw []byte) ([]byte, error) {
	pkt, err := Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decode EAP packet: %w", err)
	}

	sm.logger.Debug().
		Str("session_id", sessionID).
		Str("code", pkt.Code.String()).
		Uint8("identifier", pkt.Identifier).
		Str("type", pkt.Type.String()).
		Msg("EAP packet received")

	if pkt.Code != CodeResponse {
		return nil, fmt.Errorf("expected EAP-Response, got %s", pkt.Code)
	}

	session, err := sm.store.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get EAP session: %w", err)
	}

	if session == nil {
		session = &EAPSession{
			ID:         sessionID,
			State:      StateIdentity,
			Identifier: pkt.Identifier,
			CreatedAt:  time.Now().UTC(),
			ExpiresAt:  time.Now().UTC().Add(DefaultStateTTL),
		}
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		_ = sm.store.Delete(ctx, sessionID)
		failPkt := NewFailure(pkt.Identifier)
		return Encode(failPkt), nil
	}

	var respPkt *Packet

	switch session.State {
	case StateIdentity:
		respPkt, err = sm.handleIdentity(ctx, session, pkt)
	case StateMethodNeg:
		respPkt, err = sm.handleMethodNegotiation(ctx, session, pkt)
	case StateSIMStart:
		respPkt, err = sm.handleChallenge(ctx, session, pkt)
	case StateChallenge:
		respPkt, err = sm.handleChallenge(ctx, session, pkt)
	default:
		return nil, fmt.Errorf("unexpected EAP state: %s", session.State)
	}

	if err != nil {
		sm.logger.Error().Err(err).Str("session_id", sessionID).Msg("EAP processing error")
		_ = sm.store.Delete(ctx, sessionID)
		failPkt := NewFailure(pkt.Identifier)
		return Encode(failPkt), nil
	}

	return Encode(respPkt), nil
}

func (sm *StateMachine) handleIdentity(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error) {
	if pkt.Type != MethodIdentity {
		return NewFailure(pkt.Identifier), nil
	}

	identity := string(pkt.Data)
	session.IMSI = identity
	session.Identifier = pkt.Identifier + 1

	method := sm.selectMethod(ctx, identity)
	if method == MethodType(0) {
		return NewFailure(pkt.Identifier), nil
	}

	session.Method = method
	session.ExpiresAt = time.Now().UTC().Add(DefaultStateTTL)

	sm.mu.RLock()
	handler, ok := sm.methods[method]
	sm.mu.RUnlock()

	if !ok {
		return NewFailure(pkt.Identifier), nil
	}

	if method == MethodSIM {
		session.State = StateSIMStart
		startPkt := buildSIMStartRequest(session.Identifier)
		if err := sm.store.Save(ctx, session); err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
		sm.logger.Info().
			Str("session_id", session.ID).
			Str("imsi", session.IMSI).
			Str("method", method.String()).
			Msg("EAP-SIM Start initiated")
		return startPkt, nil
	}

	session.State = StateChallenge

	challengePkt, err := handler.StartChallenge(ctx, session, sm.provider)
	if err != nil {
		return nil, fmt.Errorf("start challenge: %w", err)
	}

	if err := sm.store.Save(ctx, session); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}

	sm.logger.Info().
		Str("session_id", session.ID).
		Str("imsi", session.IMSI).
		Str("method", method.String()).
		Msg("EAP challenge initiated")

	return challengePkt, nil
}

func (sm *StateMachine) handleMethodNegotiation(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error) {
	if pkt.Type != MethodNAK {
		return NewFailure(pkt.Identifier), nil
	}

	requestedMethods := make([]MethodType, len(pkt.Data))
	for i, b := range pkt.Data {
		requestedMethods[i] = MethodType(b)
	}

	sm.mu.RLock()
	var selectedMethod MethodType
	for _, m := range requestedMethods {
		if _, ok := sm.methods[m]; ok {
			selectedMethod = m
			break
		}
	}
	sm.mu.RUnlock()

	if selectedMethod == MethodType(0) {
		_ = sm.store.Delete(ctx, session.ID)
		return NewFailure(pkt.Identifier), nil
	}

	session.Method = selectedMethod
	session.Identifier = pkt.Identifier + 1
	session.ExpiresAt = time.Now().UTC().Add(DefaultStateTTL)

	if selectedMethod == MethodSIM {
		session.State = StateSIMStart
		startPkt := buildSIMStartRequest(session.Identifier)
		if err := sm.store.Save(ctx, session); err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
		sm.logger.Info().
			Str("session_id", session.ID).
			Str("method", selectedMethod.String()).
			Msg("EAP-SIM Start after NAK negotiation")
		return startPkt, nil
	}

	session.State = StateChallenge

	sm.mu.RLock()
	handler := sm.methods[selectedMethod]
	sm.mu.RUnlock()

	challengePkt, err := handler.StartChallenge(ctx, session, sm.provider)
	if err != nil {
		return nil, fmt.Errorf("start challenge after NAK: %w", err)
	}

	if err := sm.store.Save(ctx, session); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}

	sm.logger.Info().
		Str("session_id", session.ID).
		Str("method", selectedMethod.String()).
		Msg("method negotiated via NAK")

	return challengePkt, nil
}

func (sm *StateMachine) handleChallenge(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error) {
	sm.mu.RLock()
	handler, ok := sm.methods[session.Method]
	sm.mu.RUnlock()

	if !ok {
		_ = sm.store.Delete(ctx, session.ID)
		return NewFailure(pkt.Identifier), nil
	}

	respPkt, err := handler.HandleResponse(ctx, session, pkt)
	if err != nil {
		_ = sm.store.Delete(ctx, session.ID)
		return NewFailure(pkt.Identifier), nil
	}

	if respPkt.Code == CodeSuccess {
		session.State = StateSuccess
		sm.stashSessionMSK(session)
		_ = sm.store.Delete(ctx, session.ID)
		sm.logger.Info().
			Str("session_id", session.ID).
			Str("imsi", session.IMSI).
			Str("method", session.Method.String()).
			Msg("EAP authentication successful")
	} else if respPkt.Code == CodeFailure {
		session.State = StateFailure
		_ = sm.store.Delete(ctx, session.ID)
		sm.logger.Warn().
			Str("session_id", session.ID).
			Str("imsi", session.IMSI).
			Str("method", session.Method.String()).
			Msg("EAP authentication failed")
	} else {
		session.ExpiresAt = time.Now().UTC().Add(DefaultStateTTL)
		if err := sm.store.Save(ctx, session); err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
	}

	return respPkt, nil
}

func (sm *StateMachine) selectMethod(ctx context.Context, identity string) MethodType {
	sm.mu.RLock()
	lookupFn := sm.simTypeLookup
	sm.mu.RUnlock()

	if lookupFn != nil {
		simType, err := lookupFn(ctx, identity)
		if err == nil {
			switch simType {
			case "sim":
				sm.mu.RLock()
				_, ok := sm.methods[MethodSIM]
				sm.mu.RUnlock()
				if ok {
					return MethodSIM
				}
			case "usim", "esim", "isim":
				sm.mu.RLock()
				_, hasAKAPrime := sm.methods[MethodAKAPrime]
				_, hasAKA := sm.methods[MethodAKA]
				sm.mu.RUnlock()
				if hasAKAPrime {
					return MethodAKAPrime
				}
				if hasAKA {
					return MethodAKA
				}
			}
		} else {
			sm.logger.Warn().Err(err).Str("imsi", identity).Msg("SIM type lookup failed, falling back to priority selection")
		}
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if _, ok := sm.methods[MethodAKAPrime]; ok {
		return MethodAKAPrime
	}
	if _, ok := sm.methods[MethodAKA]; ok {
		return MethodAKA
	}
	if _, ok := sm.methods[MethodSIM]; ok {
		return MethodSIM
	}
	return MethodType(0)
}

func (sm *StateMachine) StartIdentity(sessionID string) []byte {
	pkt := NewIdentityRequest(0)
	return Encode(pkt)
}

func (sm *StateMachine) GetSessionMethod(ctx context.Context, sessionID string) (MethodType, error) {
	session, err := sm.store.Get(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if session == nil {
		return 0, nil
	}
	return session.Method, nil
}

func (sm *StateMachine) stashSessionMSK(session *EAPSession) {
	if session == nil {
		return
	}
	var msk []byte
	if session.SIMData != nil && len(session.SIMData.MSK) > 0 {
		msk = session.SIMData.MSK
	} else if session.AKAData != nil && len(session.AKAData.MSK) > 0 {
		msk = session.AKAData.MSK
	}
	if msk == nil {
		return
	}
	stashed := make([]byte, len(msk))
	copy(stashed, msk)
	sm.msks.Store(session.ID, msksEntry{msk: stashed, createdAt: time.Now()})
}

func (sm *StateMachine) ConsumeSessionMSK(sessionID string) ([]byte, bool) {
	v, ok := sm.msks.LoadAndDelete(sessionID)
	if !ok {
		return nil, false
	}
	entry, ok := v.(msksEntry)
	if !ok {
		return nil, false
	}
	if time.Since(entry.createdAt) > mskStashTTL {
		return nil, false
	}
	return entry.msk, true
}

type MemoryStateStore struct {
	mu       sync.RWMutex
	sessions map[string][]byte
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{
		sessions: make(map[string][]byte),
	}
}

func (s *MemoryStateStore) Save(_ context.Context, session *EAPSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	s.sessions[session.ID] = data
	return nil
}

func (s *MemoryStateStore) Get(_ context.Context, id string) (*EAPSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.sessions[id]
	if !ok {
		return nil, nil
	}
	var session EAPSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *MemoryStateStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *MemoryStateStore) GetAndDelete(_ context.Context, id string) (*EAPSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.sessions[id]
	if !ok {
		return nil, nil
	}
	delete(s.sessions, id)
	var session EAPSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}
