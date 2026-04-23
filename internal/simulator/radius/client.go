// Package radius wraps layeh.com/radius with session-lifecycle helpers the
// engine needs: BuildAuth, BuildAcctStart, BuildAcctInterim, BuildAcctStop.
// All packets are wire-compatible with RFC 2865/2866 and accepted by
// Argus's RADIUS server at :1812/:1813.
package radius

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/metrics"
	"github.com/google/uuid"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
)

// Client holds per-run shared state: destinations, shared secret, UDP timeouts.
type Client struct {
	authAddr   string
	acctAddr   string
	secret     []byte
	dialer     *net.Dialer
	rwTimeout  time.Duration
	retries    int
}

func New(host string, authPort, acctPort int, sharedSecret string) *Client {
	return &Client{
		authAddr:  fmt.Sprintf("%s:%d", host, authPort),
		acctAddr:  fmt.Sprintf("%s:%d", host, acctPort),
		secret:    []byte(sharedSecret),
		dialer:    &net.Dialer{Timeout: 3 * time.Second},
		rwTimeout: 3 * time.Second,
		retries:   2,
	}
}

// SessionContext carries the fields that persist across Auth → Accounting
// for one simulated session. The engine constructs this, threads it through
// every packet-builder, and discards it on session end.
type SessionContext struct {
	SIM                   discovery.SIM
	NASIP                 string
	NASIdentifier         string
	AcctSessionID         string
	FramedIP              net.IP        // filled in after Access-Accept
	StartedAt             time.Time
	BytesIn               uint64
	BytesOut              uint64
	PacketsIn             uint64
	PacketsOut            uint64
	ServerSessionTimeout  time.Duration // non-zero if Access-Accept included Session-Timeout
	ReplyMessage          string        // non-empty if response included Reply-Message (Accept or Reject)
}

// NewSessionContext mints the per-session state with a fresh Acct-Session-Id.
func NewSessionContext(sim discovery.SIM, nasIP, nasID string) *SessionContext {
	return &SessionContext{
		SIM:           sim,
		NASIP:         nasIP,
		NASIdentifier: nasID,
		AcctSessionID: uuid.NewString(),
		StartedAt:     time.Now(),
	}
}

// Auth sends an Access-Request and returns the parsed response. On
// Access-Accept, the returned *radius.Packet carries attribute values the
// caller may inspect (Framed-IP-Address is extracted into sc.FramedIP).
func (c *Client) Auth(ctx context.Context, sc *SessionContext) (*radius.Packet, error) {
	pkt := radius.New(radius.CodeAccessRequest, c.secret)
	_ = rfc2865.UserName_SetString(pkt, sc.SIM.IMSI)
	// User-Password with PAP — use IMSI as placeholder. Argus in test mode
	// accepts any non-empty password because the mock adapter's subscriber
	// lookup is keyed on IMSI, not password.
	_ = rfc2865.UserPassword_SetString(pkt, sc.SIM.IMSI)
	setCommonNAS(pkt, sc)
	_ = rfc2865.ServiceType_Set(pkt, rfc2865.ServiceType_Value_FramedUser)
	_ = rfc2865.FramedProtocol_Set(pkt, rfc2865.FramedProtocol_Value_PPP)
	if sc.SIM.APNName != nil && *sc.SIM.APNName != "" {
		_ = rfc2865.CalledStationID_SetString(pkt, *sc.SIM.APNName)
	}
	if sc.SIM.MSISDN != nil && *sc.SIM.MSISDN != "" {
		_ = rfc2865.CallingStationID_SetString(pkt, *sc.SIM.MSISDN)
	}
	// Message-Authenticator must be present for response validation on
	// servers that enforce RFC 5080; layeh's library fills this when
	// the attribute type is set to zeros before signing.
	pkt.Add(rfc2869.MessageAuthenticator_Type, make([]byte, 16))

	resp, err := c.exchange(ctx, c.authAddr, pkt)
	if err != nil {
		return nil, err
	}
	// Parse response attributes on both Accept and Reject paths.
	// Session-Timeout and Reply-Message may appear in either code path
	// depending on the NAS deployment; Framed-IP-Address is Accept-only.
	if t := rfc2865.SessionTimeout_Get(resp); t > 0 {
		sc.ServerSessionTimeout = time.Duration(t) * time.Second
	}
	if msg := rfc2865.ReplyMessage_GetString(resp); msg != "" {
		sc.ReplyMessage = msg
	}
	if resp.Code == radius.CodeAccessAccept {
		if ip := rfc2865.FramedIPAddress_Get(resp); ip != nil {
			sc.FramedIP = ip
		}
	}
	return resp, nil
}

