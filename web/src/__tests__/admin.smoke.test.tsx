/**
 * Smoke tests for admin pages — structural import validation.
 * Actual rendering tests require a vitest/jsdom setup (not configured yet).
 * These imports are checked by `tsc --noEmit` during the build gate.
 */

import type { DeliveryStatus, ActiveSession, APIKeyUsageItem, PurgeHistoryItem } from '@/types/admin'

const _ds: DeliveryStatus = {
  webhook: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  email: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  sms: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  in_app: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
  telegram: { success_rate: 0.99, failure_rate: 0.01, retry_depth: 0, last_delivery_at: null, p50_ms: 10, p95_ms: 50, p99_ms: 100, health: 'green' },
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

void [_ds, _as, _aku, _ph]

export {}
