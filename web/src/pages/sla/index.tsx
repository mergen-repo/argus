import { useState, useMemo } from 'react'
import {
  Activity,
  AlertTriangle,
  Clock,
  Download,
  FileBarChart,
  Server,
  ShieldAlert,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { Skeleton } from '@/components/ui/skeleton'
import { AnimatedCounter } from '@/components/ui/animated-counter'
import { Select } from '@/components/ui/select'
import { EmptyState } from '@/components/shared/empty-state'
import { useSLAHistory, useSLAPDFDownload } from '@/hooks/use-sla'
import { SLAMonthDetailPanel } from './month-detail'
import { cn } from '@/lib/utils'
import { classifyUptime, uptimeStatusColor, uptimeStatusLabel, yearOptions } from '@/lib/sla'
import type { SLAMonthSummary } from '@/types/sla'

const MONTH_NAMES = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December',
]

const YEAR_OPTIONS = yearOptions(5)

const ROLLING_OPTIONS = [
  { value: '6', label: '6mo' },
  { value: '12', label: '12mo' },
  { value: '24', label: '24mo' },
]

const BREADCRUMB_ITEMS = [
  { label: 'Platform', href: '/' },
  { label: 'SLA' },
]

interface MonthCardProps {
  summary: SLAMonthSummary
  onClick: () => void
}

function MonthCard({ summary, onClick }: MonthCardProps) {
  const { year, month, overall } = summary
  const status = classifyUptime(overall.uptime_pct, overall.sla_uptime_target)
  const palette = uptimeStatusColor(status)
  const { download, pending } = useSLAPDFDownload()

  const handlePdfClick = (e: React.MouseEvent) => {
    e.stopPropagation()
    void download({ year, month })
  }

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onClick() }}
      className={cn(
        'group flex flex-col rounded-[var(--radius-md)] border border-border bg-bg-surface',
        'cursor-pointer transition-all duration-300 ease-out',
        'hover:-translate-y-0.5 hover:border-border/80',
        palette.glow,
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-primary',
      )}
    >
      <div className="flex items-center justify-between px-4 pt-4 pb-2">
        <div>
          <p className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary font-medium">
            {year}
          </p>
          <h3 className="text-sm font-semibold text-text-primary leading-none mt-0.5">
            {MONTH_NAMES[month - 1]}
          </h3>
        </div>
        <Button
          type="button"
          variant="link"
          size="sm"
          onClick={handlePdfClick}
          disabled={pending}
          className={cn(
            'h-auto p-0 gap-1 font-mono text-[10px] text-accent hover:text-accent/70 hover:no-underline opacity-0 group-hover:opacity-100 focus-visible:opacity-100',
            pending && 'opacity-100 cursor-wait',
          )}
          aria-label={`Download PDF for ${MONTH_NAMES[month - 1]} ${year}`}
        >
          <Download className={cn('h-3 w-3', pending && 'animate-pulse')} />
          {pending ? 'PDF…' : 'PDF'}
        </Button>
      </div>

      <div className="px-4 pb-2">
        <div className="flex items-baseline gap-1.5">
          <span className="font-mono text-xl font-bold tabular-nums text-text-primary">
            {overall.uptime_pct.toFixed(3)}%
          </span>
          <span className="text-[10px] text-text-tertiary">uptime</span>
        </div>
        <div className="mt-1.5 h-1 w-full rounded-full bg-bg-hover overflow-hidden">
          <div
            className={cn('h-full rounded-full transition-all duration-700', palette.bar)}
            style={{ width: `${Math.min(overall.uptime_pct, 100)}%` }}
          />
        </div>
      </div>

      <div className="flex items-center gap-3 px-4 pb-3">
        <div className="flex flex-col">
          <span className="text-[9px] uppercase tracking-wide text-text-tertiary">Incidents</span>
          <span className="font-mono text-sm font-bold text-text-primary tabular-nums">
            {overall.incident_count}
          </span>
        </div>
        <div className="h-6 w-px bg-border" />
        <div className="flex flex-col">
          <span className="text-[9px] uppercase tracking-wide text-text-tertiary">Breach min</span>
          <span className="font-mono text-sm font-bold text-text-primary tabular-nums">
            {overall.breach_minutes}
          </span>
        </div>
      </div>

      <div className="border-t border-border px-4 py-2.5">
        <span
          className={cn(
            'inline-flex items-center rounded px-2 py-0.5 text-[10px] font-mono font-semibold border',
            palette.pill,
          )}
        >
          {uptimeStatusLabel(status)}
        </span>
      </div>
    </div>
  )
}

