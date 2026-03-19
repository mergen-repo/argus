package eap

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func testLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

func TestDecodeSuccess(t *testing.T) {
	raw := []byte{3, 42, 0, 4}
	pkt, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if pkt.Code != CodeSuccess {
		t.Errorf("Code = %d, want %d", pkt.Code, CodeSuccess)
	}
	if pkt.Identifier != 42 {
		t.Errorf("Identifier = %d, want 42", pkt.Identifier)
	}
}

func TestDecodeFailure(t *testing.T) {
	raw := []byte{4, 1, 0, 4}
	pkt, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if pkt.Code != CodeFailure {
		t.Errorf("Code = %d, want %d", pkt.Code, CodeFailure)
	}
}

func TestDecodeIdentityResponse(t *testing.T) {
	identity := "286010123456789"
	length := uint16(5 + len(identity))
	raw := make([]byte, length)
	raw[0] = byte(CodeResponse)
	raw[1] = 1
	raw[2] = byte(length >> 8)
	raw[3] = byte(length)
	raw[4] = byte(MethodIdentity)
	copy(raw[5:], identity)

	pkt, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if pkt.Code != CodeResponse {
		t.Errorf("Code = %d, want %d", pkt.Code, CodeResponse)
	}
	if pkt.Type != MethodIdentity {
		t.Errorf("Type = %d, want %d", pkt.Type, MethodIdentity)
	}
	if string(pkt.Data) != identity {
		t.Errorf("Data = %q, want %q", string(pkt.Data), identity)
	}
}

func TestDecodeTooShort(t *testing.T) {
	_, err := Decode([]byte{1, 2})
	if err != ErrPacketTooShort {
		t.Errorf("expected ErrPacketTooShort, got %v", err)
	}
}

func TestDecodeLengthMismatch(t *testing.T) {
	raw := []byte{1, 1, 0, 20, byte(MethodIdentity)}
	_, err := Decode(raw)
	if err != ErrLengthMismatch {
		t.Errorf("expected ErrLengthMismatch, got %v", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	original := &Packet{
		Code:       CodeRequest,
		Identifier: 7,
		Type:       MethodSIM,
		Data:       []byte{1, 2, 3, 4, 5},
	}

	encoded := Encode(original)
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded.Code != original.Code {
		t.Errorf("Code = %d, want %d", decoded.Code, original.Code)
	}
	if decoded.Identifier != original.Identifier {
		t.Errorf("Identifier = %d, want %d", decoded.Identifier, original.Identifier)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type = %d, want %d", decoded.Type, original.Type)
	}
	if !bytes.Equal(decoded.Data, original.Data) {
		t.Errorf("Data mismatch")
	}
}

func TestEncodeSuccess(t *testing.T) {
	pkt := NewSuccess(10)
	encoded := Encode(pkt)
	if len(encoded) != 4 {
		t.Fatalf("encoded length = %d, want 4", len(encoded))
	}
	if encoded[0] != byte(CodeSuccess) {
		t.Errorf("code = %d, want %d", encoded[0], CodeSuccess)
	}
	if encoded[1] != 10 {
		t.Errorf("identifier = %d, want 10", encoded[1])
	}
}

func TestEncodeFailure(t *testing.T) {
	pkt := NewFailure(20)
	encoded := Encode(pkt)
	if len(encoded) != 4 {
		t.Fatalf("encoded length = %d, want 4", len(encoded))
	}
	if encoded[0] != byte(CodeFailure) {
		t.Errorf("code = %d, want %d", encoded[0], CodeFailure)
	}
}

func TestNewNAK(t *testing.T) {
	supported := []MethodType{MethodSIM, MethodAKA}
	pkt := NewNAK(5, supported)
	if pkt.Code != CodeResponse {
		t.Errorf("Code = %d, want %d", pkt.Code, CodeResponse)
	}
	if pkt.Type != MethodNAK {
		t.Errorf("Type = %d, want %d", pkt.Type, MethodNAK)
	}
	if len(pkt.Data) != 2 {
		t.Fatalf("Data length = %d, want 2", len(pkt.Data))
	}
	if pkt.Data[0] != byte(MethodSIM) {
		t.Errorf("Data[0] = %d, want %d", pkt.Data[0], MethodSIM)
	}
	if pkt.Data[1] != byte(MethodAKA) {
		t.Errorf("Data[1] = %d, want %d", pkt.Data[1], MethodAKA)
	}
}

func TestNewIdentityRequest(t *testing.T) {
	pkt := NewIdentityRequest(0)
	if pkt.Code != CodeRequest {
		t.Errorf("Code = %d, want %d", pkt.Code, CodeRequest)
	}
	if pkt.Type != MethodIdentity {
		t.Errorf("Type = %d, want %d", pkt.Type, MethodIdentity)
	}
}

func TestNewIdentityResponse(t *testing.T) {
	pkt := NewIdentityResponse(1, "286010123456789")
	if pkt.Code != CodeResponse {
		t.Errorf("Code = %d, want %d", pkt.Code, CodeResponse)
	}
	if pkt.Type != MethodIdentity {
		t.Errorf("Type = %d, want %d", pkt.Type, MethodIdentity)
	}
	if string(pkt.Data) != "286010123456789" {
		t.Errorf("Data = %q, want 286010123456789", string(pkt.Data))
	}
}

func TestMethodTypeString(t *testing.T) {
	tests := []struct {
		m    MethodType
		want string
	}{
		{MethodIdentity, "Identity"},
		{MethodNAK, "NAK"},
		{MethodSIM, "EAP-SIM"},
		{MethodAKA, "EAP-AKA"},
		{MethodAKAPrime, "EAP-AKA'"},
		{MethodType(99), "Method(99)"},
	}
	for _, tt := range tests {
		got := tt.m.String()
		if got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.m, got, tt.want)
		}
	}
}

