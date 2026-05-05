// STORY-098 Task 6 — SCR-198 Settings → Log Forwarding (Syslog).
// DTOs mirror backend `syslogDestResponse` / `upsertRequest` shape verbatim
// (internal/api/settings/log_forwarding.go). Pure validation functions are
// exported so the tsc-throw smoke test can assert contract behaviour without
// rendering the React component.

export const SYSLOG_TRANSPORTS = ['udp', 'tcp', 'tls'] as const
export type SyslogTransport = (typeof SYSLOG_TRANSPORTS)[number]

export const SYSLOG_FORMATS = ['rfc3164', 'rfc5424'] as const
export type SyslogFormat = (typeof SYSLOG_FORMATS)[number]

export const SYSLOG_CATEGORIES = [
  'auth',
  'audit',
  'alert',
  'session',
  'policy',
  'imei',
  'system',
] as const
export type SyslogCategory = (typeof SYSLOG_CATEGORIES)[number]

export const SYSLOG_CATEGORY_LABEL: Record<SyslogCategory, string> = {
  auth: 'Auth',
  audit: 'Audit',
  alert: 'Alerts',
  session: 'Sessions',
  policy: 'Policy',
  imei: 'IMEI',
  system: 'System',
}

export const SYSLOG_FACILITIES: ReadonlyArray<{ value: number; label: string }> = [
  { value: 0, label: '0 — kern' },
  { value: 1, label: '1 — user' },
  { value: 2, label: '2 — mail' },
  { value: 3, label: '3 — daemon' },
  { value: 4, label: '4 — auth' },
  { value: 5, label: '5 — syslog' },
  { value: 6, label: '6 — lpr' },
  { value: 7, label: '7 — news' },
  { value: 8, label: '8 — uucp' },
  { value: 9, label: '9 — cron' },
  { value: 10, label: '10 — authpriv' },
  { value: 11, label: '11 — ftp' },
  { value: 16, label: '16 — local0' },
  { value: 17, label: '17 — local1' },
  { value: 18, label: '18 — local2' },
  { value: 19, label: '19 — local3' },
  { value: 20, label: '20 — local4' },
  { value: 21, label: '21 — local5' },
  { value: 22, label: '22 — local6' },
  { value: 23, label: '23 — local7' },
]

export const SYSLOG_SEVERITIES: ReadonlyArray<{ value: number; label: string }> = [
  { value: 0, label: '0 — emergency' },
  { value: 1, label: '1 — alert' },
  { value: 2, label: '2 — critical' },
  { value: 3, label: '3 — error' },
  { value: 4, label: '4 — warning' },
  { value: 5, label: '5 — notice' },
  { value: 6, label: '6 — informational' },
  { value: 7, label: '7 — debug' },
]

// Backend response shape (snake_case wire fields).
export interface SyslogDestination {
  id: string
  tenant_id: string
  name: string
  host: string
  port: number
  transport: SyslogTransport
  format: SyslogFormat
  facility: number
  severity_floor: number | null
  filter_categories: SyslogCategory[]
  filter_min_severity: number | null
  enabled: boolean
  last_delivery_at: string | null
  last_error: string | null
  created_at: string
  updated_at: string
}

// Upsert request body (mirrors backend `upsertRequest`).
// PEM fields stay client-side only (server returns them as null in responses;
// clients send them on create/update).
export interface UpsertSyslogDestinationRequest {
  name: string
  host: string
  port: number
  transport: SyslogTransport
  format: SyslogFormat
  facility: number
  severity_floor: number | null
  filter_categories: SyslogCategory[]
  filter_min_severity: number | null
  tls_ca_pem: string | null
  tls_client_cert_pem: string | null
  tls_client_key_pem: string | null
  enabled: boolean
}

// Test connection request — same shape as upsert (no DB write).
export type TestSyslogConnectionRequest = UpsertSyslogDestinationRequest

export interface TestSyslogConnectionResponse {
  ok: boolean
  error?: string
}

export interface SetEnabledRequest {
  enabled: boolean
}

// ── Form draft + initial state ──────────────────────────────────────────────

export interface DestinationFormDraft {
  name: string
  host: string
  port: number
  transport: SyslogTransport
  format: SyslogFormat
  facility: number
  severity_floor: number | null
  filter_categories: SyslogCategory[]
  filter_min_severity: number | null
  tls_ca_pem: string
  tls_client_cert_pem: string
  tls_client_key_pem: string
  enabled: boolean
}

export const INITIAL_DESTINATION_FORM: DestinationFormDraft = {
  name: '',
  host: '',
  port: 514,
  transport: 'udp',
  format: 'rfc5424',
  facility: 16,
  severity_floor: null,
  filter_categories: ['audit', 'alert'],
  filter_min_severity: null,
  tls_ca_pem: '',
  tls_client_cert_pem: '',
  tls_client_key_pem: '',
  enabled: true,
}

