# TLS Certificate and JWT Key Rotation

## When to use

- TLS certificate is within 30 days of expiry (`certbot` or monitoring alert)
- JWT signing key is scheduled for rotation (quarterly or on security incident)
- Security incident where private key material may have been compromised
- Certificate Authority (CA) change
- `argus_jwt_verify_total` audit shows unexpected key slot usage patterns (from STORY-066 JWT rotation audit)

## Prerequisites

- `docker`, `docker compose` on operator machine
- `certbot` installed (for Let's Encrypt) OR new certificate PEM files from CA
- Access to modify Argus environment variables (`deploy/.env.production` or secrets manager)
- Ability to restart argus and nginx containers
- `openssl` installed for certificate validation
- Admin access to `argusctl` for audit log entries

## Estimated Duration

| Step | Expected time |
|------|---------------|
| TLS: Step 1 — Obtain new certificate | 5–30 min |
| TLS: Step 2 — Install and reload nginx | 2–5 min |
| TLS: Step 3 — Verify TLS | 2–3 min |
| JWT: Step A — Add new JWT secret | 2–3 min |
| JWT: Step B — Restart argus | 2–5 min |
| JWT: Step C — Verify dual-key period | 5 min |
| JWT: Step D — Retire old key | 2–3 min |
| **Total (TLS only)** | **~15–40 min** |
| **Total (JWT only)** | **~15–20 min** |
| **Total (both)** | **~30–60 min** |

---

## Part 1: TLS Certificate Rotation

### TLS Step 1 — Obtain new certificate

**Option A: Let's Encrypt via certbot (automated)**

```bash
# Renew via certbot with nginx authenticator
certbot renew --nginx --cert-name argus.yourdomain.com --non-interactive
# Expected: "Congratulations! Your certificate and chain have been saved at /etc/letsencrypt/live/argus.yourdomain.com/fullchain.pem"

# If certbot is not installed or first-time setup:
certbot certonly \
  --standalone \
  --non-interactive \
  --agree-tos \
  --email ops@yourdomain.com \
  -d argus.yourdomain.com
# Expected: certificate files created in /etc/letsencrypt/live/argus.yourdomain.com/
```

**Option B: Manual certificate from CA**

```bash
# Generate a new private key and CSR
openssl genrsa -out /tmp/argus.key 4096
openssl req -new -key /tmp/argus.key \
  -subj "/CN=argus.yourdomain.com/O=YourOrg/C=TR" \
  -out /tmp/argus.csr
# Expected: argus.csr file created

# Submit the CSR to your CA and receive the signed certificate
# Place the received certificate at /tmp/argus.crt (full chain including intermediates)

# Validate the new certificate before installing
openssl x509 -in /tmp/argus.crt -noout -text | grep -E 'Not After|Subject|Issuer'
# Expected: Not After shows the new expiry date (> 90 days away)

# Verify the certificate matches the private key
openssl x509 -noout -modulus -in /tmp/argus.crt | md5sum
openssl rsa -noout -modulus -in /tmp/argus.key | md5sum
# Expected: both md5sum values must match — if they don't, the cert and key are mismatched
```

### TLS Step 2 — Install certificate and reload nginx

```bash
# Copy new certificate files to the nginx cert directory
# (adjust paths to match your nginx volume mount)
cp /tmp/argus.crt deploy/nginx/certs/argus.crt
cp /tmp/argus.key deploy/nginx/certs/argus.key
# For Let's Encrypt:
# cp /etc/letsencrypt/live/argus.yourdomain.com/fullchain.pem deploy/nginx/certs/argus.crt
# cp /etc/letsencrypt/live/argus.yourdomain.com/privkey.pem deploy/nginx/certs/argus.key

# Verify nginx config is valid before reloading
docker compose -f deploy/docker-compose.yml exec nginx nginx -t
# Expected: "syntax is ok" and "test is successful"

# Reload nginx to apply new certificate (zero-downtime — uses SIGUSR1 / graceful reload)
docker compose -f deploy/docker-compose.yml exec nginx nginx -s reload
# Expected: nginx reloads without terminating existing connections

# Verify the new certificate is being served
openssl s_client -connect argus.yourdomain.com:443 -servername argus.yourdomain.com < /dev/null 2>/dev/null | \
  openssl x509 -noout -dates
# Expected: notAfter shows the new expiry date
```

### TLS Step 3 — Verify TLS

```bash
# Full TLS handshake test
curl -sv https://argus.yourdomain.com/health/ready 2>&1 | grep -E 'SSL|TLS|certificate|expire|issuer'
# Expected: TLS handshake succeeds, certificate details from new cert

# Check certificate expiry is > 30 days
openssl s_client -connect argus.yourdomain.com:443 -servername argus.yourdomain.com < /dev/null 2>/dev/null | \
  openssl x509 -noout -checkend $((30 * 86400))
# Expected: "Certificate will not expire" (checks 30 days)

# Verify health check passes via HTTPS
curl -sf https://argus.yourdomain.com/health/ready | jq
# Expected: {"status":"ok", ...}

# Create audit log entry
argusctl audit log \
  --action=tls_cert_rotated \
  --resource=nginx \
  --note="New certificate installed. Expiry: <new_expiry>. Previous expiry: <old_expiry>. CN: argus.yourdomain.com."
# Expected: Audit log entry created

# Clean up temporary files
rm -f /tmp/argus.key /tmp/argus.csr /tmp/argus.crt
```

---

## Part 2: JWT Signing Key Rotation

Argus supports dual-key JWT verification to enable zero-downtime key rotation. The current key signs new tokens; the previous key remains valid until all tokens it issued have expired (TTL-based). This follows the STORY-066 JWT rotation audit design.

The `argus_jwt_verify_total` metric tracks verification by `key_slot` label — use this to confirm both keys are active and eventually confirm the old key is no longer used.

### JWT Step A — Add new JWT secret (dual-key window)

```bash
# Generate a new cryptographically strong JWT secret
NEW_JWT_SECRET=$(openssl rand -base64 64 | tr -d '\n')
echo "New JWT secret (save this securely): ${NEW_JWT_SECRET}"

# Update the environment configuration
# The current JWT_SECRET becomes JWT_SECRET_PREVIOUS
# The new secret becomes JWT_SECRET
# This enables argus to verify tokens signed by EITHER key

# Edit the production environment file
# (or update your secrets manager — Vault, AWS Secrets Manager, etc.)
# The variable names argus expects:
#   JWT_SECRET          — active signing key (new tokens are signed with this)
#   JWT_SECRET_PREVIOUS — previous key (old tokens are verified with this)

# Example using deploy/.env.production:
# 1. Copy current JWT_SECRET to JWT_SECRET_PREVIOUS
# 2. Set JWT_SECRET to the new value

# Read current value
CURRENT_JWT_SECRET=$(grep '^JWT_SECRET=' deploy/.env.production | cut -d'=' -f2-)

# Update the file (set previous key, then new key)
# Use your preferred secrets management approach:
sed -i.bak \
  -e "s|^JWT_SECRET=.*|JWT_SECRET=${NEW_JWT_SECRET}|" \
  -e "s|^JWT_SECRET_PREVIOUS=.*|JWT_SECRET_PREVIOUS=${CURRENT_JWT_SECRET}|" \
  deploy/.env.production

# Or if JWT_SECRET_PREVIOUS does not exist yet, append it:
grep -q '^JWT_SECRET_PREVIOUS=' deploy/.env.production || \
  echo "JWT_SECRET_PREVIOUS=${CURRENT_JWT_SECRET}" >> deploy/.env.production

# Verify both variables are set correctly (do NOT echo secrets to CI logs)
grep '^JWT_SECRET' deploy/.env.production | cut -d'=' -f1
# Expected: JWT_SECRET and JWT_SECRET_PREVIOUS (names only, not values)
```

### JWT Step B — Restart argus to load new keys

```bash
# Restart argus to pick up the new JWT_SECRET and JWT_SECRET_PREVIOUS
docker compose -f deploy/docker-compose.yml up -d --force-recreate argus
# Expected: argus container restarts with new environment

# Wait for argus to be healthy
docker compose -f deploy/docker-compose.yml exec argus \
  sh -c 'for i in $(seq 30); do curl -sf http://localhost:8080/health/ready && break || sleep 2; done'
# Expected: healthy response within 60 seconds

# Verify argus started without JWT config errors
docker compose -f deploy/docker-compose.yml logs argus | tail -20 | grep -iE 'jwt|secret|key'
# Expected: "JWT dual-key mode active" or similar — no error messages

# Health check
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}
```

### JWT Step C — Verify dual-key period

During the dual-key window, both old and new tokens must be accepted. The window lasts until all tokens signed with the old key have expired (typically equal to the JWT TTL, e.g., 24 hours).

```bash
# Check jwt_verify_total metric to see verification activity per key slot
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_jwt_verify_total%5B5m%5D)' | \
  jq '.data.result[] | {key_slot: .metric.key_slot, verifications_per_sec: .value[1]}'
# Expected: both "current" and "previous" key slots show activity while old tokens are in use

# Test that new token login works
curl -sf -X POST http://localhost:8084/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@argus.io","password":"admin"}' | jq '.status'
# Expected: "success" — new token issued and signed with new JWT_SECRET

# Test that an old token still works (if you have one from before the rotation)
# curl -sf -H "Authorization: Bearer <old_token>" http://localhost:8084/api/v1/sims | jq '.status'
# Expected: "success" — old token verified with JWT_SECRET_PREVIOUS

# Monitor key slot usage over time (during dual-key window)
# As old tokens expire, "previous" slot rate should approach 0
watch -n 30 'curl -s "http://localhost:9090/api/v1/query?query=rate(argus_jwt_verify_total%5B5m%5D)" | jq "[.data.result[] | {slot: .metric.key_slot, rps: .value[1]}]"'
```

### JWT Step D — Retire the old key (after JWT TTL expires)

Once the JWT TTL has elapsed (check `JWT_TTL` or `JWT_ACCESS_TOKEN_EXPIRY` in config — typically 1h to 24h), no tokens signed with the old key should be in circulation.

```bash
# Confirm the previous key slot is no longer being used
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_jwt_verify_total%7Bkey_slot%3D"previous"%7D%5B5m%5D)' | \
  jq '.data.result[] | {key_slot: .metric.key_slot, rps: .value[1]}'
# Expected: rate = 0 (or very close to 0)

# Remove JWT_SECRET_PREVIOUS from the environment
sed -i.bak '/^JWT_SECRET_PREVIOUS=/d' deploy/.env.production
# Expected: line removed from env file

# Restart argus to remove the previous key from memory
docker compose -f deploy/docker-compose.yml up -d --force-recreate argus
# Expected: argus restarts in single-key mode

# Verify argus is healthy
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}

# Confirm only "current" key slot is active
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_jwt_verify_total%5B5m%5D)' | \
  jq '.data.result[] | {key_slot: .metric.key_slot, rps: .value[1]}'
# Expected: only "current" key slot shows activity; "previous" slot absent or at 0

# Create audit log entry
argusctl audit log \
  --action=jwt_key_rotated \
  --resource=auth \
  --note="JWT signing key rotated. Dual-key window: <start> to <end>. Previous key retired at $(date -u +%Y-%m-%dT%H:%M:%SZ)."
# Expected: Audit log entry created

# Securely delete the backup env files
rm -f deploy/.env.production.bak
```

---

## Verification

**TLS:**
- `openssl s_client -connect argus.yourdomain.com:443` shows new certificate with correct expiry
- `curl https://argus.yourdomain.com/health/ready` returns 200
- Expiry > 30 days: `openssl s_client ... | openssl x509 -noout -checkend $((30 * 86400))` exits 0

**JWT:**
- New logins work: `POST /api/auth/login` returns tokens
- `argus_jwt_verify_total{key_slot="current"}` is incrementing
- `argus_jwt_verify_total{key_slot="previous"}` reaches 0 after TTL window
- `curl http://localhost:8084/health/ready` returns 200

---

## Post-incident

- Audit log entries created for both `tls_cert_rotated` and `jwt_key_rotated` as applicable
- Update the certificate expiry tracking spreadsheet / monitoring alert threshold
- If rotation was triggered by compromise: rotate all related secrets (database passwords, NATS credentials, Redis AUTH password) and force-logout all active sessions with `argusctl session terminate-all`
- Schedule next rotation in the ops calendar (TLS: 60 days before expiry; JWT: quarterly)
- **Comms template (incident channel):**
  > `[ACTION] TLS certificate rotated for argus.yourdomain.com. New expiry: <date>. | JWT signing key rotated. Dual-key window: <duration>. No user action required.`
- **Stakeholder email (security incident only):**
  > Subject: [Argus] Credential rotation completed — security incident response
  > Body: Following the detection of a potential credential compromise at <time>, all TLS and JWT credentials for the Argus platform have been rotated. Active sessions have been terminated and users must re-authenticate. Action taken: <details>. Systems confirmed secure as of <time>.