func TestCodeString(t *testing.T) {
	tests := []struct {
		c    Code
		want string
	}{
		{CodeRequest, "Request"},
		{CodeResponse, "Response"},
		{CodeSuccess, "Success"},
		{CodeFailure, "Failure"},
		{Code(99), "Code(99)"},
	}
	for _, tt := range tests {
		got := tt.c.String()
		if got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.c, got, tt.want)
		}
	}
}

func TestMemoryStateStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStateStore()

	session := &EAPSession{
		ID:        "test-123",
		IMSI:      "286010123456789",
		State:     StateIdentity,
		Method:    MethodSIM,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(30 * time.Second),
	}

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := store.Get(ctx, "test-123")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.IMSI != session.IMSI {
		t.Errorf("IMSI = %q, want %q", got.IMSI, session.IMSI)
	}
	if got.State != StateIdentity {
		t.Errorf("State = %q, want %q", got.State, StateIdentity)
	}

	if err := store.Delete(ctx, "test-123"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	got, err = store.Get(ctx, "test-123")
	if err != nil {
		t.Fatalf("Get after delete error: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestStateMachineRegistration(t *testing.T) {
	store := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(store, provider, testLogger())

	sm.RegisterMethod(NewSIMHandler())
	sm.RegisterMethod(NewAKAHandler())
	sm.RegisterMethod(NewAKAPrimeHandler())

	methods := sm.SupportedMethods()
	if len(methods) != 3 {
		t.Errorf("SupportedMethods count = %d, want 3", len(methods))
	}

	found := map[MethodType]bool{}
	for _, m := range methods {
		found[m] = true
	}
	if !found[MethodSIM] {
		t.Error("MethodSIM not found in supported methods")
	}
	if !found[MethodAKA] {
		t.Error("MethodAKA not found in supported methods")
	}
	if !found[MethodAKAPrime] {
		t.Error("MethodAKAPrime not found in supported methods")
	}
}

func TestStateMachineStartIdentity(t *testing.T) {
	store := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(store, provider, testLogger())
	sm.RegisterMethod(NewSIMHandler())

	raw := sm.StartIdentity("sess-1")
	pkt, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if pkt.Code != CodeRequest {
		t.Errorf("Code = %d, want %d", pkt.Code, CodeRequest)
	}
	if pkt.Type != MethodIdentity {
		t.Errorf("Type = %d, want %d", pkt.Type, MethodIdentity)
	}
}

func TestStateMachineEAPSIMFlow(t *testing.T) {
	stateStore := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(stateStore, provider, testLogger())
	sm.RegisterMethod(NewSIMHandler())

	ctx := context.Background()
	sessionID := "sim-flow-1"

	identityResp := NewIdentityResponse(0, "286010123456789")
	identityRaw := Encode(identityResp)

	challengeRaw, err := sm.ProcessPacket(ctx, sessionID, identityRaw)
	if err != nil {
		t.Fatalf("ProcessPacket identity error: %v", err)
	}

	challengePkt, err := Decode(challengeRaw)
	if err != nil {
		t.Fatalf("Decode challenge error: %v", err)
	}
	if challengePkt.Code != CodeRequest {
		t.Errorf("challenge Code = %d, want %d", challengePkt.Code, CodeRequest)
	}
	if challengePkt.Type != MethodSIM {
		t.Errorf("challenge Type = %d, want %d", challengePkt.Type, MethodSIM)
	}

	session, _ := stateStore.Get(ctx, sessionID)
	if session == nil {
		t.Fatal("session not found in state store after identity")
	}
	if session.State != StateChallenge {
		t.Errorf("session state = %q, want %q", session.State, StateChallenge)
	}
	if session.Method != MethodSIM {
		t.Errorf("session method = %d, want %d", session.Method, MethodSIM)
	}

	var combinedSRES []byte
	for i := 0; i < 3; i++ {
		combinedSRES = append(combinedSRES, session.SIMData.SRES[i][:]...)
	}

	simChallengeData := buildSIMChallengeResponseData(combinedSRES)
	simResp := NewResponse(challengePkt.Identifier, MethodSIM, simChallengeData)
	resultRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(simResp))
	if err != nil {
		t.Fatalf("ProcessPacket challenge response error: %v", err)
	}

	resultPkt, _ := Decode(resultRaw)
	if resultPkt.Code != CodeSuccess {
		t.Errorf("result Code = %d, want %d (Success)", resultPkt.Code, CodeSuccess)
	}
}

func TestStateMachineEAPAKAFlow(t *testing.T) {
	stateStore := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(stateStore, provider, testLogger())
	sm.RegisterMethod(NewAKAHandler())

	ctx := context.Background()
	sessionID := "aka-flow-1"

	identityResp := NewIdentityResponse(0, "286010123456789")
	challengeRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(identityResp))
	if err != nil {
		t.Fatalf("ProcessPacket identity error: %v", err)
	}

	challengePkt, err := Decode(challengeRaw)
	if err != nil {
		t.Fatalf("Decode challenge error: %v", err)
	}
	if challengePkt.Type != MethodAKA {
		t.Errorf("challenge Type = %d, want %d", challengePkt.Type, MethodAKA)
	}

	session, _ := stateStore.Get(ctx, sessionID)
	if session == nil {
		t.Fatal("session not found")
	}

	resData := buildAKAChallengeResponseData(session.AKAData.XRES)
	akaResp := NewResponse(challengePkt.Identifier, MethodAKA, resData)
	resultRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(akaResp))
	if err != nil {
		t.Fatalf("ProcessPacket AKA challenge error: %v", err)
	}

	resultPkt, _ := Decode(resultRaw)
	if resultPkt.Code != CodeSuccess {
		t.Errorf("result Code = %d, want %d (Success)", resultPkt.Code, CodeSuccess)
	}
}

