import { useState, useMemo, useCallback, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueries } from '@tanstack/react-query'
import {
  Search,
  X,
  Plus,
  Stethoscope,
  ExternalLink,
  ArrowLeftRight,
  Loader2,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { RAT_DISPLAY } from '@/lib/constants'
import { stateVariant, stateLabel } from '@/lib/sim-utils'
import { timeAgo } from '@/lib/format'
import type { SIM, ApiResponse } from '@/types/sim'

const MAX_SIMS = 3

function useSearchSIM(query: string) {
  return useQuery({
    queryKey: ['sims', 'search', query],
    queryFn: async () => {
      const res = await api.get<{ data: SIM[] }>(`/sims?q=${encodeURIComponent(query)}&limit=5`)
      return res.data.data || []
    },
    enabled: query.length >= 3,
    staleTime: 10_000,
  })
}

function useCompareSIMs(ids: string[]) {
  return useQueries({
    queries: ids.filter(Boolean).map((id) => ({
      queryKey: ['sims', 'detail', id],
      queryFn: async () => {
        const res = await api.get<ApiResponse<SIM>>(`/sims/${id}`)
        return res.data.data
      },
      enabled: !!id,
      staleTime: 10_000,
    })),
  })
}

interface SearchBoxProps {
  index: number
  value: string
  selectedSim: SIM | null
  onSelect: (sim: SIM) => void
  onRemove: () => void
  onQueryChange: (q: string) => void
  isLoading: boolean
}

function SearchBox({ index, value, selectedSim, onSelect, onRemove, onQueryChange, isLoading }: SearchBoxProps) {
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const { data: results, isFetching } = useSearchSIM(query)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  if (selectedSim) {
    return (
      <div className="flex items-center gap-2 p-3 rounded-[var(--radius-sm)] border border-border bg-bg-surface">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-xs text-text-tertiary">SIM {index + 1}</span>
            <Badge variant={stateVariant(selectedSim.state)} className="text-[10px]">
              {stateLabel(selectedSim.state)}
            </Badge>
          </div>
          <div className="font-mono text-xs text-text-primary mt-1 truncate">
            {selectedSim.iccid}
          </div>
        </div>
        {isLoading && <Loader2 className="h-3.5 w-3.5 animate-spin text-accent flex-shrink-0" />}
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 flex-shrink-0"
          onClick={onRemove}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
    )
  }

  return (
    <div ref={containerRef} className="relative">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
        <Input
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            onQueryChange(e.target.value)
            setOpen(true)
          }}
          onFocus={() => { if (query.length >= 3) setOpen(true) }}
          placeholder={`Search SIM ${index + 1} by ICCID, IMSI, MSISDN...`}
          className="pl-9 pr-8"
        />
        {isFetching && (
          <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 animate-spin text-accent" />
        )}
      </div>

      {open && results && results.length > 0 && (
        <div className="absolute z-50 top-full mt-1 left-0 right-0 rounded-[var(--radius-sm)] border border-border bg-bg-elevated shadow-lg max-h-[200px] overflow-y-auto">
          {results.map((sim) => (
            <Button
              key={sim.id}
              variant="ghost"
              className="w-full text-left px-3 py-2 h-auto justify-start hover:bg-bg-hover transition-colors border-b border-border-subtle last:border-0 rounded-none"
              onClick={() => {
                onSelect(sim)
                setQuery('')
                setOpen(false)
              }}
            >
              <div className="flex items-center gap-2">
                <span className="font-mono text-xs text-text-primary">{sim.iccid}</span>
                <Badge variant={stateVariant(sim.state)} className="text-[10px]">
                  {stateLabel(sim.state)}
                </Badge>
              </div>
              <div className="flex items-center gap-3 mt-0.5">
                <span className="text-[10px] text-text-tertiary">IMSI: {sim.imsi}</span>
                {sim.operator_name && (
                  <span className="text-[10px] text-text-tertiary">{sim.operator_name}</span>
                )}
              </div>
            </Button>
          ))}
        </div>
      )}

      {open && query.length >= 3 && !isFetching && results && results.length === 0 && (
        <div className="absolute z-50 top-full mt-1 left-0 right-0 rounded-[var(--radius-sm)] border border-border bg-bg-elevated shadow-lg">
          <div className="px-3 py-4 text-center">
            <p className="text-xs text-text-tertiary">No SIMs found</p>
          </div>
        </div>
      )}
    </div>
  )
}

interface ComparisonRow {
  label: string
  getValue: (sim: SIM) => string | React.ReactNode
  mono?: boolean
  highlightDiff?: boolean
}

