import { useState, useMemo, useEffect, useCallback } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Activity,
  Clock,
  Download,
  FileBarChart,
  Filter,
  HardDrive,
  RefreshCw,
  Search,
  Users,
  X,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { EmptyState } from '@/components/shared/empty-state'
import { EntityLink } from '@/components/shared/entity-link'
import { CopyableId } from '@/components/shared/copyable-id'
import { SimSearch } from '@/components/ui/sim-search'
import { RATBadge } from '@/components/ui/rat-badge'
import { TimeframeSelector, type TimeframeValue, type TimeframePreset } from '@/components/ui/timeframe-selector'
import { useTimeframeUrlSync } from '@/hooks/use-timeframe-url-sync'
import { useOperatorList } from '@/hooks/use-operators'
import { useAPNList } from '@/hooks/use-apns'
import { useCDRList, useCDRStats, useSimBatch, type CDRFilters } from '@/hooks/use-cdrs'
import { useExport } from '@/hooks/use-export'
import { useAuthStore } from '@/stores/auth'
import { formatBytes, formatNumber } from '@/lib/format'
import { formatCDRTimestamp } from '@/lib/time'
import { recordTypeBadgeClass } from '@/lib/cdr'
import { SessionTimelineDrawer } from '@/components/cdrs/session-timeline-drawer'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'

const FILTER_STORAGE_KEY = 'argus.cdrs.filters.v1'
const RECORD_TYPES = ['start', 'interim', 'stop', 'auth', 'auth_fail', 'reject']
const RAT_TYPES = ['nb_iot', 'lte_m', 'lte', 'nr_5g']
const ADMIN_ROLES = new Set(['super_admin', 'tenant_admin'])

const CDR_TIMEFRAME_OPTIONS = [
  { value: '1h' as TimeframePreset, label: '1h' },
  { value: '6h' as TimeframePreset, label: '6h' },
  { value: '24h' as TimeframePreset, label: '24h' },
  { value: '7d' as TimeframePreset, label: '7d' },
  { value: '30d' as TimeframePreset, label: '30d' },
]

function presetToRange(preset: TimeframePreset): { from: string; to: string } {
  const now = Date.now()
  const offsets: Record<string, number> = {
    '1h': 60 * 60 * 1000,
    '6h': 6 * 60 * 60 * 1000,
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
    '30d': 30 * 24 * 60 * 60 * 1000,
  }
  const offset = offsets[preset] ?? offsets['24h']
  return { from: new Date(now - offset).toISOString(), to: new Date(now).toISOString() }
}

function initialFilters(): CDRFilters {
  try {
    const stored = localStorage.getItem(FILTER_STORAGE_KEY)
    if (stored) {
      const parsed = JSON.parse(stored) as CDRFilters
      const from = parsed.from ? new Date(parsed.from) : null
      if (from && (Date.now() - from.getTime()) > 60 * 60 * 1000) {
        const range = presetToRange('24h')
        return {
          operator_id: parsed.operator_id,
          apn_id: parsed.apn_id,
          record_type: parsed.record_type,
          rat_type: parsed.rat_type,
          from: range.from,
          to: range.to,
        }
      }
      return parsed
    }
  } catch {
    /* ignore */
  }
  const range = presetToRange('24h')
  return { from: range.from, to: range.to }
}