func TestStateMachineEAPAKAPrimeFlow(t *testing.T) {
	stateStore := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(stateStore, provider, testLogger())
	sm.RegisterMethod(NewAKAPrimeHandler())

	ctx := context.Background()
	sessionID := "akaprime-flow-1"

	identityResp := NewIdentityResponse(0, "286010123456789")
	challengeRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(identityResp))
	if err != nil {
		t.Fatalf("ProcessPacket identity error: %v", err)
	}

	challengePkt, err := Decode(challengeRaw)
	if err != nil {
		t.Fatalf("Decode challenge error: %v", err)
	}
	if challengePkt.Type != MethodAKAPrime {
		t.Errorf("challenge Type = %d, want %d", challengePkt.Type, MethodAKAPrime)
	}

	session, _ := stateStore.Get(ctx, sessionID)
	if session == nil {
		t.Fatal("session not found")
	}

	resData := buildAKAChallengeResponseData(session.AKAData.XRES)
	akaResp := NewResponse(challengePkt.Identifier, MethodAKAPrime, resData)
	resultRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(akaResp))
	if err != nil {
		t.Fatalf("ProcessPacket AKA' challenge error: %v", err)
	}

	resultPkt, _ := Decode(resultRaw)
	if resultPkt.Code != CodeSuccess {
		t.Errorf("result Code = %d, want %d (Success)", resultPkt.Code, CodeSuccess)
	}
}

