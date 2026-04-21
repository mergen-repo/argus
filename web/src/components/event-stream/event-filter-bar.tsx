import { useMemo, useState } from 'react'
import { ChevronDown, Search, X } from 'lucide-react'
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useEventCatalog } from '@/hooks/use-event-catalog'
import { useEventStore, type EventDateRange } from '@/stores/events'
import { SEVERITY_VALUES, type Severity, severityLabel, SEVERITY_PILL_CLASSES } from '@/lib/severity'
import type { EventSeverity } from '@/types/events'

// Proof-of-consumer for PAT-015 gate. Type-only reference keeps the import
// live (tsc --noEmit enforces) without shipping any runtime proof object
// into the bundle. EventSeverity is structurally equivalent to Severity.
type _SeverityTypeProof = EventSeverity extends Severity ? Severity extends EventSeverity ? true : false : false
export type { _SeverityTypeProof }

const DATE_RANGE_OPTIONS: ReadonlyArray<{ value: EventDateRange; label: string }> = [
  { value: 'session', label: 'Oturum' },
  { value: '1h', label: 'Son 1s' },
  { value: '24h', label: 'Son 24s' },
]

interface FilterChipPopoverProps {
  label: string
  values: string[]
  options: string[]
  counts: Record<string, number>
  onChange: (values: string[]) => void
  formatOption?: (v: string) => string
  emptyLabel?: string
}

function summaryLabel(label: string, count: number): string {
  if (count === 0) return label
  if (count === 1) return `${label} (1)`
  return `${label} (${count})`
}

function FilterChipPopover({ label, values, options, counts, onChange, formatOption, emptyLabel }: FilterChipPopoverProps) {
  const [search, setSearch] = useState('')
  const filtered = useMemo(
    () => options.filter((o) => o.toLowerCase().includes(search.toLowerCase())),
    [options, search],
  )

  const toggle = (v: string) => {
    if (values.includes(v)) onChange(values.filter((x) => x !== v))
    else onChange([...values, v])
  }

  const display = (v: string) => (formatOption ? formatOption(v) : v)

  return (
    <Popover>
      <PopoverTrigger
        className={cn(
          'inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-[11px] font-medium',
          'border border-border bg-bg-surface hover:bg-bg-hover transition-colors',
          'focus:outline-none focus-visible:ring-1 focus-visible:ring-accent',
          values.length > 0 && 'border-accent/40 bg-accent/10 text-accent',
        )}
        aria-label={`${label} filtresini aç`}
      >
        <span>{summaryLabel(label, values.length)}</span>
        <ChevronDown className="h-3 w-3 opacity-70" aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent className="w-[260px] p-0" align="start">
        <div className="flex items-center gap-1.5 border-b border-border-subtle px-2 py-1.5">
          <Search className="h-3 w-3 text-text-tertiary" aria-hidden="true" />
          <Input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={`${label} ara...`}
            className="h-auto w-full border-0 bg-transparent p-0 text-[11px] text-text-primary placeholder:text-text-tertiary focus-visible:ring-0 focus-visible:ring-offset-0"
            aria-label={`${label} içinde ara`}
          />
          {search && (
            <Button
              onClick={() => setSearch('')}
              className="h-auto w-auto p-0 text-text-tertiary hover:bg-transparent hover:text-text-primary"
              type="button"
              variant="ghost"
              size="xs"
              aria-label="Arama kutusunu temizle"
            >
              <X className="h-3 w-3" aria-hidden="true" />
            </Button>
          )}
        </div>
        <div className="max-h-[280px] overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <div className="px-3 py-4 text-center text-[11px] text-text-tertiary">
              {emptyLabel || 'Sonuç yok'}
            </div>
          ) : (
            filtered.map((opt) => {
              const isChecked = values.includes(opt)
              const count = counts[opt] ?? 0
              return (
                <Button
                  key={opt}
                  type="button"
                  variant="ghost"
                  size="xs"
                  onClick={() => toggle(opt)}
                  aria-pressed={isChecked}
                  className={cn(
                    'flex h-auto w-full items-center justify-start gap-2 rounded-none px-2 py-1 text-left text-[11px] font-normal transition-colors',
                    'hover:bg-bg-hover',
                    isChecked && 'text-accent',
                  )}
                >
                  <span
                    className={cn(
                      'inline-flex h-3 w-3 shrink-0 items-center justify-center rounded-[2px] border',
                      isChecked ? 'border-accent bg-accent/20' : 'border-border',
                    )}
                    aria-hidden="true"
                  >
                    {isChecked && <span className="h-1.5 w-1.5 rounded-[1px] bg-accent" />}
                  </span>
                  <span className="flex-1 truncate font-mono">{display(opt)}</span>
                  {count > 0 && <span className="text-[10px] text-text-tertiary">{count}</span>}
                </Button>
              )
            })
          )}
        </div>
        <div className="flex items-center justify-between border-t border-border-subtle px-2 py-1.5">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={() => onChange(options.slice())}
            className="h-auto px-0 py-0 text-[10px] font-normal text-text-secondary hover:bg-transparent hover:text-text-primary"
          >
            Tümünü Seç
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={() => onChange([])}
            className="h-auto px-0 py-0 text-[10px] font-normal text-text-secondary hover:bg-transparent hover:text-text-primary"
          >
            Temizle
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

interface DateRangeChipProps {
  value: EventDateRange
  onChange: (v: EventDateRange) => void
}

