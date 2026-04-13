export interface RouteMetric {
  method: string
  route: string
  count: number
  p50_ms: number
  p95_ms: number
  p99_ms: number
  error_count: number
  error_rate: number
}

export interface HTTPTotals {
  requests: number
  errors: number
  error_rate: number
}

export interface ByStatus {
  status: string
  count: number
}

export interface HTTPBlock {
  totals: HTTPTotals
  by_route: RouteMetric[]
  by_status: ByStatus[]
}

export interface AAAProtocol {
  protocol: string
  req_per_sec: number
  success_rate: number
  p99_ms: number
}

export interface AAABlock {
  by_protocol: AAAProtocol[]
}

export interface RuntimeBlock {
  goroutines: number
  mem_alloc_bytes: number
  mem_sys_bytes: number
  gc_pause_p99_ms: number
}

export interface JobTypeMetric {
  job_type: string
  runs: number
  success: number
  failed: number
  p50_s: number
  p95_s: number
  p99_s: number
}

export interface JobsBlock {
  by_type: JobTypeMetric[]
}

export interface OpsMetricsSnapshot {
  http: HTTPBlock
  aaa: AAABlock
  runtime: RuntimeBlock
  jobs: JobsBlock
}

export interface DBPool {
  max: number
  in_use: number
  idle: number
  waiting: number
  acquired_total: number
}

export interface TableSize {
  name: string
  size_bytes: number
  row_estimate: number
}

export interface PartitionInfo {
  parent: string
  child: string
  range_from?: string
  range_to?: string
}

export interface DBBlock {
  pool: DBPool
  tables: TableSize[]
  partitions: PartitionInfo[]
  replication_lag_seconds: number | null
  error?: string
}

export interface RedisDBInfo {
  db: number
  keys: number
  expires: number
}

export interface RedisBlock {
  ops_per_sec: number
  hit_rate: number
  miss_rate: number
  memory_used_bytes: number
  memory_max_bytes: number
  evictions_total: number
  connected_clients: number
  latency_p99_ms: number
  keys_by_db: RedisDBInfo[]
  error?: string
}

export interface ConsumerLag {
  consumer: string
  pending: number
  ack_pending: number
  redeliveries: number
  slow: boolean
}

export interface NATSStream {
  name: string
  subjects: string[]
  messages: number
  bytes: number
  consumers: number
  consumer_lag: ConsumerLag[]
}

export interface NATSBlock {
  streams: NATSStream[]
  dlq_depth: number
  error?: string
}

export interface InfraHealth {
  db: DBBlock
  redis: RedisBlock
  nats: NATSBlock
}

export interface Incident {
  ts: string
  anomaly_id: string
  sim_id?: string
  severity: string
  type: string
  action: string
  actor_id?: string
  actor_email?: string
  note?: string
  current_state: string
}

export interface AnomalyComment {
  id: string
  anomaly_id: string
  user_id: string
  user_email: string
  body: string
  created_at: string
}

export interface EscalateRequest {
  note: string
  on_call_user_id?: string
}

export interface IncidentFilters {
  from?: string
  to?: string
  severity?: string
  state?: string
  entity_id?: string
  cursor?: string
  limit?: number
}
