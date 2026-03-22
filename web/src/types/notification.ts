export interface Notification {
  id: string
  tenant_id: string
  type: NotificationType
  category: NotificationCategory
  title: string
  message: string
  severity: 'info' | 'warning' | 'error' | 'critical'
  read: boolean
  read_at?: string
  resource_type?: string
  resource_id?: string
  created_at: string
}

export type NotificationType =
  | 'operator.down'
  | 'operator.degraded'
  | 'operator.recovered'
  | 'sim.state_changed'
  | 'sim.threshold_exceeded'
  | 'policy.activated'
  | 'policy.rollout_complete'
  | 'session.anomaly'
  | 'system.alert'
  | 'job.completed'
  | 'job.failed'
  | string

export type NotificationCategory =
  | 'operator'
  | 'sim'
  | 'policy'
  | 'session'
  | 'system'
  | 'job'
  | string