export default function CDRExplorerPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const user = useAuthStore((s) => s.user)
  const isAdmin = user ? ADMIN_ROLES.has(user.role) : false

  const { timeframe, setTimeframe, customRange, setCustomRange } = useTimeframeUrlSync('24h')

  const tfValue: TimeframeValue = customRange
    ? { value: 'custom', from: customRange.start, to: customRange.end }
    : { value: timeframe }

  const [filters, setFiltersState] = useState<CDRFilters>(() => {
    const params = Object.fromEntries(searchParams.entries())
    if (Object.keys(params).length > 0) {
      const fromQ: CDRFilters = {}
      if (params.session_id) fromQ.session_id = params.session_id
      if (params.sim_id) fromQ.sim_id = params.sim_id
      if (params.operator_id) fromQ.operator_id = params.operator_id
      if (params.apn_id) fromQ.apn_id = params.apn_id
      if (params.record_type) fromQ.record_type = params.record_type
      if (params.rat_type) fromQ.rat_type = params.rat_type
    }
    return initialFilters()
  })

  useEffect(() => {
    if (tfValue.value === 'custom' && tfValue.from && tfValue.to) {
      setFiltersState((prev) => ({ ...prev, from: tfValue.from, to: tfValue.to }))
    } else if (tfValue.value !== 'custom') {
      const range = presetToRange(tfValue.value)
      setFiltersState((prev) => ({ ...prev, from: range.from, to: range.to }))
    }
  }, [tfValue.value, tfValue.from, tfValue.to])

  const setFilters = useCallback((updater: CDRFilters | ((prev: CDRFilters) => CDRFilters)) => {
    setFiltersState((prev) => {
      const next = typeof updater === 'function' ? (updater as (p: CDRFilters) => CDRFilters)(prev) : updater
      try {
        localStorage.setItem(FILTER_STORAGE_KEY, JSON.stringify(next))
      } catch {
        /* ignore */
      }
      return next
    })
  }, [])

  useEffect(() => {
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev)
      const FILTER_KEYS = ['operator_id', 'apn_id', 'record_type', 'rat_type'] as const
      for (const k of FILTER_KEYS) p.delete(k)
      for (const [k, v] of Object.entries(filters)) {
        if (k === 'from' || k === 'to') continue
        if (!(FILTER_KEYS as readonly string[]).includes(k)) continue
        if (v !== undefined && v !== '') p.set(k, String(v))
      }
      return p
    }, { replace: true })
  }, [filters, setSearchParams])

  const handleTimeframeChange = useCallback((v: TimeframeValue) => {
    if (v.value === 'custom' && v.from && v.to) {
      setCustomRange({ start: v.from, end: v.to })
    } else if (v.value !== 'custom') {
      setTimeframe(v.value)
    }
  }, [setTimeframe, setCustomRange])

  const [selectedSession, setSelectedSession] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)

  const { data: operators } = useOperatorList()
  const { data: apnsList } = useAPNList({})

  const list = useCDRList(filters, 50, Boolean(filters.from && filters.to))
  const stats = useCDRStats(filters, Boolean(filters.from && filters.to))
  const { exportCSV, exporting } = useExport('cdrs')

  const rows = useMemo(() => {
    const out = list.data?.pages?.flatMap((page) => page.data ?? []) ?? []
    return out
  }, [list.data])

  const simIDs = useMemo(() => Array.from(new Set(rows.map((r) => r.sim_id))), [rows])
  const { data: simMap } = useSimBatch(simIDs)

  const operatorMap = useMemo(() => {
    const m: Record<string, string> = {}
    for (const op of operators ?? []) m[op.id] = op.name
    return m
  }, [operators])

  const apnMap = useMemo(() => {
    const m: Record<string, string> = {}
    for (const apn of apnsList ?? []) m[apn.id] = apn.name
    return m
  }, [apnsList])

  const operatorOptions = useMemo(
    () => [{ value: '', label: 'Tüm operatörler' }, ...(operators ?? []).map((op) => ({ value: op.id, label: op.name }))],
    [operators],
  )

  const apnOptions = useMemo(
    () => [{ value: '', label: 'Tüm APNler' }, ...(apnsList ?? []).map((apn) => ({ value: apn.id, label: apn.name }))],
    [apnsList],
  )

  const ratTypeOptions = useMemo(
    () => [{ value: '', label: 'Tüm RAT' }, ...RAT_TYPES.map((t) => ({ value: t, label: t }))],
    [],
  )

  const clearFilters = useCallback(() => {
    setTimeframe('24h')
    const range = presetToRange('24h')
    setFilters({ from: range.from, to: range.to })
  }, [setFilters, setTimeframe])

  const handleExport = useCallback(async () => {
    if (!filters.from || !filters.to) {
      toast.error('Tarih aralığı gerekli')
      return
    }
    try {
      await api.post('/cdrs/export', { ...filters, format: 'csv' })
      toast.success('Rapor kuyruğa alındı — İşler sekmesinden takip edin')
    } catch {
      toast.error('Rapor başlatılamadı')
    }
  }, [filters])

  const handleExportInline = useCallback(() => {
    const filterRecord: Record<string, string | number | undefined> = {}
    for (const [k, v] of Object.entries(filters)) {
      if (v !== undefined && v !== '') filterRecord[k] = typeof v === 'number' ? v : String(v)
    }
    exportCSV(filterRecord)
  }, [filters, exportCSV])

  const openDrawer = useCallback((sessionID: string) => {
    setSelectedSession(sessionID)
    setDrawerOpen(true)
  }, [])

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-[15px] font-semibold text-text-primary flex items-center gap-2">
            <FileBarChart className="h-4 w-4 text-accent" />
            CDR Kayıtları
            <span className="ml-2 flex items-center gap-1">
              <span
                className="h-1.5 w-1.5 rounded-full bg-success animate-pulse"
                style={{ boxShadow: '0 0 6px rgba(0,255,136,0.4)' }}
              />
              <span className="text-[10px] text-text-tertiary">LIVE</span>
            </span>
          </h1>
          <p className="text-[12px] text-text-secondary mt-1">
            Oturum bazlı ücretlendirme kayıtları (CDR). Filtrele, incele, dışa aktar.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => list.refetch()}
            className="gap-1.5"
            disabled={list.isFetching}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${list.isFetching ? 'animate-spin' : ''}`} />
            Yenile
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={handleExportInline}
            className="gap-1.5"
            disabled={exporting}
          >
            <Download className="h-3.5 w-3.5" />
            CSV İndir
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={handleExport}
            className="gap-1.5"
          >
            <Download className="h-3.5 w-3.5" />
            Rapor İşi
          </Button>
        </div>
      </div>

      <Card className="p-4 bg-bg-surface border-border">
        <div className="flex items-center gap-2 mb-3 flex-wrap">
          <Filter className="h-3.5 w-3.5 text-text-secondary" />
          <p className="text-[11px] uppercase tracking-[1.5px] text-text-secondary font-medium">Filtreler</p>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <TimeframeSelector
              value={tfValue}
              onChange={handleTimeframeChange}
              options={CDR_TIMEFRAME_OPTIONS}
              allowCustom
              disabledPresets={!isAdmin ? ['30d'] : []}
              aria-label="CDR zaman aralığı"
            />
            <Button variant="ghost" size="sm" onClick={clearFilters} className="h-7 px-2 text-[11px] gap-1">
              <X className="h-3 w-3" />
              Temizle
            </Button>
          </div>
        </div>

        {/* Record-type chip row (F-U10) */}
        <div className="flex items-center gap-2 mb-3 flex-wrap">
          <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Kayıt Tipi</label>
          <div className="flex flex-wrap gap-1">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setFilters((p) => ({ ...p, record_type: undefined }))}
              className={cn(
                'h-7 px-3 text-[11px] font-medium',
                !filters.record_type && 'bg-accent-dim text-accent border-accent',
              )}
            >
              Tümü
            </Button>
            {RECORD_TYPES.map((rt) => (
              <Button
                key={rt}
                size="sm"
                variant="outline"
                onClick={() => setFilters((p) => ({ ...p, record_type: rt }))}
                className={cn(
                  'h-7 px-3 text-[11px] font-mono uppercase',
                  filters.record_type === rt && 'bg-accent-dim text-accent border-accent',
                )}
              >
                {rt}
              </Button>
            ))}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          <div>
            <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">SIM</label>
            <div className="mt-1">
              <SimSearch
                value={filters.sim_id ?? ''}
                onChange={(simID) => setFilters((p) => ({ ...p, sim_id: simID || undefined }))}
              />
            </div>
          </div>

          <div>
            <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Operatör</label>
            <Select
              className="mt-1"
              value={filters.operator_id ?? ''}
              options={operatorOptions}
              onChange={(e) => setFilters((p) => ({ ...p, operator_id: e.target.value || undefined }))}
            />
          </div>

          <div>
            <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">APN</label>
            <Select
              className="mt-1"
              value={filters.apn_id ?? ''}
              options={apnOptions}
              onChange={(e) => setFilters((p) => ({ ...p, apn_id: e.target.value || undefined }))}
            />
          </div>

          <div>
            <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">RAT</label>
            <Select
              className="mt-1"
              value={filters.rat_type ?? ''}
              options={ratTypeOptions}
              onChange={(e) => setFilters((p) => ({ ...p, rat_type: e.target.value || undefined }))}
            />
          </div>

          <div>
            <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Oturum ID</label>
            <div className="relative mt-1">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary pointer-events-none" />
              <Input
                className="pl-9 font-mono text-xs"
                placeholder="UUID"
                value={filters.session_id ?? ''}
                onChange={(e) => setFilters((p) => ({ ...p, session_id: e.target.value || undefined }))}
              />
            </div>
          </div>

          <div>
            <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Başlangıç (UTC)</label>
            <Input
              className="mt-1 font-mono text-xs"
              type="datetime-local"
              value={filters.from ? filters.from.slice(0, 16) : ''}
              onChange={(e) => setFilters((p) => ({
                ...p,
                from: e.target.value ? new Date(e.target.value).toISOString() : undefined,
              }))}
            />
          </div>

          <div>
            <label className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">Bitiş (UTC)</label>
            <Input
              className="mt-1 font-mono text-xs"
              type="datetime-local"
              value={filters.to ? filters.to.slice(0, 16) : ''}
              onChange={(e) => setFilters((p) => ({
                ...p,
                to: e.target.value ? new Date(e.target.value).toISOString() : undefined,
              }))}
            />
          </div>
        </div>
      </Card>

      {/* 4 stat cards per F-U5 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <StatCard
          label="Toplam Kayıt"
          value={stats.isLoading ? '—' : formatNumber(stats.data?.total_count ?? 0)}
          icon={<Activity className="h-4 w-4" />}
          tone="accent"
        />
        <StatCard
          label="Toplam Bayt"
          value={
            stats.isLoading
              ? '—'
              : `↓ ${formatBytes(stats.data?.total_bytes_in ?? 0)}  ↑ ${formatBytes(stats.data?.total_bytes_out ?? 0)}`
          }
          icon={<HardDrive className="h-4 w-4" />}
        />
        <StatCard
          label="Tekil SIM"
          value={stats.isLoading ? '—' : formatNumber(stats.data?.unique_sims ?? 0)}
          icon={<Users className="h-4 w-4" />}
        />
        <StatCard
          label="Oturum"
          value={stats.isLoading ? '—' : formatNumber(stats.data?.unique_sessions ?? 0)}
          icon={<Clock className="h-4 w-4" />}
        />
      </div>

      <Card className="overflow-hidden density-compact">
        <div className="max-h-[520px] overflow-y-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ZAMAN</TableHead>
                <TableHead>TİP</TableHead>
                <TableHead>OTURUM</TableHead>
                <TableHead>ICCID</TableHead>
                <TableHead>IMSI</TableHead>
                <TableHead>MSISDN</TableHead>
                <TableHead>OPERATÖR</TableHead>
                <TableHead>APN</TableHead>
                <TableHead>RAT</TableHead>
                <TableHead className="text-right">↓ BYTES</TableHead>
                <TableHead className="text-right">↑ BYTES</TableHead>
                <TableHead className="text-right">SÜRE</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isLoading && Array.from({ length: 8 }).map((_, i) => (
                <TableRow key={`sk-${i}`}>
                  <TableCell colSpan={12}>
                    <Skeleton className="h-6 w-full" />
                  </TableCell>
                </TableRow>
              ))}
              {!list.isLoading && rows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={12}>
                    <EmptyState
                      title="Bu filtre için CDR bulunamadı."
                      description="Filtre ölçütlerinizi genişletmeyi deneyin veya tarih aralığını değiştirin."
                      ctaLabel="Filtreleri Temizle"
                      onCta={clearFilters}
                    />
                  </TableCell>
                </TableRow>
              )}
              {rows.map((c) => {
                const simInfo = simMap?.[c.sim_id]
                return (
                  <TableRow
                    key={c.id}
                    className="cursor-pointer hover:bg-bg-hover"
                    onClick={() => openDrawer(c.session_id)}
                  >
                    <TableCell>
                      <span className="font-mono text-[12px] text-text-secondary" title={c.timestamp}>
                        {formatCDRTimestamp(c.timestamp)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant="secondary"
                        className={`font-mono text-[10px] uppercase ${recordTypeBadgeClass(c.record_type)}`}
                      >
                        {c.record_type}
                      </Badge>
                    </TableCell>
                    <TableCell onClick={(e) => e.stopPropagation()}>
                      <CopyableId value={c.session_id} mono />
                    </TableCell>
                    <TableCell onClick={(e) => e.stopPropagation()}>
                      <EntityLink
                        entityType="sim"
                        entityId={c.sim_id}
                        label={simInfo?.iccid || c.sim_id.slice(0, 8)}
                      />
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-[12px] text-text-secondary">
                        {simInfo?.imsi || '—'}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-[12px] text-text-secondary">
                        {simInfo?.msisdn || '—'}
                      </span>
                    </TableCell>
                    <TableCell onClick={(e) => e.stopPropagation()}>
                      <EntityLink
                        entityType="operator"
                        entityId={c.operator_id}
                        label={operatorMap[c.operator_id] ?? c.operator_id.slice(0, 8)}
                      />
                    </TableCell>
                    <TableCell onClick={(e) => e.stopPropagation()}>
                      {c.apn_id ? (
                        <EntityLink
                          entityType="apn"
                          entityId={c.apn_id}
                          label={apnMap[c.apn_id] ?? c.apn_id.slice(0, 8)}
                        />
                      ) : (
                        <span className="text-text-tertiary text-[12px]">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {c.rat_type ? <RATBadge ratType={c.rat_type} /> : <span className="text-text-tertiary text-xs">—</span>}
                    </TableCell>
                    <TableCell className="text-right">
                      <span className="font-mono text-[12px] text-success">{formatBytes(c.bytes_in)}</span>
                    </TableCell>
                    <TableCell className="text-right">
                      <span className="font-mono text-[12px] text-accent">{formatBytes(c.bytes_out)}</span>
                    </TableCell>
                    <TableCell className="text-right">
                      <span className="font-mono text-[12px] text-text-secondary">{c.duration_sec}s</span>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
        {list.hasNextPage && (
          <div className="flex items-center justify-center py-3 border-t border-border-subtle">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => list.fetchNextPage()}
              disabled={list.isFetchingNextPage}
            >
              {list.isFetchingNextPage ? 'Yükleniyor…' : 'Daha fazla yükle'}
            </Button>
          </div>
        )}
      </Card>

      <SessionTimelineDrawer
        sessionID={selectedSession}
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        onNavigateDetail={(id) => navigate(`/sessions/${id}`)}
      />
    </div>
  )
}

function StatCard({ label, value, icon, tone }: { label: string; value: string; icon: React.ReactNode; tone?: 'accent' | 'success' | 'warning' }) {
  const toneClass = tone === 'success' ? 'text-success' : tone === 'warning' ? 'text-warning' : tone === 'accent' ? 'text-accent' : 'text-text-primary'
  return (
    <div className="flex items-center gap-3 px-4 py-3 rounded-[var(--radius-md)] bg-bg-surface border border-border">
      <span className={`${toneClass} opacity-70`}>{icon}</span>
      <div className="min-w-0">
        <p className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">{label}</p>
        <p className="font-mono text-[13px] font-bold text-text-primary leading-none mt-0.5 truncate">{value}</p>
      </div>
    </div>
  )
}
