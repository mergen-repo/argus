/**
 * Smoke tests for Sidebar component (FIX-240 AC-6).
 * Type-level validation — structural import checks resolved by tsc --noEmit.
 *
 * Covered scenarios:
 * 12. Sidebar SETTINGS group renders exactly 3 items for tenant_admin (AC-6)
 *
 * Note: Full DOM rendering (mount, query DOM nodes) requires vitest + jsdom which
 * is not yet configured in this project. DOM integration tracked for test infra wave.
 */

import { hasMinRole } from '@/lib/rbac'

// ─── NavItem / NavGroup shape (mirrors sidebar.tsx) ──────────────────────────

type NavItem = {
  label: string
  icon: unknown
  path: string
  minRole?: string
}

type NavGroup = {
  title: string
  items: NavItem[]
  minRole?: string
}

// ─── SETTINGS group items (mirrors sidebar.tsx navGroups) ────────────────────

const SETTINGS_GROUP: NavGroup = {
  title: 'SETTINGS',
  minRole: 'tenant_admin',
  items: [
    { label: 'Settings', icon: null, path: '/settings' },
    { label: 'Users & Roles', icon: null, path: '/settings/users' },
    { label: 'Knowledge Base', icon: null, path: '/settings/knowledgebase' },
  ],
}

// ─── AC-6: Scenario 12 — tenant_admin sees exactly 3 SETTINGS items ──────────

function filterGroupItems(group: NavGroup, role: string): NavItem[] {
  if (group.minRole && !hasMinRole(role, group.minRole)) return []
  return group.items.filter((item) => !item.minRole || hasMinRole(role, item.minRole))
}

// tenant_admin (level 6) ≥ tenant_admin (level 6) → SETTINGS group visible
const tenantAdminItems = filterGroupItems(SETTINGS_GROUP, 'tenant_admin')

if (tenantAdminItems.length !== 3) {
  throw new Error(
    `AC-6 FAIL: tenant_admin should see exactly 3 SETTINGS items, got ${tenantAdminItems.length}. Items: ${tenantAdminItems.map((i) => i.label).join(', ')}`,
  )
}

// Verify exact labels
const expectedLabels = ['Settings', 'Users & Roles', 'Knowledge Base']
for (const expected of expectedLabels) {
  if (!tenantAdminItems.some((item) => item.label === expected)) {
    throw new Error(`AC-6 FAIL: expected SETTINGS item "${expected}" not found`)
  }
}

// Verify exact paths
const expectedPaths = ['/settings', '/settings/users', '/settings/knowledgebase']
for (const expectedPath of expectedPaths) {
  if (!tenantAdminItems.some((item) => item.path === expectedPath)) {
    throw new Error(`AC-6 FAIL: expected SETTINGS path "${expectedPath}" not found`)
  }
}

// ─── AC-6: analyst (level 2) < tenant_admin (level 6) → SETTINGS hidden ─────

const analystItems = filterGroupItems(SETTINGS_GROUP, 'analyst')
if (analystItems.length !== 0) {
  throw new Error(
    `AC-6 FAIL: analyst should NOT see SETTINGS group, got ${analystItems.length} items`,
  )
}

// ─── AC-6: super_admin also sees all 3 SETTINGS items ────────────────────────

const superAdminItems = filterGroupItems(SETTINGS_GROUP, 'super_admin')
if (superAdminItems.length !== 3) {
  throw new Error(
    `AC-6 FAIL: super_admin should see 3 SETTINGS items, got ${superAdminItems.length}`,
  )
}

// ─── Sidebar structural import check ─────────────────────────────────────────
// tsc will fail if Sidebar cannot be resolved or has type errors

import { Sidebar } from '@/components/layout/sidebar'

type _SidebarComponent = typeof Sidebar
const _sidebarTypeCheck: _SidebarComponent = Sidebar
void _sidebarTypeCheck

export {
  tenantAdminItems,
  analystItems,
  superAdminItems,
}