// AcctStart sends Accounting-Request with Acct-Status-Type=Start.
func (c *Client) AcctStart(ctx context.Context, sc *SessionContext) (*radius.Packet, error) {
	pkt := c.newAcct(sc, rfc2866.AcctStatusType_Value_Start)
	return c.exchange(ctx, c.acctAddr, pkt)
}

// AcctInterim sends an update with cumulative counters.
func (c *Client) AcctInterim(ctx context.Context, sc *SessionContext) (*radius.Packet, error) {
	pkt := c.newAcct(sc, rfc2866.AcctStatusType_Value_InterimUpdate)
	return c.exchange(ctx, c.acctAddr, pkt)
}

// AcctStop sends a final Accounting-Request with Acct-Status-Type=Stop and
// a Terminate-Cause (User-Request for normal scenario-driven ends).
func (c *Client) AcctStop(ctx context.Context, sc *SessionContext, cause rfc2866.AcctTerminateCause) (*radius.Packet, error) {
	pkt := c.newAcct(sc, rfc2866.AcctStatusType_Value_Stop)
	_ = rfc2866.AcctTerminateCause_Set(pkt, cause)
	return c.exchange(ctx, c.acctAddr, pkt)
}

// Helpers ─────────────────────────────────────────────────────────────

func (c *Client) newAcct(sc *SessionContext, status rfc2866.AcctStatusType) *radius.Packet {
	pkt := radius.New(radius.CodeAccountingRequest, c.secret)
	_ = rfc2865.UserName_SetString(pkt, sc.SIM.IMSI)
	setCommonNAS(pkt, sc)
	_ = rfc2866.AcctStatusType_Set(pkt, status)
	_ = rfc2866.AcctSessionID_SetString(pkt, sc.AcctSessionID)

	// Cumulative counters. 32-bit RADIUS counters are the standard; the
	// 64-bit VSA extensions exist but Argus's Accounting handler reads
	// the standard attributes.
	_ = rfc2866.AcctInputOctets_Set(pkt, rfc2866.AcctInputOctets(sc.BytesIn&0xFFFFFFFF))
	_ = rfc2866.AcctOutputOctets_Set(pkt, rfc2866.AcctOutputOctets(sc.BytesOut&0xFFFFFFFF))
	_ = rfc2866.AcctInputPackets_Set(pkt, rfc2866.AcctInputPackets(sc.PacketsIn&0xFFFFFFFF))
	_ = rfc2866.AcctOutputPackets_Set(pkt, rfc2866.AcctOutputPackets(sc.PacketsOut&0xFFFFFFFF))
	_ = rfc2866.AcctSessionTime_Set(pkt, rfc2866.AcctSessionTime(time.Since(sc.StartedAt).Seconds()))

	if sc.FramedIP != nil {
		_ = rfc2865.FramedIPAddress_Set(pkt, sc.FramedIP)
	}
	if sc.SIM.APNName != nil && *sc.SIM.APNName != "" {
		_ = rfc2865.CalledStationID_SetString(pkt, *sc.SIM.APNName)
	}
	if sc.SIM.MSISDN != nil && *sc.SIM.MSISDN != "" {
		_ = rfc2865.CallingStationID_SetString(pkt, *sc.SIM.MSISDN)
	}
	return pkt
}

func setCommonNAS(pkt *radius.Packet, sc *SessionContext) {
	if ip := net.ParseIP(sc.NASIP); ip != nil {
		_ = rfc2865.NASIPAddress_Set(pkt, ip)
	} else {
		metrics.SimulatorNASIPMissingTotal.WithLabelValues(sc.SIM.OperatorCode).Inc()
	}
	if sc.NASIdentifier != "" {
		_ = rfc2865.NASIdentifier_SetString(pkt, sc.NASIdentifier)
	}
	_ = rfc2865.NASPortType_Set(pkt, rfc2865.NASPortType_Value_Virtual)
}

// exchange does the wire I/O with retries. Timeout errors are returned as-is
// so the caller can record them in metrics; network-level errors wrap through
// with %w.
func (c *Client) exchange(ctx context.Context, addr string, pkt *radius.Packet) (*radius.Packet, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		resp, err := radius.Exchange(ctx, pkt, addr)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		// Honor context cancellation — don't retry after shutdown.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
	}
	return nil, fmt.Errorf("radius exchange to %s: %w", addr, lastErr)
}
