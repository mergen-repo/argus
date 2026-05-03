package diameter

import (
	"fmt"
	"sync"
)

type SessionState int

const (
	SessionStateIdle SessionState = iota
	SessionStateOpen
	SessionStatePending
	SessionStateClosed
)

func (s SessionState) String() string {
	switch s {
	case SessionStateIdle:
		return "idle"
	case SessionStateOpen:
		return "open"
	case SessionStatePending:
		return "pending"
	case SessionStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

type DiameterSession struct {
	mu         sync.Mutex
	SessionID  string
	State      SessionState
	PeerHost   string
	AppID      uint32
	IMSI       string
	InternalID string
}

func (ds *DiameterSession) GetState() SessionState {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.State
}

func (ds *DiameterSession) Transition(to SessionState) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !isValidTransition(ds.State, to) {
		return fmt.Errorf("invalid session state transition: %s -> %s", ds.State, to)
	}
	ds.State = to
	return nil
}

func isValidTransition(from, to SessionState) bool {
	switch from {
	case SessionStateIdle:
		return to == SessionStateOpen || to == SessionStateClosed
	case SessionStateOpen:
		return to == SessionStatePending || to == SessionStateClosed
	case SessionStatePending:
		return to == SessionStateOpen || to == SessionStateClosed
	case SessionStateClosed:
		return false
	}
	return false
}

type SessionStateMap struct {
	mu       sync.RWMutex
	sessions map[string]*DiameterSession
}

func NewSessionStateMap() *SessionStateMap {
	return &SessionStateMap{
		sessions: make(map[string]*DiameterSession),
	}
}

func (m *SessionStateMap) Create(sessionID, peerHost string, appID uint32, imsi string) *DiameterSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	ds := &DiameterSession{
		SessionID: sessionID,
		State:     SessionStateIdle,
		PeerHost:  peerHost,
		AppID:     appID,
		IMSI:      imsi,
	}
	m.sessions[sessionID] = ds
	return ds
}

func (m *SessionStateMap) Get(sessionID string) *DiameterSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionID]
}

func (m *SessionStateMap) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

func (m *SessionStateMap) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *SessionStateMap) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, ds := range m.sessions {
		if ds.GetState() == SessionStateOpen || ds.GetState() == SessionStatePending {
			count++
		}
	}
	return count
}