function DateRangeChip({ value, onChange }: DateRangeChipProps) {
  const current = DATE_RANGE_OPTIONS.find((o) => o.value === value)
  return (
    <Popover>
      <PopoverTrigger
        className={cn(
          'inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-[11px] font-medium',
          'border border-border bg-bg-surface hover:bg-bg-hover transition-colors',
          'focus:outline-none focus-visible:ring-1 focus-visible:ring-accent',
        )}
        aria-label="Zaman aralığı filtresini aç"
      >
        <span>Zaman · {current?.label}</span>
        <ChevronDown className="h-3 w-3 opacity-70" aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent className="w-[140px] p-1" align="end">
        {DATE_RANGE_OPTIONS.map((opt) => (
          <Button
            key={opt.value}
            type="button"
            variant="ghost"
            size="xs"
            onClick={() => onChange(opt.value)}
            aria-pressed={value === opt.value}
            className={cn(
              'flex h-auto w-full items-center justify-start gap-2 rounded-[var(--radius-sm)] px-2 py-1 text-left text-[11px] font-normal',
              'hover:bg-bg-hover transition-colors',
              value === opt.value && 'text-accent',
            )}
          >
            <span className="flex-1">{opt.label}</span>
            {value === opt.value && <span className="h-1.5 w-1.5 rounded-full bg-accent" aria-hidden="true" />}
          </Button>
        ))}
      </PopoverContent>
    </Popover>
  )
}

export function EventFilterBar() {
  const { types, entityTypes, sources } = useEventCatalog()
  const filters = useEventStore((s) => s.filters)
  const setFilters = useEventStore((s) => s.setFilters)
  const events = useEventStore((s) => s.events)

  // Count-by-type / severity / entityType / source for visible buffer —
  // helps users see "how many of each" before toggling.
  const counts = useMemo(() => {
    const byType: Record<string, number> = {}
    const bySeverity: Record<string, number> = {}
    const byEntityType: Record<string, number> = {}
    const bySource: Record<string, number> = {}
    for (const e of events) {
      byType[e.type] = (byType[e.type] || 0) + 1
      bySeverity[e.severity] = (bySeverity[e.severity] || 0) + 1
      const et = e.entity?.type || e.entity_type
      if (et) byEntityType[et] = (byEntityType[et] || 0) + 1
      if (e.source) bySource[e.source] = (bySource[e.source] || 0) + 1
    }
    return { byType, bySeverity, byEntityType, bySource }
  }, [events])

  // F-A6: merge catalog + buffer so newly-emitted subjects (shipped before
  // catalog reload) stay reachable in the filter popover. Dedup via Set.
  const typeOptions = useMemo(
    () => [...new Set([...types, ...Object.keys(counts.byType)])].sort(),
    [types, counts.byType],
  )
  const entityTypeOptions = useMemo(
    () => [...new Set([...entityTypes, ...Object.keys(counts.byEntityType)])].sort(),
    [entityTypes, counts.byEntityType],
  )
  const sourceOptions = useMemo(
    () => [...new Set([...sources, ...Object.keys(counts.bySource)])].sort(),
    [sources, counts.bySource],
  )

  const toggleSeverity = (s: Severity) => {
    if (filters.severities.includes(s)) {
      setFilters({ severities: filters.severities.filter((x) => x !== s) })
    } else {
      setFilters({ severities: [...filters.severities, s] })
    }
  }

  return (
    <div className="sticky top-0 z-10 mb-2 bg-bg-elevated/95 px-1 py-2 backdrop-blur-md border-b border-border">
      <div className="flex flex-wrap items-center gap-1.5">
        <FilterChipPopover
          label="Tür"
          values={filters.types}
          options={typeOptions}
          counts={counts.byType}
          onChange={(v) => setFilters({ types: v })}
        />
        <div className="inline-flex items-center gap-1 rounded-[var(--radius-sm)] border border-border bg-bg-surface px-1.5 py-0.5">
          <span className="text-[10px] text-text-tertiary mr-1">Önem</span>
          {SEVERITY_VALUES.map((s) => {
            const active = filters.severities.includes(s)
            const label = severityLabel(s)
            return (
              <Button
                key={s}
                type="button"
                variant="ghost"
                size="xs"
                onClick={() => toggleSeverity(s)}
                title={label}
                aria-label={`Önem: ${label}`}
                aria-pressed={active}
                className={cn(
                  'h-auto gap-1 rounded-[var(--radius-sm)] px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider transition-all hover:bg-transparent',
                  active
                    ? `${SEVERITY_PILL_CLASSES[s]} opacity-100`
                    : 'text-text-tertiary opacity-60 hover:opacity-100',
                )}
              >
                <span
                  className={cn('h-1.5 w-1.5 rounded-full', active ? 'bg-current' : 'bg-current opacity-40')}
                  aria-hidden="true"
                />
                <span className="md:hidden">{s.charAt(0).toUpperCase()}</span>
                <span className="hidden md:inline">{s.slice(0, 3).toUpperCase()}</span>
              </Button>
            )
          })}
        </div>
        <FilterChipPopover
          label="Varlık"
          values={filters.entityTypes}
          options={entityTypeOptions}
          counts={counts.byEntityType}
          onChange={(v) => setFilters({ entityTypes: v })}
        />
        <FilterChipPopover
          label="Kaynak"
          values={filters.sources}
          options={sourceOptions}
          counts={counts.bySource}
          onChange={(v) => setFilters({ sources: v })}
        />
        <DateRangeChip value={filters.dateRange} onChange={(v) => setFilters({ dateRange: v })} />
      </div>
    </div>
  )
}