func TestStateMachineAuthFailure(t *testing.T) {
	stateStore := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(stateStore, provider, testLogger())
	sm.RegisterMethod(NewAKAHandler())

	ctx := context.Background()
	sessionID := "fail-flow-1"

	identityResp := NewIdentityResponse(0, "286010123456789")
	challengeRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(identityResp))
	if err != nil {
		t.Fatalf("ProcessPacket identity error: %v", err)
	}

	challengePkt, _ := Decode(challengeRaw)

	wrongRES := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	resData := buildAKAChallengeResponseData(wrongRES)
	akaResp := NewResponse(challengePkt.Identifier, MethodAKA, resData)
	resultRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(akaResp))
	if err != nil {
		t.Fatalf("ProcessPacket error: %v", err)
	}

	resultPkt, _ := Decode(resultRaw)
	if resultPkt.Code != CodeFailure {
		t.Errorf("result Code = %d, want %d (Failure)", resultPkt.Code, CodeFailure)
	}
}

func TestStateMachineSessionTimeout(t *testing.T) {
	stateStore := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(stateStore, provider, testLogger())
	sm.RegisterMethod(NewSIMHandler())

	ctx := context.Background()
	sessionID := "timeout-flow-1"

	identityResp := NewIdentityResponse(0, "286010123456789")
	_, err := sm.ProcessPacket(ctx, sessionID, Encode(identityResp))
	if err != nil {
		t.Fatalf("ProcessPacket identity error: %v", err)
	}

	session, _ := stateStore.Get(ctx, sessionID)
	if session != nil {
		session.ExpiresAt = time.Now().UTC().Add(-1 * time.Second)
		_ = stateStore.Save(ctx, session)
	}

	simResp := NewResponse(1, MethodSIM, []byte{SimSubtypeChallenge, 0, 0})
	resultRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(simResp))
	if err != nil {
		t.Fatalf("ProcessPacket error: %v", err)
	}

	resultPkt, _ := Decode(resultRaw)
	if resultPkt.Code != CodeFailure {
		t.Errorf("result Code = %d, want %d (Failure) for expired session", resultPkt.Code, CodeFailure)
	}
}

func TestStateMachineNAKNegotiation(t *testing.T) {
	stateStore := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(stateStore, provider, testLogger())
	sm.RegisterMethod(NewSIMHandler())
	sm.RegisterMethod(NewAKAHandler())

	ctx := context.Background()
	sessionID := "nak-flow-1"

	identityResp := NewIdentityResponse(0, "286010123456789")
	_, err := sm.ProcessPacket(ctx, sessionID, Encode(identityResp))
	if err != nil {
		t.Fatalf("ProcessPacket identity error: %v", err)
	}

	session, _ := stateStore.Get(ctx, sessionID)
	if session != nil {
		session.State = StateMethodNeg
		_ = stateStore.Save(ctx, session)
	}

	nakPkt := NewNAK(1, []MethodType{MethodSIM})
	resultRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(nakPkt))
	if err != nil {
		t.Fatalf("ProcessPacket NAK error: %v", err)
	}

	resultPkt, _ := Decode(resultRaw)
	if resultPkt.Code == CodeFailure {
		t.Error("NAK negotiation should not result in immediate failure when method is supported")
	}
}