function MonthCardSkeleton() {
  return (
    <div className="flex flex-col rounded-[var(--radius-md)] border border-border bg-bg-surface p-4 space-y-3">
      <div className="flex items-center justify-between">
        <div className="space-y-1.5">
          <Skeleton className="h-2.5 w-10" />
          <Skeleton className="h-4 w-20" />
        </div>
      </div>
      <div className="space-y-1.5">
        <Skeleton className="h-6 w-28" />
        <Skeleton className="h-1 w-full rounded-full" />
      </div>
      <div className="flex gap-3">
        <Skeleton className="h-8 w-16" />
        <Skeleton className="h-8 w-16" />
      </div>
      <div className="border-t border-border pt-2.5">
        <Skeleton className="h-5 w-20" />
      </div>
    </div>
  )
}

interface KpiCardProps {
  label: string
  value: number
  formatter?: (n: number) => string
  icon: React.ReactNode
  tone?: 'accent' | 'success' | 'warning' | 'danger'
}

function KpiCard({ label, value, formatter, icon, tone = 'accent' }: KpiCardProps) {
  const borderTone: Record<string, string> = {
    accent: 'border-l-accent',
    success: 'border-l-success',
    warning: 'border-l-warning',
    danger: 'border-l-danger',
  }
  const iconTone: Record<string, string> = {
    accent: 'text-accent',
    success: 'text-success',
    warning: 'text-warning',
    danger: 'text-danger',
  }

  return (
    <div className={cn(
      'flex items-center gap-4 px-4 py-4 rounded-[var(--radius-md)] bg-bg-surface border border-border border-l-2',
      borderTone[tone],
    )}>
      <span className={cn('opacity-70 shrink-0', iconTone[tone])}>
        {icon}
      </span>
      <div className="min-w-0">
        <p className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">
          {label}
        </p>
        <p className="font-mono text-xl font-bold text-text-primary leading-none mt-1">
          <AnimatedCounter value={value} formatter={formatter} />
        </p>
      </div>
    </div>
  )
}