function ComparisonTable({ sims }: { sims: (SIM | undefined)[] }) {
  const navigate = useNavigate()
  const validSims = sims.filter((s): s is SIM => s != null)

  const rows: ComparisonRow[] = useMemo(() => [
    {
      label: 'ICCID',
      getValue: (sim) => sim.iccid,
      mono: true,
    },
    {
      label: 'IMSI',
      getValue: (sim) => sim.imsi,
      mono: true,
    },
    {
      label: 'MSISDN',
      getValue: (sim) => sim.msisdn || '-- not assigned --',
      mono: true,
      highlightDiff: true,
    },
    {
      label: 'State',
      getValue: (sim) => (
        <Badge variant={stateVariant(sim.state)} className="text-[10px]">
          {stateLabel(sim.state)}
        </Badge>
      ),
      highlightDiff: true,
    },
    {
      label: 'Operator',
      getValue: (sim) => sim.operator_name || sim.operator_id || '--',
      highlightDiff: true,
    },
    {
      label: 'APN',
      getValue: (sim) => sim.apn_name || sim.apn_id || '-- not assigned --',
      highlightDiff: true,
    },
    {
      label: 'IP Address',
      getValue: (sim) => sim.ip_address || '-- no IP --',
      mono: true,
      highlightDiff: true,
    },
    {
      label: 'Policy',
      getValue: (sim) => sim.policy_name || sim.policy_version_id || '-- none --',
      highlightDiff: true,
    },
    {
      label: 'RAT Type',
      getValue: (sim) => sim.rat_type ? (RAT_DISPLAY[sim.rat_type] ?? sim.rat_type) : '-- not set --',
      highlightDiff: true,
    },
    {
      label: 'SIM Type',
      getValue: (sim) => sim.sim_type === 'esim' ? 'eSIM' : 'Physical',
      highlightDiff: true,
    },
    {
      label: 'Max Sessions',
      getValue: (sim) => String(sim.max_concurrent_sessions),
      mono: true,
      highlightDiff: true,
    },
    {
      label: 'Idle Timeout',
      getValue: (sim) => `${sim.session_idle_timeout_sec}s`,
      mono: true,
      highlightDiff: true,
    },
    {
      label: 'Hard Timeout',
      getValue: (sim) => `${sim.session_hard_timeout_sec}s`,
      mono: true,
      highlightDiff: true,
    },
    {
      label: 'Created',
      getValue: (sim) => timeAgo(sim.created_at),
      highlightDiff: true,
    },
    {
      label: 'Last Updated',
      getValue: (sim) => timeAgo(sim.updated_at),
      highlightDiff: true,
    },
    {
      label: 'Activated',
      getValue: (sim) => sim.activated_at ? timeAgo(sim.activated_at) : '--',
      highlightDiff: true,
    },
  ], [])

  const getStringValue = useCallback((sim: SIM, row: ComparisonRow): string => {
    const val = row.getValue(sim)
    if (typeof val === 'string') return val
    return ''
  }, [])

  const hasDiff = useCallback((row: ComparisonRow): boolean => {
    if (!row.highlightDiff || validSims.length < 2) return false
    const values = validSims.map((s) => getStringValue(s, row))
    return values.some((v) => v !== values[0])
  }, [validSims, getStringValue])

  if (validSims.length === 0) return null

  return (
    <Card className="overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow className="bg-bg-elevated border-b border-border hover:bg-bg-elevated">
            <TableHead className="text-[10px] uppercase tracking-[1px] text-text-tertiary font-medium w-36">
              Property
            </TableHead>
            {sims.map((sim, i) => (
              <TableHead key={i} className="min-w-[200px]">
                {sim ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="flex items-center gap-2 hover:text-accent transition-colors group h-auto p-0"
                    onClick={() => navigate(`/sims/${sim.id}`)}
                  >
                    <span className="text-sm font-medium text-text-primary group-hover:text-accent">
                      SIM {i + 1}
                    </span>
                    <ExternalLink className="h-3 w-3 text-text-tertiary group-hover:text-accent" />
                  </Button>
                ) : (
                  <span className="text-xs text-text-tertiary">SIM {i + 1}</span>
                )}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const diff = hasDiff(row)
            return (
              <TableRow
                key={row.label}
                className={cn(
                  'border-b border-border-subtle last:border-0 transition-colors',
                  diff && 'bg-accent/5',
                )}
              >
                <TableCell className="px-4 py-2.5">
                  <span className="text-xs text-text-secondary">{row.label}</span>
                  {diff && (
                    <span className="ml-1.5 inline-block h-1.5 w-1.5 rounded-full bg-accent" />
                  )}
                </TableCell>
                {sims.map((sim, i) => (
                  <TableCell key={i} className="px-4 py-2.5">
                    {sim ? (
                      <span className={cn('text-sm text-text-primary', row.mono && 'font-mono text-xs')}>
                        {row.getValue(sim)}
                      </span>
                    ) : (
                      <span className="text-xs text-text-tertiary">--</span>
                    )}
                  </TableCell>
                ))}
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </Card>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-24">
      <div className="rounded-xl border border-border bg-bg-surface p-8 shadow-[var(--shadow-card)] text-center max-w-md">
        <div className="h-12 w-12 rounded-lg bg-accent/10 border border-accent/20 flex items-center justify-center mx-auto mb-4">
          <ArrowLeftRight className="h-6 w-6 text-accent" />
        </div>
        <h3 className="text-sm font-semibold text-text-primary mb-1">No SIMs selected</h3>
        <p className="text-xs text-text-secondary">
          Search and add up to 3 SIM cards above to compare their properties side by side.
        </p>
      </div>
    </div>
  )
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-6 w-48" />
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
      <Card>
        <CardContent className="p-4 space-y-3">
          {Array.from({ length: 10 }).map((_, i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

export default function SIMComparePage() {
  const navigate = useNavigate()
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [queries, setQueries] = useState<string[]>(['', '', ''])

  const results = useCompareSIMs(selectedIds)

  const sims = useMemo(() => {
    const simMap = new Map<string, SIM>()
    results.forEach((r) => {
      if (r.data) simMap.set(r.data.id, r.data)
    })
    return selectedIds.map((id) => simMap.get(id))
  }, [results, selectedIds])

  const anyLoading = results.some((r) => r.isLoading)

  const handleSelect = useCallback((index: number, sim: SIM) => {
    if (selectedIds.includes(sim.id)) return
    setSelectedIds((prev) => {
      const next = [...prev]
      if (index < next.length) {
        next[index] = sim.id
      } else {
        next.push(sim.id)
      }
      return next
    })
  }, [selectedIds])

  const handleRemove = useCallback((index: number) => {
    setSelectedIds((prev) => prev.filter((_, i) => i !== index))
  }, [])

  const handleAddSlot = useCallback(() => {
    // Just adds visual slot, no id yet
  }, [])

  const slotCount = Math.max(selectedIds.length + 1, 2)
  const visibleSlots = Math.min(slotCount, MAX_SIMS)

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Breadcrumb
          items={[
            { label: 'SIM Cards', href: '/sims' },
            { label: 'Compare' },
          ]}
        />
        <div className="flex items-center justify-between">
          <h1 className="text-[16px] font-semibold text-text-primary">SIM Comparison</h1>
          {selectedIds.length < MAX_SIMS && selectedIds.length > 0 && (
            <Button
              variant="outline"
              size="sm"
              className="gap-2"
              onClick={handleAddSlot}
            >
              <Plus className="h-3.5 w-3.5" />
              Add SIM ({selectedIds.length}/{MAX_SIMS})
            </Button>
          )}
        </div>
      </div>

      <div className={cn('grid gap-3', visibleSlots === 2 ? 'grid-cols-1 md:grid-cols-2' : 'grid-cols-1 md:grid-cols-3')}>
        {Array.from({ length: visibleSlots }).map((_, i) => {
          const simId = selectedIds[i]
          const sim = simId ? sims.find((s) => s?.id === simId) ?? null : null
          const isLoadingThis = simId ? results.find((r) => r.data?.id === simId)?.isLoading ?? false : false

          return (
            <SearchBox
              key={i}
              index={i}
              value={queries[i] || ''}
              selectedSim={sim}
              onSelect={(s) => handleSelect(i, s)}
              onRemove={() => handleRemove(i)}
              onQueryChange={(q) => {
                setQueries((prev) => {
                  const next = [...prev]
                  next[i] = q
                  return next
                })
              }}
              isLoading={isLoadingThis}
            />
          )
        })}
      </div>

      {selectedIds.length === 0 ? (
        <EmptyState />
      ) : anyLoading ? (
        <Card>
          <CardContent className="p-4 space-y-3">
            {Array.from({ length: 10 }).map((_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </CardContent>
        </Card>
      ) : (
        <>
          <ComparisonTable sims={sims} />

          <div className="flex items-center gap-3">
            <div className="flex items-center gap-1.5 text-xs text-text-tertiary">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-accent" />
              Highlighted rows indicate differences between SIMs
            </div>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Actions</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="flex flex-wrap gap-2">
                {sims.map((sim, i) => sim && (
                  <div key={sim.id} className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1.5"
                      onClick={() => navigate(`/sims/${sim.id}`)}
                    >
                      <ExternalLink className="h-3 w-3" />
                      View SIM {i + 1}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1.5"
                      onClick={() => {
                        navigate(`/sims/${sim.id}`)
                      }}
                    >
                      <Stethoscope className="h-3 w-3" />
                      Diagnose SIM {i + 1}
                    </Button>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