func TestStateMachineUnknownMethodNAK(t *testing.T) {
	stateStore := NewMemoryStateStore()
	provider := NewMockVectorProvider()
	sm := NewStateMachine(stateStore, provider, testLogger())
	sm.RegisterMethod(NewSIMHandler())

	ctx := context.Background()
	sessionID := "unknown-nak-1"

	session := &EAPSession{
		ID:        sessionID,
		IMSI:      "286010123456789",
		State:     StateMethodNeg,
		Method:    MethodSIM,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(30 * time.Second),
	}
	_ = stateStore.Save(ctx, session)

	nakPkt := NewNAK(1, []MethodType{MethodType(99)})
	resultRaw, err := sm.ProcessPacket(ctx, sessionID, Encode(nakPkt))
	if err != nil {
		t.Fatalf("ProcessPacket error: %v", err)
	}

	resultPkt, _ := Decode(resultRaw)
	if resultPkt.Code != CodeFailure {
		t.Errorf("result Code = %d, want %d (Failure) for unknown method NAK", resultPkt.Code, CodeFailure)
	}
}

func TestMockVectorProviderDeterministic(t *testing.T) {
	provider := NewMockVectorProvider()
	ctx := context.Background()

	triplets1, _ := provider.GetSIMTriplets(ctx, "286010123456789")
	triplets2, _ := provider.GetSIMTriplets(ctx, "286010123456789")

	if triplets1.RAND != triplets2.RAND {
		t.Error("SIM triplets should be deterministic for same IMSI")
	}

	quintets1, _ := provider.GetAKAQuintets(ctx, "286010123456789")
	quintets2, _ := provider.GetAKAQuintets(ctx, "286010123456789")

	if quintets1.RAND != quintets2.RAND {
		t.Error("AKA quintets should be deterministic for same IMSI")
	}
}

func TestMockVectorProviderUniqueness(t *testing.T) {
	provider := NewMockVectorProvider()
	ctx := context.Background()

	triplets1, _ := provider.GetSIMTriplets(ctx, "286010123456789")
	triplets2, _ := provider.GetSIMTriplets(ctx, "286010987654321")

	if triplets1.RAND == triplets2.RAND {
		t.Error("different IMSIs should produce different triplets")
	}
}

func TestSIMHandlerType(t *testing.T) {
	h := NewSIMHandler()
	if h.Type() != MethodSIM {
		t.Errorf("SIMHandler.Type() = %d, want %d", h.Type(), MethodSIM)
	}
}

func TestAKAHandlerType(t *testing.T) {
	h := NewAKAHandler()
	if h.Type() != MethodAKA {
		t.Errorf("AKAHandler.Type() = %d, want %d", h.Type(), MethodAKA)
	}
}

func TestAKAPrimeHandlerType(t *testing.T) {
	h := NewAKAPrimeHandler()
	if h.Type() != MethodAKAPrime {
		t.Errorf("AKAPrimeHandler.Type() = %d, want %d", h.Type(), MethodAKAPrime)
	}
}

func TestEncodeSIMVersionList(t *testing.T) {
	versions := []uint16{1, 2}
	encoded := EncodeSIMVersionList(versions)
	if len(encoded) != 4 {
		t.Fatalf("encoded length = %d, want 4", len(encoded))
	}
	if encoded[0] != 0 || encoded[1] != 1 {
		t.Errorf("first version = %d, want 1", int(encoded[0])<<8|int(encoded[1]))
	}
}

func buildSIMChallengeResponseData(combinedSRES []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(SimSubtypeChallenge)
	buf.WriteByte(0)
	buf.WriteByte(0)

	macData := make([]byte, 16)
	copy(macData, combinedSRES)
	buf.WriteByte(SimATMAC)
	buf.WriteByte(5)
	buf.WriteByte(0)
	buf.WriteByte(0)
	buf.Write(macData)

	return buf.Bytes()
}

func buildAKAChallengeResponseData(xres []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(AKASubtypeChallenge)
	buf.WriteByte(0)
	buf.WriteByte(0)

	resLenBits := len(xres) * 8
	resPayload := make([]byte, 2+len(xres))
	resPayload[0] = byte(resLenBits >> 8)
	resPayload[1] = byte(resLenBits)
	copy(resPayload[2:], xres)

	totalLen := 4 + len(resPayload)
	padding := (4 - totalLen%4) % 4
	attrLen := uint8((totalLen + padding) / 4)

	buf.WriteByte(AKAATRes)
	buf.WriteByte(attrLen)
	buf.WriteByte(0)
	buf.WriteByte(0)
	buf.Write(resPayload)
	for i := 0; i < padding; i++ {
		buf.WriteByte(0)
	}

	return buf.Bytes()
}
