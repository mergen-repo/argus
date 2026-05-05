/**
 * Smoke tests for SCR-198 Settings → Log Forwarding (STORY-098 Task 6).
 * tsc-throw pattern: tsc compiles + module-load executes throw branches.
 * Mirrors the existing settings tests (imei-pools.test.tsx, settings-index.test.tsx).
 *
 * Covered scenarios:
 *  1. Canonical category union has exactly 7 entries.
 *  2. INITIAL_DESTINATION_FORM hits the validator clean (defaults are valid).
 *  3. Empty name → validator surfaces an error on `name`.
 *  4. Mutual TLS XOR: client_cert without client_key → validator returns pair error.
 *  5. Mutual TLS XOR: client_key without client_cert → validator returns pair error.
 *  6. Mutual TLS: both blank or both set → no pair error.
 *  7. requiresTLSGroup() returns true only for transport=tls.
 *  8. Port out-of-range (0, 70000) → validator surfaces port error.
 *  9. validateDestinationForm rejects empty filter_categories.
 * 10. formToUpsertRequest converts blank PEM → null on the wire.
 * 11. destinationToForm round-trips canonical fields and zeroes PEM.
 */

import {
  type DestinationFormDraft,
  type SyslogDestination,
  INITIAL_DESTINATION_FORM,
  SYSLOG_CATEGORIES,
  SYSLOG_FORMATS,
  SYSLOG_TRANSPORTS,
  destinationToForm,
  formToUpsertRequest,
  isValidPort,
  requiresTLSGroup,
  validateDestinationForm,
  validateMutualTLS,
} from '@/types/log-forwarding'

// ─── Scenario 1: 7 canonical categories ─────────────────────────────────────

if (SYSLOG_CATEGORIES.length !== 7) {
  throw new Error(
    `AC-13 FAIL: SYSLOG_CATEGORIES must have 7 entries (auth/audit/alert/session/policy/imei/system), got ${SYSLOG_CATEGORIES.length}`,
  )
}
for (const expected of [
  'auth',
  'audit',
  'alert',
  'session',
  'policy',
  'imei',
  'system',
] as const) {
  if (!(SYSLOG_CATEGORIES as readonly string[]).includes(expected)) {
    throw new Error(`AC-13 FAIL: SYSLOG_CATEGORIES missing "${expected}"`)
  }
}

if (SYSLOG_TRANSPORTS.length !== 3) {
  throw new Error(`AC-13 FAIL: SYSLOG_TRANSPORTS must have 3 entries, got ${SYSLOG_TRANSPORTS.length}`)
}
if (SYSLOG_FORMATS.length !== 2) {
  throw new Error(`AC-13 FAIL: SYSLOG_FORMATS must have 2 entries, got ${SYSLOG_FORMATS.length}`)
}

// ─── Scenario 2: defaults are valid ─────────────────────────────────────────

const defaultErrs = validateDestinationForm({
  ...INITIAL_DESTINATION_FORM,
  name: 'siem-prod',
  host: 'splunk.corp.example.net',
})
if (Object.keys(defaultErrs).length !== 0) {
  throw new Error(
    `Validation FAIL: defaults with name+host should be valid; got ${JSON.stringify(defaultErrs)}`,
  )
}

// ─── Scenario 3: empty name surfaces error ──────────────────────────────────

const emptyNameErrs = validateDestinationForm({
  ...INITIAL_DESTINATION_FORM,
  name: '',
  host: 'host.example',
})
if (!emptyNameErrs.name) {
  throw new Error('Validation FAIL: empty name must surface an error on `name`')
}

const blankNameErrs = validateDestinationForm({
  ...INITIAL_DESTINATION_FORM,
  name: '   ',
  host: 'host.example',
})
if (!blankNameErrs.name) {
  throw new Error('Validation FAIL: whitespace-only name must surface an error on `name`')
}

// ─── Scenario 4 & 5: Mutual TLS XOR ─────────────────────────────────────────

const certOnly = validateMutualTLS('-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----', '')
if (certOnly === null) {
  throw new Error('Validation FAIL: client_cert without client_key must return a pair error')
}

const keyOnly = validateMutualTLS('', '-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----')
if (keyOnly === null) {
  throw new Error('Validation FAIL: client_key without client_cert must return a pair error')
}

// ─── Scenario 6: Mutual TLS — both set or both blank → ok ───────────────────

if (validateMutualTLS('', '') !== null) {
  throw new Error('Validation FAIL: both blank PEM must be valid')
}
if (
  validateMutualTLS(
    '-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----',
    '-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----',
  ) !== null
) {
  throw new Error('Validation FAIL: both PEM set must be valid')
}

const tlsXorForm: DestinationFormDraft = {
  ...INITIAL_DESTINATION_FORM,
  name: 'tls-mismatch',
  host: 'tls.example',
  transport: 'tls',
  tls_client_cert_pem: '-----BEGIN CERTIFICATE-----\nz\n-----END CERTIFICATE-----',
  tls_client_key_pem: '',
}
const tlsXorErrs = validateDestinationForm(tlsXorForm)
if (!tlsXorErrs.tls_pair) {
  throw new Error('Validation FAIL: TLS XOR mismatch must surface a `tls_pair` error')
}

