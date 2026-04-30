import { api } from '@/lib/api'

export interface DSLValidationError {
  line: number
  column: number
  severity: 'error' | 'warning' | 'info'
  code: string
  message: string
  snippet?: string
}

export interface DSLValidationResult {
  valid: boolean
  errors?: DSLValidationError[]
  warnings?: DSLValidationError[]
  compiled_rules?: unknown
  formatted_source?: string
}

interface ApiErrorEnvelope {
  status: 'error'
  error: {
    code: string
    message: string
    details?: {
      valid?: boolean
      errors?: DSLValidationError[]
    }
  }
}

interface AxiosLikeError {
  response?: {
    status?: number
    data?: ApiErrorEnvelope
  }
  message?: string
}

function isAxiosLikeError(e: unknown): e is AxiosLikeError {
  return typeof e === 'object' && e !== null && 'response' in e
}

export async function validateDSL(
  dslSource: string,
  options?: { format?: boolean },
): Promise<DSLValidationResult> {
  const url = options?.format ? '/policies/validate?format=true' : '/policies/validate'
  try {
    const res = await api.post<{ status: 'success'; data: DSLValidationResult }>(url, {
      dsl_source: dslSource,
    })
    return res.data.data
  } catch (e) {
    if (isAxiosLikeError(e) && e.response?.status === 422 && e.response.data?.error?.details) {
      const details = e.response.data.error.details
      return {
        valid: false,
        errors: details.errors ?? [],
      }
    }
    throw e
  }
}

export interface DSLVocab {
  match_fields: string[]
  charging_models: string[]
  overage_actions: string[]
  billing_cycles: string[]
  units: string[]
  rule_keywords: string[]
  actions?: string[]
}

let vocabCache: DSLVocab | null = null

export async function fetchVocab(): Promise<DSLVocab> {
  if (vocabCache) return vocabCache
  try {
    const res = await api.get<{ status: 'success'; data: DSLVocab }>('/policies/vocab')
    vocabCache = res.data.data
    return vocabCache
  } catch {
    vocabCache = {
      match_fields: ['apn', 'imsi', 'tenant', 'msisdn', 'rat_type', 'sim_type', 'operator', 'group'],
      charging_models: ['prepaid', 'postpaid', 'hybrid'],
      overage_actions: ['block', 'throttle', 'notify_only', 'charge'],
      billing_cycles: ['daily', 'weekly', 'monthly'],
      units: ['B', 'KB', 'MB', 'GB', 'TB', 'bps', 'kbps', 'mbps', 'gbps', 'ms', 's', 'min', 'h', 'd'],
      rule_keywords: [
        'bandwidth_down',
        'bandwidth_up',
        'rate_limit',
        'time_window',
        'session_timeout',
        'idle_timeout',
        'max_sessions',
        'qos_class',
        'priority',
      ],
      actions: ['notify', 'throttle', 'disconnect', 'log', 'block', 'suspend', 'tag'],
    }
    return vocabCache
  }
}

export function __resetVocabCacheForTests(): void {
  vocabCache = null
}
