/**
 * Type-check smoke test for NotificationPreferencesPanel — FIX-237 E2.
 *
 * The project has no vitest + @testing-library/react — see
 * web/src/pages/sims/__tests__/index.test.tsx for the precedent. This file
 * validates TypeScript imports, the EventTier discriminator on
 * EventCatalogEntry, and documents the rendering scenarios that should be
 * implemented when vitest + RTL are added.
 *
 * SCENARIO 1 — Tier 1 events suppressed from rendered list:
 *   Mount <NotificationPreferencesPanel /> with mocked useEventCatalog
 *   returning a mixed-tier catalog (e.g. session.started=internal,
 *   operator_down=operational) and mocked useNotificationPreferences
 *   returning preference rows for BOTH event types.
 *   Assert: only the operator_down row renders. session.started row does NOT.
 *
 * SCENARIO 2 — "Add preference" picker excludes Tier 1:
 *   With same catalog as SCENARIO 1, render the panel.
 *   Assert: the picker dropdown contains operator_down but NOT session.started.
 *
 * SCENARIO 3 — Loading state:
 *   Mock useEventCatalog with isLoading=true, useNotificationPreferences
 *   with data ready. Assert: panel renders a loading state, NOT the
 *   filtered/unfiltered table (avoids brief flash of soon-filtered rows).
 *
 * SCENARIO 4 — Catalog with all-Tier-3 catalog renders all preferences:
 *   No tier 1 entries in catalog → all preference rows pass through.
 */

import type { EventCatalogEntry, EventTier } from '@/types/events'

// Type assertion: EventCatalogEntry MUST carry tier discriminator
const sampleEntry: EventCatalogEntry = {
  type: 'operator_down',
  source: 'argus.events.operator.health',
  default_severity: 'high',
  entity_type: 'operator',
  description: 'Operator outage detected',
  meta_schema: { operator_id: 'uuid' },
  tier: 'operational',
}

// Type assertion: EventTier is the union of three tier strings only
const tierValues: EventTier[] = ['internal', 'digest', 'operational']

// Compile-time ban: any other tier string must be rejected by tsc.
// Uncommenting the next line MUST cause a tsc failure (covered by reviewer).
// const bad: EventTier = 'invalid'

void sampleEntry
void tierValues

export {}
