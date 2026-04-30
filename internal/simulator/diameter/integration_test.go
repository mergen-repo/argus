//go:build integration

package diameter

import (
	"context"
	"net"
	"testing"
	"time"

	argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/radius"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// stubSIMResolver satisfies argusdiameter.SIMResolver with an in-memory map.
type stubSIMResolver struct {
	sims map[string]*store.SIM
}

func (r *stubSIMResolver) GetByIMSI(_ context.Context, imsi string) (*store.SIM, error) {
	sim, ok := r.sims[imsi]
	if !ok {
		return nil, store.ErrSIMNotFound
	}
	return sim, nil
}

// startArgusServer boots an in-process argusdiameter.Server on an ephemeral
// port with nil SessionMgr and EventBus (stateMap-only mode).
//
// Because Server.Start() binds ":port" (all-interfaces), we use the probe+retry
// pattern: bind :0, read the OS-assigned port, close, then immediately pass
// that port to Server.Start(). A small TOCTOU window exists; we retry up to 5
// times to handle the rare collision.
func startArgusServer(t *testing.T, resolver argusdiameter.SIMResolver) (*argusdiameter.Server, int) {
	t.Helper()

	for attempt := 0; attempt < 5; attempt++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("startArgusServer: probe listen: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()

		srv := argusdiameter.NewServer(
			argusdiameter.ServerConfig{
				Port:             port,
				OriginHost:       "argus.integration.test",
				OriginRealm:      "argus.local",
				VendorID:         99999,
				WatchdogInterval: 30 * time.Second,
			},
			argusdiameter.ServerDeps{
				SessionMgr:  nil,
				EventBus:    nil,
				SIMResolver: resolver,
				Logger:      zerolog.Nop(),
			},
		)

		if err := srv.Start(); err == nil {
			t.Cleanup(srv.Stop)
			return srv, port
		}
	}

	t.Fatal("startArgusServer: could not bind after 5 attempts")
	return nil, 0
}

// TestSimulator_AgainstArgusDiameter is a single end-to-end scenario:
//
//  1. Boot an in-process Argus Diameter server with a stub SIMResolver.
//  2. Connect a simulator Client (Gx + Gy) to that server.
//  3. OpenSession → stateMap ActiveCount == 1.
//  4. UpdateGy × 2 → each returns success; stateMap entry remains Open.
//  5. CloseSession → stateMap ActiveCount == 0.
func TestSimulator_AgainstArgusDiameter(t *testing.T) {
	const knownIMSI = "286010000000001"

	resolver := &stubSIMResolver{
		sims: map[string]*store.SIM{
			knownIMSI: {
				ID:         uuid.New(),
				TenantID:   uuid.New(),
				OperatorID: uuid.New(),
				IMSI:       knownIMSI,
				State:      "active",
			},
		},
	}

	srv, port := startArgusServer(t, resolver)

	opCfg := config.OperatorConfig{
		Code:          "integration-op",
		NASIdentifier: "test-nas",
		NASIP:         "127.0.0.1",
		Diameter: &config.OperatorDiameterConfig{
			Enabled:      true,
			OriginHost:   "sim-integration.sim.argus.test",
			Applications: []string{"gx", "gy"},
		},
	}
	defaults := config.DiameterDefaults{
		Host:                "127.0.0.1",
		Port:                port,
		OriginRealm:         "sim.argus.test",
		DestinationRealm:    "argus.local",
		WatchdogInterval:    30 * time.Second,
		ConnectTimeout:      5 * time.Second,
		RequestTimeout:      5 * time.Second,
		ReconnectBackoffMin: 100 * time.Millisecond,
		ReconnectBackoffMax: time.Second,
	}

	client := New(opCfg, defaults, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	ready := client.Start(ctx)
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not reach Open within 5s")
	}

	if client.peer.State() != PeerStateOpen {
		t.Fatalf("peer not open after Start; state=%s", client.peer.State())
	}

	msisdn := "905001234567"
	sc := &radius.SessionContext{
		SIM: discovery.SIM{
			IMSI:   knownIMSI,
			MSISDN: &msisdn,
		},
		NASIP:         "127.0.0.1",
		NASIdentifier: "test-nas",
		AcctSessionID: "integ-sess-001",
		FramedIP:      net.ParseIP("10.0.0.42"),
		StartedAt:     time.Now(),
	}

	// --- OpenSession ---
	if err := client.OpenSession(ctx, sc); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	count, err := srv.ActiveSessionCount(ctx)
	if err != nil {
		t.Fatalf("ActiveSessionCount after open: %v", err)
	}
	// Both Gx and Gy CCR-I write to the same stateMap key (AcctSessionID),
	// so count is 1 (last writer — Gy — wins).
	if count != 1 {
		t.Errorf("ActiveSessionCount after OpenSession = %d, want 1", count)
	}

	// --- UpdateGy × 2 ---
	for i := 0; i < 2; i++ {
		if err := client.UpdateGy(ctx, sc, 1024*(uint64(i)+1), 512*(uint64(i)+1), uint32(30*(i+1))); err != nil {
			t.Fatalf("UpdateGy[%d]: %v", i, err)
		}
		// After each update the Gy handler defers Pending→Open, so ActiveCount stays 1.
		count, err = srv.ActiveSessionCount(ctx)
		if err != nil {
			t.Fatalf("ActiveSessionCount after UpdateGy[%d]: %v", i, err)
		}
		if count != 1 {
			t.Errorf("ActiveSessionCount after UpdateGy[%d] = %d, want 1", i, count)
		}
	}

	// --- CloseSession ---
	if err := client.CloseSession(ctx, sc); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	// After Gx CCR-T and Gy CCR-T the stateMap entries are deleted.
	// Both CCR-T calls are synchronous (peer.Send blocks until CCA arrives,
	// and the defer-delete fires before handleCCR calls sendMessage), so no
	// polling is needed — but allow one retry just in case of scheduling jitter.
	var finalCount int64
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		finalCount, err = srv.ActiveSessionCount(ctx)
		if err != nil {
			t.Fatalf("ActiveSessionCount after close: %v", err)
		}
		if finalCount == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if finalCount != 0 {
		t.Errorf("ActiveSessionCount after CloseSession = %d, want 0", finalCount)
	}

	// Graceful disconnect.
	if err := client.Stop(ctx); err != nil {
		t.Fatalf("client Stop: %v", err)
	}
}
