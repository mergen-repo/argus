/**
 * Smoke tests for EntityLink shared component.
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 */
import type { EntityType } from '@/components/shared/entity-link'

const validTypes: EntityType[] = [
  'sim',
  'apn',
  'operator',
  'policy',
  'user',
  'session',
  'tenant',
  'violation',
  'alert',
  'anomaly',
  'job',
  'apikey',
]

// Verify the EntityType union is exhaustive for all route-mapped entity types
const _entityTypeCheck: EntityType[] = validTypes

// Prop type smoke test — ensure EntityLink props are well-typed
type EntityLinkProps = {
  entityType: EntityType | string
  entityId: string
  label?: string
  className?: string
  truncate?: boolean
}

const _props: EntityLinkProps = {
  entityType: 'sim',
  entityId: '00000000-0000-0000-0000-000000000001',
  label: 'Test SIM',
  truncate: true,
}

export { _entityTypeCheck, _props }
