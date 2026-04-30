export type UptimeStatus = 'compliant' | 'at_risk' | 'breached'

export function classifyUptime(uptime: number, target: number): UptimeStatus {
  if (uptime >= target) return 'compliant'
  if (uptime >= target - 0.5) return 'at_risk'
  return 'breached'
}

export function uptimeStatusColor(status: UptimeStatus): {
  text: string
  bar: string
  pill: string
  glow: string
} {
  if (status === 'compliant') {
    return {
      text: 'text-success',
      bar: 'bg-success',
      pill: 'bg-success/15 text-success border-success/30',
      glow: 'hover:shadow-[var(--shadow-card-success)]',
    }
  }
  if (status === 'at_risk') {
    return {
      text: 'text-warning',
      bar: 'bg-warning',
      pill: 'bg-warning/15 text-warning border-warning/30',
      glow: 'hover:shadow-[var(--shadow-card-warning)]',
    }
  }
  return {
    text: 'text-danger',
    bar: 'bg-danger',
    pill: 'bg-danger/15 text-danger border-danger/30',
    glow: 'hover:shadow-[var(--shadow-card-danger)]',
  }
}

export function uptimeStatusLabel(status: UptimeStatus): string {
  if (status === 'compliant') return 'Compliant'
  if (status === 'at_risk') return 'At Risk'
  return 'Breached'
}

export function yearOptions(span = 5): { value: string; label: string }[] {
  const current = new Date().getFullYear()
  return Array.from({ length: span }, (_, i) => {
    const y = String(current - (span - 1) + i)
    return { value: y, label: y }
  })
}
