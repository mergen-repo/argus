export interface TenantUser {
  id: string
  tenant_id: string
  email: string
  name: string
  role: string
  status: 'active' | 'invited' | 'deactivated'
  last_login_at?: string
  created_at: string
}

export interface ApiKey {
  id: string
  tenant_id: string
  name: string
  prefix: string
  scopes: string[]
  rate_limit: number
  expires_at?: string
  last_used_at?: string
  created_at: string
}

export interface ApiKeyCreateResult {
  id: string
  name: string
  key: string
  prefix: string
  scopes: string[]
  rate_limit: number
  expires_at?: string
  created_at: string
}

export interface IpPool {
  id: string
  tenant_id: string
  apn_id?: string
  name: string
  cidr: string
  total_addresses: number
  used_addresses: number
  available_addresses: number
  created_at: string
}

export interface IpAddress {
  id: string
  pool_id: string
  address_v4?: string
  address_v6?: string
  allocation_type: string
  state: string
  sim_id?: string
  sim_iccid?: string
  sim_imsi?: string
  sim_msisdn?: string
  allocated_at?: string
}

export interface NotificationConfig {
  channels: {
    email: boolean
    telegram: boolean
    webhook: boolean
    sms: boolean
  }
  subscriptions: EventSubscription[]
  thresholds: ThresholdConfig[]
}

export interface EventSubscription {
  category: string
  events: {
    event: string
    label: string
    enabled: boolean
  }[]
}

export interface ThresholdConfig {
  key: string
  label: string
  value: number
  min: number
  max: number
  unit: string
}

export interface ServiceHealth {
  name: string
  status: 'healthy' | 'degraded' | 'down'
  latency_ms: number
  message?: string
}

export interface SystemMetrics {
  auth_per_sec: number
  active_sessions: number
  error_rate?: number
  auth_error_rate?: number
  latency: {
    p50: number
    p95: number
    p99: number
  }
  services?: ServiceHealth[]
  system_status?: string
}

export interface Tenant {
  id: string
  name: string
  slug: string
  plan?: string
  sim_count: number
  user_count: number
  retention_days: number
  max_sims: number
  max_users: number
  max_api_keys: number
  created_at: string
  updated_at: string
}
