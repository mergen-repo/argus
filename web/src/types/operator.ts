// Per-protocol configuration echoed back from the server (decrypted)
// when the Protocols tab fetches a fresh operator detail. Fields match
// the canonical protocol-specific shape enforced by the backend's
// adapterschema.Validate. All fields are optional because the client
// only renders what the server sent — empty objects render as disabled
// cards.
export interface AdapterProtocolConfig {
  enabled?: boolean
  // RADIUS
  shared_secret?: string
  listen_addr?: string
  acct_port?: string | number
  // Diameter
  origin_host?: string
  origin_realm?: string
  peers?: string[]
  product_name?: string
  // SBA
  nrf_url?: string
  nf_instance_id?: string
  tls?: {
    cert_path?: string
    key_path?: string
    ca_path?: string
  }
  // HTTP
  base_url?: string
  auth_type?: 'none' | 'bearer' | 'basic' | 'mtls' | string
  auth_token?: string
  username?: string
  password?: string
  health_path?: string
  // Mock
  latency_ms?: number
  simulated_imsi_count?: number
}

export interface AdapterConfig {
  mock?: AdapterProtocolConfig
  radius?: AdapterProtocolConfig
  diameter?: AdapterProtocolConfig
  sba?: AdapterProtocolConfig
  http?: AdapterProtocolConfig
}

export interface Operator {
  id: string
  name: string
  code: string
  mcc: string
  mnc: string
  // STORY-090 Wave 2 D2-B: the legacy type field is retired. The
  // server emits `enabled_protocols` derived from the nested
  // adapter_config in canonical order (diameter, radius, sba, http,
  // mock). The primary display label is `enabled_protocols[0]`.
  enabled_protocols: string[]
  adapter_config?: AdapterConfig
  supported_rat_types: string[]
  health_status: string
  health_check_interval_sec: number
  failover_policy: string
  failover_timeout_ms: number
  circuit_breaker_threshold: number
  circuit_breaker_recovery_sec: number
  sla_uptime_target?: number
  sla_latency_threshold_ms?: number
  state: string
  created_at: string
  updated_at: string
  sim_count: number
  active_sessions: number
  total_traffic_bytes: number
  last_health_check?: string
}

export interface OperatorHealthDetail {
  health_status: string
  latency_ms?: number
  circuit_state: string
  last_check?: string
  uptime_24h: number
  failure_count: number
}

export interface OperatorTestResult {
  success: boolean
  latency_ms: number
  error?: string
}
