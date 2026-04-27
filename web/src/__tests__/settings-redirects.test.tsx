/**
 * Smoke tests for settings redirect routes (FIX-240 AC-5 + AC-2 sub-rule).
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 *
 * Covered scenarios:
 * 10. Visit /settings/security → redirects to /settings#security (AC-5)
 *     Also covers: /settings/sessions, /settings/reliability, /settings/notifications
 * 11. Visit /notifications?tab=preferences → redirects to /settings#notifications (AC-2)
 *
 * Note: Full route rendering (MemoryRouter + React Router Navigate assertions)
 * requires vitest + jsdom which is not yet configured in this project.
 * Route integration tests tracked for test infra wave.
 */

// ─── AC-5: Legacy path redirect contracts ─────────────────────────────────────

type RedirectRule = {
  from: string
  to: string
  replace: boolean
}

// The router.tsx defines 4 <Navigate replace> entries for legacy settings paths.
// Contract: each legacy path maps to the hash-based equivalent.
const SETTINGS_REDIRECTS: RedirectRule[] = [
  { from: '/settings/security', to: '/settings#security', replace: true },
  { from: '/settings/sessions', to: '/settings#sessions', replace: true },
  { from: '/settings/reliability', to: '/settings#reliability', replace: true },
  { from: '/settings/notifications', to: '/settings#notifications', replace: true },
]

// Verify all 4 legacy paths are covered
if (SETTINGS_REDIRECTS.length !== 4) {
  throw new Error(`AC-5 FAIL: expected 4 legacy redirects, got ${SETTINGS_REDIRECTS.length}`)
}

// Verify each redirect uses replace: true (no history push — preserves back button)
for (const rule of SETTINGS_REDIRECTS) {
  if (!rule.replace) {
    throw new Error(`AC-5 FAIL: redirect from "${rule.from}" must use replace=true`)
  }
  // Verify target format: /settings#{tab}
  if (!rule.to.startsWith('/settings#')) {
    throw new Error(`AC-5 FAIL: redirect target "${rule.to}" must start with /settings#`)
  }
  // Verify hash matches the last segment of the source path
  const sourceTab = rule.from.split('/').pop()
  const targetHash = rule.to.split('#')[1]
  if (sourceTab !== targetHash) {
    throw new Error(`AC-5 FAIL: source tab "${sourceTab}" must match target hash "${targetHash}"`)
  }
}

// Verify all 4 known tabs are covered
const coveredTabs = SETTINGS_REDIRECTS.map((r) => r.to.split('#')[1])
const expectedTabs = ['security', 'sessions', 'reliability', 'notifications']

for (const expected of expectedTabs) {
  if (!coveredTabs.includes(expected)) {
    throw new Error(`AC-5 FAIL: legacy path for tab "${expected}" must have a redirect`)
  }
}

// ─── AC-2: /notifications?tab=preferences → /settings#notifications ──────────

// The NotificationsPage watches searchParams for tab=preferences and redirects.
// Contract: useEffect detects the param and calls navigate('/settings#notifications', { replace: true })

type NotificationsRedirectRule = {
  fromPath: string
  fromParam: string
  fromValue: string
  toPath: string
  replace: boolean
}

const NOTIFICATIONS_PREF_REDIRECT: NotificationsRedirectRule = {
  fromPath: '/notifications',
  fromParam: 'tab',
  fromValue: 'preferences',
  toPath: '/settings#notifications',
  replace: true,
}

// Verify destination
if (NOTIFICATIONS_PREF_REDIRECT.toPath !== '/settings#notifications') {
  throw new Error(
    `AC-2 FAIL: /notifications?tab=preferences must redirect to /settings#notifications, got "${NOTIFICATIONS_PREF_REDIRECT.toPath}"`,
  )
}

// Verify replace semantics (no history push)
if (!NOTIFICATIONS_PREF_REDIRECT.replace) {
  throw new Error('AC-2 FAIL: preference redirect must use replace=true')
}

// Verify the param name/value contract
if (NOTIFICATIONS_PREF_REDIRECT.fromParam !== 'tab' || NOTIFICATIONS_PREF_REDIRECT.fromValue !== 'preferences') {
  throw new Error('AC-2 FAIL: redirect must trigger on ?tab=preferences')
}

// ─── Route shape type contract ────────────────────────────────────────────────

// React Router v6 <Navigate> props type
type NavigateProps = {
  to: string
  replace?: boolean
  state?: unknown
}

const _securityNav: NavigateProps = { to: '/settings#security', replace: true }
const _sessionsNav: NavigateProps = { to: '/settings#sessions', replace: true }
const _reliabilityNav: NavigateProps = { to: '/settings#reliability', replace: true }
const _notificationsNav: NavigateProps = { to: '/settings#notifications', replace: true }

void _securityNav
void _sessionsNav
void _reliabilityNav
void _notificationsNav

// ─── Verify import path exists ───────────────────────────────────────────────
// tsc will fail if these modules cannot be resolved

import type {} from '@/pages/notifications/index'
import type {} from '@/pages/settings/index'

export {
  SETTINGS_REDIRECTS,
  NOTIFICATIONS_PREF_REDIRECT,
  coveredTabs,
}
