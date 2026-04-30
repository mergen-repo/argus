const TIMEZONE = 'Europe/Istanbul'

const cdrTimeFormatter = new Intl.DateTimeFormat('tr-TR', {
  timeZone: TIMEZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

const shortTimeFormatter = new Intl.DateTimeFormat('tr-TR', {
  timeZone: TIMEZONE,
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

const dayFormatter = new Intl.DateTimeFormat('tr-TR', {
  timeZone: TIMEZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

function toDate(input: string | number | Date): Date | null {
  if (input instanceof Date) return input
  if (typeof input === 'number') return new Date(input)
  if (typeof input === 'string' && input) {
    const d = new Date(input)
    if (!Number.isNaN(d.getTime())) return d
  }
  return null
}

export function formatCDRTimestamp(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined) return '-'
  const d = toDate(input)
  if (!d) return '-'
  const parts = cdrTimeFormatter.formatToParts(d)
  const map = Object.fromEntries(parts.map((p) => [p.type, p.value]))
  return `${map.year}-${map.month}-${map.day} ${map.hour}:${map.minute}:${map.second}`
}

export function formatCDRTime(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined) return '-'
  const d = toDate(input)
  if (!d) return '-'
  return shortTimeFormatter.format(d)
}

export function formatCDRDay(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined) return '-'
  const d = toDate(input)
  if (!d) return '-'
  return dayFormatter.format(d)
}

export function isoUTC(input: string | number | Date): string {
  const d = toDate(input)
  return d ? d.toISOString() : ''
}

export const CDR_TIMEZONE = TIMEZONE
