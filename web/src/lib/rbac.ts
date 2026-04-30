export const ROLE_LEVELS: Record<string, number> = {
  api_user: 1,
  analyst: 2,
  policy_editor: 3,
  sim_manager: 4,
  operator_manager: 5,
  tenant_admin: 6,
  super_admin: 7,
}

export type MinRole = keyof typeof ROLE_LEVELS

export function hasMinRole(userRole: string | undefined, minRole: string): boolean {
  if (!userRole) return false
  return (ROLE_LEVELS[userRole] ?? 0) >= (ROLE_LEVELS[minRole] ?? 99)
}
