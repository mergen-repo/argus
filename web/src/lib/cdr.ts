// recordTypeBadgeClass returns the tone class for a CDR record_type Badge.
// Taxonomy is aligned with the backend consumer (start/interim/stop) plus the
// anomaly sub-types (auth/auth_fail/reject) surfaced in the CDR Explorer.
export function recordTypeBadgeClass(recordType: string): string {
  switch (recordType) {
    case 'start':
      return 'bg-accent-dim text-accent'
    case 'interim':
    case 'update':
      return 'bg-info-dim text-info'
    case 'stop':
      return 'bg-success-dim text-success'
    case 'auth':
      return 'bg-warning-dim text-warning'
    case 'auth_fail':
    case 'reject':
      return 'bg-danger-dim text-danger'
    default:
      return 'bg-bg-elevated text-text-secondary'
  }
}
