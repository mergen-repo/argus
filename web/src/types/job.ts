export interface Job {
  id: string
  tenant_id: string
  type: string
  state: JobState
  priority: number
  total_items: number
  processed_items: number
  failed_items: number
  progress_pct: number
  error_report?: unknown
  result?: unknown
  max_retries: number
  retry_count: number
  started_at?: string
  completed_at?: string
  created_at: string
  created_by?: string
  created_by_name?: string
  created_by_email?: string
  is_system?: boolean
  duration?: string
  locked_by?: string
}

export type JobState = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface JobProgressEvent {
  job_id: string
  job_type: string
  state: string
  total_items: number
  processed_items: number
  failed_items: number
  progress_pct: number
  estimated_remaining_sec?: number
  items_per_second?: number
  started_at: string
}

export interface JobCompletedEvent {
  job_id: string
  job_type: string
  final_state: string
  total_items: number
  processed_items: number
  failed_items: number
  success_items: number
  progress_pct: number
  duration_sec: number
  started_at: string
  completed_at: string
  error_report_available: boolean
  result_summary?: string
}

export interface JobError {
  sim_id?: string
  iccid?: string
  error_code: string
  error_message: string
  row?: number
}