// ─── Scenario 7: requiresTLSGroup ───────────────────────────────────────────

if (requiresTLSGroup('tls') !== true) {
  throw new Error('requiresTLSGroup FAIL: tls must reveal the TLS group')
}
if (requiresTLSGroup('udp') !== false) {
  throw new Error('requiresTLSGroup FAIL: udp must hide the TLS group')
}
if (requiresTLSGroup('tcp') !== false) {
  throw new Error('requiresTLSGroup FAIL: tcp must hide the TLS group')
}

// ─── Scenario 8: port range ─────────────────────────────────────────────────

if (isValidPort(0)) throw new Error('isValidPort FAIL: 0 must be invalid')
if (isValidPort(70000)) throw new Error('isValidPort FAIL: 70000 must be invalid')
if (!isValidPort(514)) throw new Error('isValidPort FAIL: 514 must be valid')
if (!isValidPort(65535)) throw new Error('isValidPort FAIL: 65535 must be valid')

const badPortForm = validateDestinationForm({
  ...INITIAL_DESTINATION_FORM,
  name: 'bad-port',
  host: 'host.example',
  port: 0,
})
if (!badPortForm.port) {
  throw new Error('Validation FAIL: port=0 must surface a port error')
}

// ─── Scenario 9: empty filter_categories ────────────────────────────────────

const emptyCatForm = validateDestinationForm({
  ...INITIAL_DESTINATION_FORM,
  name: 'no-cats',
  host: 'host.example',
  filter_categories: [],
})
if (!emptyCatForm.filter_categories) {
  throw new Error('Validation FAIL: empty filter_categories must surface an error')
}

// ─── Scenario 10: formToUpsertRequest blanks PEM → null ─────────────────────

const wireBlank = formToUpsertRequest({
  ...INITIAL_DESTINATION_FORM,
  name: 'wire',
  host: 'host.example',
  tls_ca_pem: '',
  tls_client_cert_pem: '   ',
  tls_client_key_pem: '\n  \n',
})
if (wireBlank.tls_ca_pem !== null) {
  throw new Error('Wire FAIL: empty tls_ca_pem must serialise to null')
}
if (wireBlank.tls_client_cert_pem !== null) {
  throw new Error('Wire FAIL: whitespace-only tls_client_cert_pem must serialise to null')
}
if (wireBlank.tls_client_key_pem !== null) {
  throw new Error('Wire FAIL: whitespace-only tls_client_key_pem must serialise to null')
}

const wireSet = formToUpsertRequest({
  ...INITIAL_DESTINATION_FORM,
  name: ' wire-set ',
  host: '  host.example  ',
  tls_ca_pem: '-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----',
})
if (wireSet.name !== 'wire-set') throw new Error('Wire FAIL: name must be trimmed')
if (wireSet.host !== 'host.example') throw new Error('Wire FAIL: host must be trimmed')
if (typeof wireSet.tls_ca_pem !== 'string' || !wireSet.tls_ca_pem.includes('CERTIFICATE')) {
  throw new Error('Wire FAIL: non-blank tls_ca_pem must round-trip')
}

// ─── Scenario 11: destinationToForm round-trip ──────────────────────────────

const sample: SyslogDestination = {
  id: '00000000-0000-0000-0000-000000000001',
  tenant_id: '00000000-0000-0000-0000-000000000002',
  name: 'siem-prod',
  host: 'splunk.corp.example.net',
  port: 6514,
  transport: 'tls',
  format: 'rfc5424',
  facility: 16,
  severity_floor: 6,
  filter_categories: ['audit', 'alert', 'session'],
  filter_min_severity: null,
  enabled: true,
  last_delivery_at: '2026-04-26T14:02:30Z',
  last_error: null,
  created_at: '2026-04-20T09:00:00Z',
  updated_at: '2026-04-26T14:02:30Z',
}
const draft = destinationToForm(sample)
if (draft.name !== sample.name) throw new Error('Hydrate FAIL: name mismatch')
if (draft.transport !== 'tls') throw new Error('Hydrate FAIL: transport mismatch')
if (draft.tls_ca_pem !== '') throw new Error('Hydrate FAIL: PEM must start blank (server never returns PEM)')
if (draft.tls_client_cert_pem !== '') throw new Error('Hydrate FAIL: cert PEM must start blank')
if (draft.tls_client_key_pem !== '') throw new Error('Hydrate FAIL: key PEM must start blank')
if (draft.filter_categories.length !== 3) throw new Error('Hydrate FAIL: filter_categories length mismatch')
if (draft.filter_categories === sample.filter_categories) {
  throw new Error('Hydrate FAIL: filter_categories must be a copy, not a shared reference')
}

// ─── Type-level export checks ───────────────────────────────────────────────

const _draftCheck: DestinationFormDraft = INITIAL_DESTINATION_FORM
void _draftCheck

export {}
