# Tenant Suspension Workflow

## When to use

- Tenant is delinquent on payment and billing has triggered suspension
- Tenant account is suspected of fraud, abuse, or policy violation
- Tenant explicitly requests account suspension (e.g., going dormant)
- Regulatory or legal hold requiring immediate cessation of service
- Security incident where tenant credentials are believed compromised

## Prerequisites

- `argusctl` CLI installed and authenticated with `system-admin` role
- The target tenant's ID (UUID) — obtain from the admin UI or `argusctl tenant list`
- Written authorization for suspension (ticket number, email, or legal notice reference)
- Understanding of impact: which SIMs, sessions, and jobs belong to this tenant

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Pre-suspension impact assessment | 5–10 min |
| Step 2 — Suspend the tenant | 1–2 min |
| Step 3 — Verify suspension is active | 2–3 min |
| Step 4 — Handle active sessions | 2–5 min |
| Step 5 — Notify affected parties | 5 min |
| **Total** | **~15–25 min** |

---

## Procedure

### 1. Pre-suspension impact assessment

Before suspending, quantify what will be affected.

```bash
# Get tenant details
TENANT_ID="<tenant_uuid>"
argusctl tenant show ${TENANT_ID}
# Expected: tenant name, status=active, plan, created_at, contact email

# Count active SIMs under this tenant
argusctl sim list --tenant=${TENANT_ID} --status=active --count
# Expected: total number of active SIMs

# Count active RADIUS/Diameter sessions
curl -s "http://localhost:9090/api/v1/query?query=argus_active_sessions%7Btenant_id%3D%22${TENANT_ID}%22%7D" | \
  jq '.data.result[] | {operator: .metric.operator_id, sessions: .value[1]}'
# Expected: current active session count per operator for this tenant

# List active API keys (will be deactivated on suspension)
argusctl apikey list --tenant=${TENANT_ID}
# Expected: list of API keys and their last-used timestamps

# Check background jobs
argusctl job list --tenant=${TENANT_ID} --status=running
# Expected: any running jobs for this tenant that will be stopped

# Record these numbers before proceeding — needed for the post-suspension report
echo "Tenant: ${TENANT_ID}"
echo "Active SIMs: $(argusctl sim list --tenant=${TENANT_ID} --status=active --count)"
echo "Authorization: <ticket_number_or_email_reference>"
```

### 2. Suspend the tenant

```bash
# Suspend the tenant with a reason and authorization reference
argusctl tenant suspend ${TENANT_ID} \
  --reason="<payment_delinquent|fraud|abuse|legal_hold|security_incident|tenant_request>" \
  --reference="<ticket_id_or_legal_notice>"
# Expected: Tenant <id> suspended. Status: suspended.
# This action:
#   - Sets tenant.status = 'suspended' in the database
#   - Immediately blocks all API requests from this tenant (returns 403)
#   - Queues background tasks to terminate active sessions
#   - Deactivates all API keys for this tenant
#   - Pauses all scheduled jobs for this tenant
#   - Creates an audit log entry automatically

# Example with all flags:
# argusctl tenant suspend 550e8400-e29b-41d4-a716-446655440000 \
#   --reason="payment_delinquent" \
#   --reference="BILLING-4521"
```

### 3. Verify suspension is active

```bash
# Confirm tenant status is suspended
argusctl tenant show ${TENANT_ID} | jq '{status: .status, suspended_at: .suspended_at, reason: .suspend_reason}'
# Expected: {"status": "suspended", "suspended_at": "<timestamp>", "reason": "<reason>"}

# Verify API access is blocked for this tenant
# Try an API call with a tenant API key (should return 403)
curl -sf -H "Authorization: Bearer <tenant_api_key>" \
  http://localhost:8084/api/v1/sims | jq '.error'
# Expected: {"code": "TENANT_SUSPENDED", "message": "Tenant account is suspended"}

# Verify no new auth requests are being processed for this tenant
curl -s "http://localhost:9090/api/v1/query?query=rate(argus_aaa_auth_requests_total%7Btenant_id%3D%22${TENANT_ID}%22%7D%5B2m%5D)" | \
  jq '.data.result[] | {result: .metric.result, rate: .value[1]}'
# Expected: rate approaching 0 (existing sessions may still terminate over 1–2 minutes)

# Confirm API keys are deactivated
argusctl apikey list --tenant=${TENANT_ID}
# Expected: all keys show status=deactivated

# Check audit log for the suspension event
argusctl audit logs --tenant=${TENANT_ID} --action=tenant_suspend --limit=1
# Expected: audit entry with action=tenant_suspend, actor=<your_user>, reference=<ticket>
```

### 4. Handle active sessions

Active RADIUS/Diameter sessions for the tenant's SIMs do not terminate instantly — the NAS must be notified via CoA/Disconnect-Request. The suspension process queues these, but verify they drain.