// ── Pure validators (exported for tsc-throw smoke test) ─────────────────────

export type DestinationFormErrors = Partial<Record<keyof DestinationFormDraft | 'tls_pair', string>>

export function requiresTLSGroup(transport: SyslogTransport): boolean {
  return transport === 'tls'
}

/**
 * Mutual TLS rule: client cert and key must both be provided or both absent.
 * Returns null if valid, or an error message string.
 */
export function validateMutualTLS(clientCertPEM: string, clientKeyPEM: string): string | null {
  const certSet = clientCertPEM.trim().length > 0
  const keySet = clientKeyPEM.trim().length > 0
  if (certSet !== keySet) {
    return 'Both client certificate and private key are required, or neither.'
  }
  return null
}

export function isValidPort(port: number): boolean {
  return Number.isInteger(port) && port >= 1 && port <= 65535
}

export function isValidFacility(facility: number): boolean {
  return Number.isInteger(facility) && facility >= 0 && facility <= 23
}

export function isValidSeverity(severity: number | null): boolean {
  if (severity === null) return true
  return Number.isInteger(severity) && severity >= 0 && severity <= 7
}

/**
 * Full form validator. Returns an errors object — empty when valid.
 */
export function validateDestinationForm(form: DestinationFormDraft): DestinationFormErrors {
  const errors: DestinationFormErrors = {}

  if (!form.name.trim()) {
    errors.name = 'Name is required.'
  } else if (form.name.length > 255) {
    errors.name = 'Name must be 255 characters or fewer.'
  }

  if (!form.host.trim()) {
    errors.host = 'Host is required.'
  } else if (form.host.length > 255) {
    errors.host = 'Host must be 255 characters or fewer.'
  }

  if (!isValidPort(form.port)) {
    errors.port = 'Port must be between 1 and 65535.'
  }

  if (!(SYSLOG_TRANSPORTS as readonly string[]).includes(form.transport)) {
    errors.transport = 'Transport must be udp, tcp, or tls.'
  }

  if (!(SYSLOG_FORMATS as readonly string[]).includes(form.format)) {
    errors.format = 'Format must be rfc3164 or rfc5424.'
  }

  if (!isValidFacility(form.facility)) {
    errors.facility = 'Facility must be between 0 and 23.'
  }

  if (!isValidSeverity(form.severity_floor)) {
    errors.severity_floor = 'Severity floor must be between 0 and 7.'
  }

  if (!isValidSeverity(form.filter_min_severity)) {
    errors.filter_min_severity = 'Minimum severity must be between 0 and 7.'
  }

  if (form.filter_categories.length === 0) {
    errors.filter_categories = 'At least one event category must be selected.'
  } else {
    for (const cat of form.filter_categories) {
      if (!(SYSLOG_CATEGORIES as readonly string[]).includes(cat)) {
        errors.filter_categories = `Unknown category "${cat}".`
        break
      }
    }
  }

  if (requiresTLSGroup(form.transport)) {
    const pairErr = validateMutualTLS(form.tls_client_cert_pem, form.tls_client_key_pem)
    if (pairErr) {
      errors.tls_pair = pairErr
    }
  }

  return errors
}

/**
 * Convert form draft → upsert request body.
 * PEM fields are blank-trimmed; empty PEM → null on the wire so the backend
 * treats them as "no TLS material provided".
 */
export function formToUpsertRequest(form: DestinationFormDraft): UpsertSyslogDestinationRequest {
  const blankToNull = (s: string): string | null => {
    const t = s.trim()
    return t.length === 0 ? null : t
  }
  return {
    name: form.name.trim(),
    host: form.host.trim(),
    port: form.port,
    transport: form.transport,
    format: form.format,
    facility: form.facility,
    severity_floor: form.severity_floor,
    filter_categories: form.filter_categories,
    filter_min_severity: form.filter_min_severity,
    tls_ca_pem: blankToNull(form.tls_ca_pem),
    tls_client_cert_pem: blankToNull(form.tls_client_cert_pem),
    tls_client_key_pem: blankToNull(form.tls_client_key_pem),
    enabled: form.enabled,
  }
}

/**
 * Hydrate form draft from an existing destination row (Edit mode).
 * PEM fields are not returned by the API, so they start blank — the user must
 * re-enter them if they want to change TLS material; otherwise the backend
 * keeps the prior values (per upsert semantics).
 */
export function destinationToForm(d: SyslogDestination): DestinationFormDraft {
  return {
    name: d.name,
    host: d.host,
    port: d.port,
    transport: d.transport,
    format: d.format,
    facility: d.facility,
    severity_floor: d.severity_floor,
    filter_categories: [...d.filter_categories],
    filter_min_severity: d.filter_min_severity,
    tls_ca_pem: '',
    tls_client_cert_pem: '',
    tls_client_key_pem: '',
    enabled: d.enabled,
  }
}