export default function SLAReportsPage() {
  const [year, setYear] = useState(String(new Date().getFullYear()))
  const [rolling, setRolling] = useState('6')
  const [selectedMonth, setSelectedMonth] = useState<{ year: number; month: number } | null>(null)

  const { data, isLoading, isError, refetch } = useSLAHistory({
    year: Number(year),
    months: Number(rolling),
  })

  const summaries = useMemo(() => data ?? [], [data])

  const kpis = useMemo(() => {
    if (!summaries.length) return { uptime: 0, incidents: 0, breachMinutes: 0, operators: 0 }
    const avgUptime = summaries.reduce((s, m) => s + m.overall.uptime_pct, 0) / summaries.length
    const totalIncidents = summaries.reduce((s, m) => s + m.overall.incident_count, 0)
    const totalBreach = summaries.reduce((s, m) => s + m.overall.breach_minutes, 0)
    const operatorSet = new Set(summaries.flatMap((m) => m.operators.map((o) => o.operator_id)))
    return {
      uptime: avgUptime,
      incidents: totalIncidents,
      breachMinutes: totalBreach,
      operators: operatorSet.size,
    }
  }, [summaries])

  return (
    <div className="flex flex-col gap-6 p-6 max-w-screen-xl mx-auto">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="space-y-1.5">
          <Breadcrumb items={BREADCRUMB_ITEMS} />
          <h1 className="text-2xl font-bold text-text-primary tracking-tight">
            SLA Reports
          </h1>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <Select
            className="w-24"
            value={year}
            options={YEAR_OPTIONS}
            onChange={(e) => setYear(e.target.value)}
            aria-label="Select year"
          />
          <div
            role="group"
            aria-label="Rolling window selection"
            className="flex items-center rounded-[var(--radius-sm)] border border-border overflow-hidden"
          >
            {ROLLING_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                onClick={() => setRolling(opt.value)}
                className={cn(
                  'px-3 py-1.5 text-xs font-medium transition-colors',
                  rolling === opt.value
                    ? 'bg-accent-dim text-accent border-r border-accent/30'
                    : 'bg-bg-elevated text-text-secondary hover:text-text-primary hover:bg-bg-hover border-r border-border',
                  'last:border-r-0',
                )}
                aria-pressed={rolling === opt.value}
              >
                {opt.label}
              </button>
            ))}
          </div>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => refetch()}
            aria-label="Refresh SLA data"
          >
            <Activity className="h-3.5 w-3.5 mr-1.5" />
            Refresh
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {isLoading ? (
          Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-[var(--radius-md)]" />
          ))
        ) : (
          <>
            <KpiCard
              label="Overall Uptime"
              value={kpis.uptime}
              formatter={(n) => `${n.toFixed(2)}%`}
              icon={<ShieldAlert className="h-5 w-5" />}
              tone="success"
            />
            <KpiCard
              label="Total Incidents"
              value={kpis.incidents}
              icon={<AlertTriangle className="h-5 w-5" />}
              tone="danger"
            />
            <KpiCard
              label="Breach Minutes"
              value={kpis.breachMinutes}
              icon={<Clock className="h-5 w-5" />}
              tone="warning"
            />
            <KpiCard
              label="Operators Tracked"
              value={kpis.operators}
              icon={<Server className="h-5 w-5" />}
              tone="accent"
            />
          </>
        )}
      </div>

      {isError && (
        <div className="flex items-center justify-between gap-3 rounded-[var(--radius-md)] border border-danger/30 bg-danger/10 px-4 py-3">
          <div className="flex items-center gap-2 text-sm text-danger">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            Failed to load SLA history. Check connectivity or try refreshing.
          </div>
          <Button size="sm" variant="ghost" className="text-danger" onClick={() => refetch()}>
            Retry
          </Button>
        </div>
      )}

      <div>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-secondary uppercase tracking-wider">
            Monthly Breakdown
          </h2>
          {!isLoading && !isError && (
            <span className="text-xs text-text-tertiary font-mono">
              {summaries.length} months
            </span>
          )}
        </div>

        {isLoading ? (
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
            {Array.from({ length: Number(rolling) }).map((_, i) => (
              <MonthCardSkeleton key={i} />
            ))}
          </div>
        ) : summaries.length === 0 && !isError ? (
          <EmptyState
            icon={FileBarChart}
            title="No SLA data for this period"
            description={`No monthly summaries found for ${year} (${rolling} months window). Seed the database or check your operator configuration.`}
            ctaLabel="Go to Operators"
            ctaHref="/operators"
          />
        ) : (
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
            {summaries.map((s) => (
              <MonthCard
                key={`${s.year}-${s.month}`}
                summary={s}
                onClick={() => setSelectedMonth({ year: s.year, month: s.month })}
              />
            ))}
          </div>
        )}
      </div>

      {selectedMonth && (
        <SLAMonthDetailPanel
          open={Boolean(selectedMonth)}
          onOpenChange={(v) => { if (!v) setSelectedMonth(null) }}
          year={selectedMonth.year}
          month={selectedMonth.month}
        />
      )}
    </div>
  )
}
