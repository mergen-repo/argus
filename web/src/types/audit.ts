export interface AuditLog {
  id: number
  user_id?: string
  user_email?: string
  user_name?: string
  action: string
  entity_type: string
  entity_id: string
  diff?: unknown
  ip_address?: string
  created_at: string
}

export interface AuditVerifyResult {
  verified: boolean
  entries_checked: number
  first_invalid?: number
}