```bash
# Monitor session count draining after suspension
watch -n 15 'curl -s "http://localhost:9090/api/v1/query?query=argus_active_sessions%7Btenant_id%3D%22'${TENANT_ID}'%22%7D" | jq "[.data.result[] | {operator: .metric.operator_id, sessions: .value[1]}]"'
# Expected: counts decreasing over 2–5 minutes as CoA/Disconnect-Request messages are processed

# If sessions are not draining after 10 minutes, manually force termination
argusctl session terminate-all --tenant=${TENANT_ID}
# Expected: all sessions queued for termination

# Verify sessions have drained
curl -s "http://localhost:9090/api/v1/query?query=argus_active_sessions%7Btenant_id%3D%22${TENANT_ID}%22%7D" | \
  jq '.data.result[] | {operator: .metric.operator_id, sessions: .value[1]}'
# Expected: all values at or near 0

# Stop any running CDR ingestion or analytics jobs for this tenant
argusctl job pause --tenant=${TENANT_ID} --all
# Expected: all running jobs paused for tenant
```

### 5. Notify affected parties

```bash
# Send suspension notification to the tenant's contact email (via argusctl or manually)
argusctl notification send \
  --tenant=${TENANT_ID} \
  --type=tenant_suspension \
  --channel=email \
  --template=suspension_notice \
  --reference="<ticket_id>"
# Expected: notification queued

# Post to internal incident channel
echo "[ACTION] Tenant ${TENANT_ID} suspended at $(date -u +%Y-%m-%dT%H:%M:%SZ). Reason: <reason>. Auth: <ticket>. Sessions: <N> terminated. SIMs: <N> deactivated."
```

---

## What users see when tenant is suspended

When a tenant is in `suspended` state:

- **API requests**: All API calls return `HTTP 403` with body `{"status":"error","error":{"code":"TENANT_SUSPENDED","message":"Account is suspended. Contact support."}}`
- **Web UI login**: Login succeeds but all pages show "Account Suspended" banner with support contact link
- **RADIUS/Diameter auth**: New auth requests are rejected; existing sessions receive Disconnect-Request
- **Webhooks**: Outbound webhook deliveries are paused
- **Scheduled jobs**: All tenant jobs move to `paused` state (not deleted — they resume on unsuspend)
- **Data**: All tenant data is preserved; nothing is deleted during suspension

---

## Un-suspending a tenant

When the suspension reason is resolved (payment received, security issue cleared, etc.):

```bash
# Un-suspend the tenant
argusctl tenant unsuspend ${TENANT_ID} \
  --reason="<payment_received|security_cleared|tenant_request>" \
  --reference="<ticket_id>"
# Expected: Tenant <id> active. Status: active.

# Verify tenant is active
argusctl tenant show ${TENANT_ID} | jq '{status: .status, unsuspended_at: .unsuspended_at}'
# Expected: {"status": "active", "unsuspended_at": "<timestamp>"}

# Verify API access is restored
curl -sf -H "Authorization: Bearer <tenant_api_key>" \
  http://localhost:8084/api/v1/sims | jq '.status'
# Expected: "success" (API key must be re-activated separately if it was fully deleted)

# Resume paused jobs
argusctl job resume --tenant=${TENANT_ID} --all
# Expected: all paused jobs resumed

# Confirm audit log for unsuspension
argusctl audit logs --tenant=${TENANT_ID} --action=tenant_unsuspend --limit=1
# Expected: audit entry created
```

---

## Verification

- `argusctl tenant show <id>` returns `status=suspended`
- API calls with tenant key return `HTTP 403 TENANT_SUSPENDED`
- `argus_active_sessions{tenant_id="<id>"}` approaches 0 within 5 minutes
- Audit log contains `action=tenant_suspend` entry with correct reference
- `curl http://localhost:8084/health/ready` returns 200 (platform unaffected)

---

## Post-incident

- Audit log entry is created automatically by `argusctl tenant suspend`; verify it exists
- Create a suspension record in your CRM/billing system referencing the ticket
- If suspension is due to security incident, also follow the `cert-rotation.md` runbook to rotate any shared credentials
- Set a calendar reminder to review the suspension and unsuspend or escalate to termination within the SLA period (check customer contract)
- **Comms template (incident channel):**
  > `[ACTION] Tenant <name> (<id>) suspended at <time>. Reason: <reason>. Authorization: <ticket>. Sessions terminated: <N>. SIMs affected: <N>. Tenant contact notified via <email|Telegram>.`
- **Stakeholder email (internal):**
  > Subject: [Argus] Tenant suspension — <tenant_name>
  > Body: Tenant <name> (ID: <id>) has been suspended as of <time>. Reason: <reason>. Authorization: <ticket/name>. Affected sessions: <N> (terminated). Affected SIMs: <N> (deactivated). Data preserved. Suspension reviewable by <date>.
