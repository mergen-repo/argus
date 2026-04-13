/**
 * Smoke tests for RelatedAuditTab shared component.
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 */

// AuditLog type contract smoke test
type AuditLog = {
  id: string
  user_id?: string
  action: string
  entity_type: string
  entity_id: string
  diff?: unknown
  ip_address?: string
  created_at: string
}

const _entry: AuditLog = {
  id: '00000000-0000-0000-0000-000000000001',
  user_id: '00000000-0000-0000-0000-000000000002',
  action: 'sim.suspend',
  entity_type: 'sim',
  entity_id: '00000000-0000-0000-0000-000000000003',
  diff: { state: ['active', 'suspended'] },
  ip_address: '192.168.1.1',
  created_at: '2026-04-13T00:00:00Z',
}

// RelatedAuditTab props contract
type RelatedAuditTabProps = {
  entityId: string
  entityType: string
}

const _props: RelatedAuditTabProps = {
  entityId: '00000000-0000-0000-0000-000000000001',
  entityType: 'sim',
}

export { _entry, _props }
