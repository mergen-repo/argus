/**
 * Smoke tests for SCR-021f Device Binding tab (STORY-097 Task 7).
 * Type-level + runtime-throw assertions — mirrors existing project pattern
 * (settings/imei-pools.test.tsx). No vitest framework configured.
 *
 * Covered scenarios:
 *  1. CAPTURE_PROTOCOLS exposes all 3 backend-validated values
 *  2. BINDING_MODE_LABEL covers every BindingMode key
 *  3. BINDING_STATUS_LABEL covers every BindingStatus key
 *  4. bindingStatusVariant maps to expected Badge variants (color-coded chips)
 *  5. graceToneFor classifies expiry windows: >=24h safe, <24h urgent, <=0 expired
 *  6. formatGraceCountdown emits "Grace expired" / "X m" / "Y h" / "Z d"
 *  7. DeviceBinding DTO is structurally type-compatible with the API-327 contract
 *
 * Behavioural DOM tests (render + click + Dialog flow) require vitest+jsdom which
 * is not configured in this project. Coverage tracked alongside the project's broader
 * test infra wave; this file enforces structural and contract invariants via tsc.
 */

import {
  BINDING_MODE_LABEL,
  BINDING_STATUS_LABEL,
  CAPTURE_PROTOCOLS,
  CAPTURE_PROTOCOL_LABEL,
  bindingStatusVariant,
  formatGraceCountdown,
  graceToneFor,
  type BindingMode,
  type BindingStatus,
  type CaptureProtocol,
  type DeviceBinding,
  type IMEIHistoryRow,
} from '@/types/device-binding'

// Scenario 1 — capture protocols match backend whitelist (radius/diameter_s6a/5g_sba)

const expectedProtocols: ReadonlyArray<CaptureProtocol> = ['radius', 'diameter_s6a', '5g_sba']
if (CAPTURE_PROTOCOLS.length !== expectedProtocols.length) {
  throw new Error(
    `STORY-097 T7 FAIL: CAPTURE_PROTOCOLS must expose 3 values, got ${CAPTURE_PROTOCOLS.length}`,
  )
}
for (const p of expectedProtocols) {
  if (!(CAPTURE_PROTOCOLS as ReadonlyArray<string>).includes(p)) {
    throw new Error(`STORY-097 T7 FAIL: CAPTURE_PROTOCOLS missing "${p}"`)
  }
  if (!CAPTURE_PROTOCOL_LABEL[p]) {
    throw new Error(`STORY-097 T7 FAIL: CAPTURE_PROTOCOL_LABEL missing "${p}"`)
  }
}

// Scenario 2 — binding mode label exhaustiveness

const allModes: ReadonlyArray<BindingMode> = [
  'strict',
  'allowlist',
  'first-use',
  'tac-lock',
  'grace-period',
  'soft',
  'disabled',
]
for (const m of allModes) {
  if (!BINDING_MODE_LABEL[m]) {
    throw new Error(`STORY-097 T7 FAIL: BINDING_MODE_LABEL missing "${m}"`)
  }
}

// Scenario 3 — binding status label exhaustiveness

const allStatuses: ReadonlyArray<BindingStatus> = ['verified', 'pending', 'mismatch', 'disabled']
for (const s of allStatuses) {
  if (!BINDING_STATUS_LABEL[s]) {
    throw new Error(`STORY-097 T7 FAIL: BINDING_STATUS_LABEL missing "${s}"`)
  }
}

// Scenario 4 — color-coded chip variants per the spec
//   verified=success | pending=warning | mismatch=danger | disabled=secondary (muted)

if (bindingStatusVariant('verified') !== 'success') {
  throw new Error('STORY-097 T7 FAIL: verified must map to success variant')
}
if (bindingStatusVariant('pending') !== 'warning') {
  throw new Error('STORY-097 T7 FAIL: pending must map to warning variant')
}
if (bindingStatusVariant('mismatch') !== 'danger') {
  throw new Error('STORY-097 T7 FAIL: mismatch must map to danger variant')
}
if (bindingStatusVariant('disabled') !== 'secondary') {
  throw new Error('STORY-097 T7 FAIL: disabled must map to secondary (muted) variant')
}
if (bindingStatusVariant(null) !== 'secondary') {
  throw new Error('STORY-097 T7 FAIL: null status must map to secondary variant')
}

// Scenario 5 — grace tone classification

const NOW = new Date('2026-05-04T12:00:00Z').getTime()
const in36h = new Date('2026-05-06T00:00:00Z').toISOString() // ~36h ahead → caution
const in14h = new Date('2026-05-05T02:00:00Z').toISOString() // 14h ahead → urgent
const in1m = new Date('2026-05-04T12:01:00Z').toISOString() // 1m ahead → urgent
const past1h = new Date('2026-05-04T11:00:00Z').toISOString() // expired

if (graceToneFor(in36h, NOW) !== 'caution') {
  throw new Error('STORY-097 T7 FAIL: 36h-out grace must be tone "caution"')
}
if (graceToneFor(in14h, NOW) !== 'urgent') {
  throw new Error('STORY-097 T7 FAIL: 14h-out grace must be tone "urgent"')
}
if (graceToneFor(in1m, NOW) !== 'urgent') {
  throw new Error('STORY-097 T7 FAIL: 1m-out grace must be tone "urgent"')
}
if (graceToneFor(past1h, NOW) !== 'expired') {
  throw new Error('STORY-097 T7 FAIL: past expiry must be tone "expired"')
}
if (graceToneFor('not-a-date', NOW) !== 'expired') {
  throw new Error('STORY-097 T7 FAIL: invalid date must default to "expired"')
}

// Scenario 6 — countdown formatting

if (formatGraceCountdown(past1h, NOW) !== 'Grace expired') {
  throw new Error(
    `STORY-097 T7 FAIL: past expiry must format as "Grace expired", got "${formatGraceCountdown(past1h, NOW)}"`,
  )
}
const fmt36 = formatGraceCountdown(in36h, NOW)
if (!/^Grace expires in 36 h$/.test(fmt36)) {
  throw new Error(`STORY-097 T7 FAIL: 36h countdown malformed: "${fmt36}"`)
}
const fmt14 = formatGraceCountdown(in14h, NOW)
if (!/^Grace expires in 14 h$/.test(fmt14)) {
  throw new Error(`STORY-097 T7 FAIL: 14h countdown malformed: "${fmt14}"`)
}
const fmt1m = formatGraceCountdown(in1m, NOW)
if (!/^Grace expires in 1 m$/.test(fmt1m)) {
  throw new Error(`STORY-097 T7 FAIL: 1m countdown malformed: "${fmt1m}"`)
}

// Scenario 7 — DTO contract sanity (structural compile-time)

const sampleBinding: DeviceBinding = {
  bound_imei: '353901080000007',
  binding_mode: 'first-use',
  binding_status: 'verified',
  binding_verified_at: '2026-05-01T10:00:00Z',
  last_imei_seen_at: '2026-05-04T11:55:00Z',
  binding_grace_expires_at: null,
  history_count: 12,
}
void sampleBinding

const sampleHistoryRow: IMEIHistoryRow = {
  id: '00000000-0000-0000-0000-000000000001',
  sim_id: '00000000-0000-0000-0000-000000000002',
  tenant_id: '00000000-0000-0000-0000-000000000003',
  observed_imei: '353901080000007',
  observed_software_version: null,
  observed_at: '2026-05-04T11:55:00Z',
  capture_protocol: 'radius',
  nas_ip_address: '10.0.0.1',
  was_mismatch: false,
  alarm_raised: false,
}
void sampleHistoryRow

export {
  expectedProtocols,
}
