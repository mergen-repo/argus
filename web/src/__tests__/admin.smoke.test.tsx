/**
 * Smoke tests for admin pages — structural import validation.
 * Actual rendering tests require a vitest/jsdom setup (not configured yet).
 * These imports are checked by `tsc --noEmit` during the build gate.
 */

import type { KillSwitch, MaintenanceWindow, DeliveryStatus, TenantResourceItem, TenantQuota, CostTenant, ActiveSession, APIKeyUsageItem, DSARQueueItem, PurgeHistoryItem } from '@/types/admin'

// Verify all admin types are well-formed
const _ks: KillSwitch = {
  key: 'read_only_mode',
  label: 'Read Only Mode',
  description: 'Disables all mutations',
  enabled: false,
  reason: '',
  toggled_by: null,
  toggled_at: null,
  created_at: '2026-01-01T00:00:00Z',
}

const _mw: MaintenanceWindow = {
  id: '00000000-0000-0000-0000-000000000001',
  tenant_id: null,
  title: 'Test',
  description: '',
  starts_at: '2026-01-01T00:00:00Z',
  ends_at: '2026-01-01T01:00:00Z',
  affected_services: ['radius'],
  cron_expression: '',
  notify_plan: {},
  state: 'scheduled',
  created_by: null,
  created_at: '2026-01-01T00:00:00Z',
}

const _ds: DeliveryStatus = {
  webhook: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  email: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  sms: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  in_app: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  telegram: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
}

const _tr: TenantResourceItem = {
  tenant_id: 'tid',
  tenant_name: 'ACME',
  sim_count: 1000,
  active_sessions: 5,
  api_rps: 2.5,
  cdr_bytes_30d: 1e9,
  storage_bytes: 5e8,
  spark: [1, 2, 3, 4, 5, 6],
}

const _tq: TenantQuota = {
  tenant_id: 'tid',
  tenant_name: 'ACME',
  sims: { current: 500, max: 1000, pct: 50, status: 'ok' },
  api_rps: { current: 10, max: 100, pct: 10, status: 'ok' },
  sessions: { current: 3, max: 50, pct: 6, status: 'ok' },
  storage_bytes: { current: 1e8, max: 1e10, pct: 1, status: 'ok' },
}

const _ct: CostTenant = {
  tenant_id: 'tid',
  tenant_name: 'ACME',
  currency: 'USD',
  total: 1200,
  radius_cost: 400,
  operator_cost: 600,
  sms_cost: 100,
  storage_cost: 100,
  trend: [900, 950, 1000, 1100, 1200, 1200],
}

const _as: ActiveSession = {
  session_id: 'sid',
  user_id: 'uid',
  user_email: 'user@example.com',
  tenant_id: 'tid',
  tenant_name: 'ACME',
  ip_address: '127.0.0.1',
  browser: 'Chrome',
  os: 'Linux',
  idle_seconds: 60,
  created_at: '2026-01-01T00:00:00Z',
  last_seen_at: '2026-01-01T00:01:00Z',
}

const _aku: APIKeyUsageItem = {
  key_id: 'kid',
  key_name: 'API Key 1',
  tenant_id: 'tid',
  tenant_name: 'ACME',
  requests: 1000,
  rate_limit: 100,
  consumption_pct: 10,
  error_rate: 0.01,
  anomaly: false,
}

const _dq: DSARQueueItem = {
  job_id: 'jid',
  type: 'data_portability_export',
  tenant_id: 'tid',
  subject_id: 'sid',
  status: 'received',
  sla_hours: 72,
  sla_remaining_hours: 48,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const _ph: PurgeHistoryItem = {
  sim_id: 'simid',
  iccid: '8901234567890123456',
  msisdn: '+905001234567',
  tenant_id: 'tid',
  tenant_name: 'ACME',
  purged_at: '2026-01-01T00:00:00Z',
  reason: 'kvkk',
  actor_id: null,
}

// Suppress unused variable warnings for type-check-only file
void [_ks, _mw, _ds, _tr, _tq, _ct, _as, _aku, _dq, _ph]

export {}
