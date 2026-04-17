// Package sba provides the 5G Service-Based Architecture client for the
// Argus simulator. It exercises Argus's SBA proxy at :8443, which implements
// the AUSF and UDM interfaces defined by 3GPP TS 29.509 / 29.503.
//
// # Architecture
//
// Each operator that opts in (sba.enabled: true in the YAML config) gets its
// own *Client backed by a single *http.Client with a per-operator connection
// pool. HTTP/1.1 cleartext is the default transport (matching the dev compose
// default where TLS is off); HTTP/2 is enabled automatically via ALPN when
// tls_enabled is true.
//
// # Protocol selection
//
// ShouldUseSBA determines, per session, whether a 5G-SBA flow is used instead
// of RADIUS. Selection is deterministic for a given AcctSessionID: the same
// session always lands in the same bucket. The picker uses a SHA-256 hash of
// the AcctSessionID modulo 10 000 and compares against rate*10 000.
//
// # Minimum flow
//
// A selected session runs three HTTP calls in order:
//
//  1. POST /nausf-auth/v1/ue-authentications  — start 5G-AKA
//  2. PUT  <link-href>/5g-aka-confirmation     — confirm with resStar
//  3. PUT  /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access — AMF register
//
// # Optional calls
//
// When IncludeOptionalCalls is true, a per-session 20% Bernoulli roll prepends
// GET /nudm-ueau/v1/{supi}/security-information before Authenticate, and a
// second independent 20% roll appends POST /nudm-ueau/v1/{supi}/auth-events
// after session hold completes. Both calls are best-effort — failures are
// logged and discarded without aborting the session.
//
// # Sentinel errors
//
// Client methods return wrapped sentinel errors (ErrAuthFailed, ErrConfirmFailed,
// ErrTimeout, ErrTransport, ErrServerError) so the engine can classify session
// aborts into disjoint Prometheus label buckets without reading HTTP status
// codes outside this package.
//
// # Reuse
//
// Request and response types are imported from internal/aaa/sba (the server's
// own types package) to guarantee JSON shape compatibility. Crypto helpers
// (generate5GAV, derivePseudoRandom, sha256Sum) are deliberately duplicated in
// crypto.go because the server functions are unexported; a golden canary test
// in crypto_test.go catches drift immediately.
package sba
