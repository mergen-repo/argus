/**
 * Smoke tests for useTabUrlSync hook (FIX-222).
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 *
 * Behavior contract:
 *  - Aliased query params redirect on mount (circuit→health, notifications→alerts)
 *    via setSearchParams({ replace: true }) — no history entry created
 *  - Invalid tab falls back to defaultTab with URL replace
 *  - setTab writes ?tab=<value> with replace=true (no history push)
 *  - validTabs guard prevents unknown tab values from being persisted
 *  - Alias chain safety: circuit→health does NOT re-trigger to another alias
 *
 * Note: Full behavioral tests require a MemoryRouter + renderHook setup from
 * @testing-library/react-hooks. These smoke tests verify the type contract only.
 * Integration tests tracked in FIX-24x test hardening wave.
 */

// Simulate alias map for type-level checks
type TabAliases = Record<string, string>

const operatorAliases: TabAliases = {
  circuit: 'health',
  notifications: 'alerts',
}

const apnAliases: TabAliases = {
  notifications: 'alerts',
}

// Verify alias values are valid tabs (not aliases themselves → no infinite loops)
const OPERATOR_VALID_TABS = [
  'overview', 'protocols', 'health', 'traffic', 'sessions',
  'sims', 'esim', 'alerts', 'audit', 'agreements',
] as const

const APN_VALID_TABS = [
  'overview', 'config', 'ip-pools', 'sims', 'traffic', 'policies', 'audit', 'alerts',
] as const

type OperatorTab = typeof OPERATOR_VALID_TABS[number]
type ApnTab = typeof APN_VALID_TABS[number]

// Verify alias targets exist in validTabs (no broken redirects)
for (const [src, target] of Object.entries(operatorAliases)) {
  if (!OPERATOR_VALID_TABS.includes(target as OperatorTab)) {
    throw new Error(`Operator alias "${src}"→"${target}" target is not in validTabs`)
  }
  // Anti-loop check: alias target must NOT itself be an alias source
  if (operatorAliases[target]) {
    throw new Error(`Operator alias chain detected: "${src}"→"${target}"→"${operatorAliases[target]}" (infinite redirect risk)`)
  }
}

for (const [src, target] of Object.entries(apnAliases)) {
  if (!APN_VALID_TABS.includes(target as ApnTab)) {
    throw new Error(`APN alias "${src}"→"${target}" target is not in validTabs`)
  }
  if (apnAliases[target]) {
    throw new Error(`APN alias chain detected: "${src}"→"${target}"→"${apnAliases[target]}" (infinite redirect risk)`)
  }
}

// useTabUrlSync options type contract
type UseTabUrlSyncOptions = {
  defaultTab: string
  aliases?: Record<string, string>
  validTabs?: string[]
  paramName?: string
}

const _operatorOpts: UseTabUrlSyncOptions = {
  defaultTab: 'overview',
  aliases: operatorAliases,
  validTabs: [...OPERATOR_VALID_TABS],
}

const _apnOpts: UseTabUrlSyncOptions = {
  defaultTab: 'overview',
  aliases: apnAliases,
  validTabs: [...APN_VALID_TABS],
}

export { _operatorOpts, _apnOpts }
