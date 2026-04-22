import { useState, useMemo, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import {
  Globe,
  AlertCircle,
  RefreshCw,
  Loader2,
  ArrowLeft,
  Bookmark,
  Search,
  X,
  Trash2,
} from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { SimSearch } from '@/components/ui/sim-search'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { Spinner } from '@/components/ui/spinner'
import { Skeleton } from '@/components/ui/skeleton'
import { useIpPoolList, useIpPoolAddresses, useReserveIp, useReleaseIp } from '@/hooks/use-settings'
import { useAPNList } from '@/hooks/use-apns'
import { useOperatorList } from '@/hooks/use-operators'
import { cn } from '@/lib/utils'
import type { SIM } from '@/types/sim'

function addressStateVariant(state: string): 'success' | 'warning' | 'secondary' {
  switch (state) {
    case 'available': return 'success'
    case 'assigned': return 'warning'
    case 'reserved': return 'secondary'
    default: return 'secondary'
  }
}

const stateOrder: Record<string, number> = { assigned: 0, reserved: 1, available: 2 }

interface SelectedSim {
  id: string
  iccid: string
  imsi: string
  msisdn?: string
}

export default function IpPoolDetailPage() {
  const { poolId } = useParams<{ poolId: string }>()
  const navigate = useNavigate()
  const { data: pools, isLoading: poolsLoading } = useIpPoolList()
  const { data: apns } = useAPNList({})
  const { data: operators } = useOperatorList()
  const pool = useMemo(() => pools?.find((p) => p.id === poolId), [pools, poolId])
  const apn = useMemo(() => apns?.find((a) => a.id === pool?.apn_id), [apns, pool])
  const operator = useMemo(() => operators?.find((o) => o.id === apn?.operator_id), [operators, apn])

  const [showReservePanel, setShowReservePanel] = useState(false)
  const [reserveQueue, setReserveQueue] = useState<SelectedSim[]>([])
  const [searchFilter, setSearchFilter] = useState('')
  const [reserving, setReserving] = useState(false)

  const {
    data: addressPages,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useIpPoolAddresses(poolId ?? '')

  const reserveMutation = useReserveIp()
  const releaseMutation = useReleaseIp()

  const loadMoreRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = loadMoreRef.current
    if (!el) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage()
        }
      },
      { threshold: 0.1 },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const allAddresses = useMemo(() => {
    if (!addressPages?.pages) return []
    return addressPages.pages.flatMap((page) => page.data)
  }, [addressPages])

  const sortedAddresses = useMemo(() => {
    return [...allAddresses].sort((a, b) => {
      const oa = stateOrder[a.state] ?? 3
      const ob = stateOrder[b.state] ?? 3
      return oa - ob
    })
  }, [allAddresses])

  const filteredAddresses = useMemo(() => {
    if (!searchFilter) return sortedAddresses
    const q = searchFilter.toLowerCase()
    return sortedAddresses.filter((addr) =>
      (addr.address_v4?.toLowerCase().includes(q)) ||
      (addr.address_v6?.toLowerCase().includes(q)) ||
      (addr.sim_iccid?.toLowerCase().includes(q)) ||
      (addr.sim_imsi?.toLowerCase().includes(q)) ||
      (addr.sim_msisdn?.toLowerCase().includes(q)) ||
      (addr.sim_id?.toLowerCase().includes(q)) ||
      addr.state.toLowerCase().includes(q)
    )
  }, [sortedAddresses, searchFilter])

  const reservedAddresses = useMemo(() => {
    return sortedAddresses.filter((a) => a.state === 'reserved' || a.state === 'assigned')
  }, [sortedAddresses])

  const handleAddToQueue = useCallback((_simId: string, sim?: SIM) => {
    if (!sim) return
    if (reserveQueue.some((s) => s.id === sim.id)) return
    setReserveQueue((prev) => [...prev, {
      id: sim.id,
      iccid: sim.iccid,
      imsi: sim.imsi,
      msisdn: sim.msisdn ?? undefined,
    }])
  }, [reserveQueue])

  const handleRemoveFromQueue = useCallback((simId: string) => {
    setReserveQueue((prev) => prev.filter((s) => s.id !== simId))
  }, [])

  const handleReserveAll = async () => {
    if (!poolId || reserveQueue.length === 0) return
    setReserving(true)
    try {
      for (const sim of reserveQueue) {
        await reserveMutation.mutateAsync({ poolId, simId: sim.id })
      }
      setReserveQueue([])
      setShowReservePanel(false)
    } catch {
      // partial success
    } finally {
      setReserving(false)
    }
  }

  const handleRelease = async (addressId: string) => {
    if (!poolId) return
    await releaseMutation.mutateAsync({ poolId, addressId })
  }

  if (poolsLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-4 w-48" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-3 w-full max-w-sm" />
        <Skeleton className="h-[400px] w-full" />
      </div>
    )
  }

  if (!pool) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Pool not found</h2>
          <Button onClick={() => navigate('/settings/ip-pools')} variant="outline" className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back to IP Pools
          </Button>
        </div>
      </div>
    )
  }

  const pct = pool.total_addresses > 0 ? (pool.used_addresses / pool.total_addresses) * 100 : 0
  const barColor = pct > 90 ? 'bg-danger' : pct > 70 ? 'bg-warning' : 'bg-accent'

  return (
    <div className="space-y-4">
      <Breadcrumb
        items={[
          { label: 'Settings', href: '/settings/ip-pools' },
          { label: 'IP Pools', href: '/settings/ip-pools' },
          { label: pool.name },
        ]}
        className="mb-1"
      />

      <div className="flex items-center gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <h1 className="text-[16px] font-semibold text-text-primary">{pool.name}</h1>
            <span className="font-mono text-xs text-text-tertiary">{pool.cidr_v4 || pool.cidr_v6 || ''}</span>
          </div>
          <div className="flex items-center gap-2 mt-1">
            {operator && (
              <Link to={`/operators/${operator.id}`} className="flex items-center gap-1.5 text-xs text-text-secondary hover:text-accent transition-colors">
                <span className="h-1.5 w-1.5 rounded-full bg-purple shrink-0" />
                {operator.name}
              </Link>
            )}
            {apn && (
              <>
                {operator && <span className="text-text-tertiary text-xs">/</span>}
                <Link to={`/apns/${apn.id}`} className="flex items-center gap-1.5 text-xs text-text-secondary hover:text-accent transition-colors">
                  <span className="h-1.5 w-1.5 rounded-full bg-cyan shrink-0" />
                  {apn.name}
                </Link>
              </>
            )}
            {!apn && !operator && (
              <span className="text-xs text-text-tertiary">Standalone pool</span>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary pointer-events-none" />
            <Input
              type="text"
              value={searchFilter}
              onChange={(e) => setSearchFilter(e.target.value)}
              placeholder="Filter by IP, SIM..."
              className="h-8 w-56 bg-bg-surface pl-8 pr-8 text-xs"
            />
            {searchFilter && (
              <Button variant="ghost" size="icon" onClick={() => setSearchFilter('')} className="absolute right-2 top-1/2 -translate-y-1/2 h-5 w-5 text-text-tertiary hover:text-text-primary">
                <X className="h-3 w-3" />
              </Button>
            )}
          </div>
          <Button size="sm" className="gap-2" onClick={() => { setShowReservePanel(true); setReserveQueue([]) }}>
            <Bookmark className="h-3.5 w-3.5" />
            Reserve IP
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-4 gap-3">
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Total</div>
            <div className="font-mono text-lg font-bold text-accent">{pool.total_addresses.toLocaleString()}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Used</div>
            <div className="font-mono text-lg font-bold text-warning">{pool.used_addresses.toLocaleString()}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Free</div>
            <div className="font-mono text-lg font-bold text-success">{pool.available_addresses.toLocaleString()}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-4 text-center">
            <div className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Utilization</div>
            <div className="font-mono text-lg font-bold text-text-primary">{pct.toFixed(1)}%</div>
            <div className="w-full h-1.5 bg-bg-hover rounded-full overflow-hidden mt-2">
              <div className={cn('h-full rounded-full', barColor)} style={{ width: `${Math.min(pct, 100)}%` }} />
            </div>
          </CardContent>
        </Card>
      </div>

      <Card className="overflow-hidden density-compact">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>IP Address</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Assigned SIM</TableHead>
                <TableHead>Assigned At</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredAddresses.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5}>
                    <div className="flex flex-col items-center justify-center py-12 text-center">
                      <Globe className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                      <p className="text-xs text-text-secondary">
                        {searchFilter ? `No addresses matching "${searchFilter}"` : 'No addresses loaded yet'}
                      </p>
                    </div>
                  </TableCell>
                </TableRow>
              )}
              {filteredAddresses.map((addr) => (
                <TableRow key={addr.id}>
                  <TableCell>
                    <span className="font-mono text-xs text-text-primary">{addr.address_v4 || addr.address_v6 || '-'}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={addressStateVariant(addr.state)} className="text-[10px]">
                      {addr.state.toUpperCase()}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {addr.sim_id ? (
                      <Link
                        to={`/sims/${addr.sim_id}`}
                        className="hover:bg-bg-hover rounded px-1 -mx-1 transition-colors inline-block"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <span className="font-mono text-xs text-accent hover:underline block">
                          {addr.sim_iccid || addr.sim_id.slice(0, 12)}
                        </span>
                        <div className="flex items-center gap-2 mt-0.5">
                          {addr.sim_imsi && <span className="font-mono text-[10px] text-text-tertiary">{addr.sim_imsi}</span>}
                          {addr.sim_msisdn && <span className="font-mono text-[10px] text-text-tertiary">{addr.sim_msisdn}</span>}
                        </div>
                      </Link>
                    ) : (
                      <span className="text-xs text-text-tertiary">-</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {addr.allocated_at ? new Date(addr.allocated_at).toLocaleString() : '-'}
                    </span>
                  </TableCell>
                  <TableCell>
                    {(addr.state === 'reserved' || addr.state === 'assigned') && (
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => handleRelease(addr.id)}
                        disabled={releaseMutation.isPending}
                        className="h-7 w-7 text-text-tertiary hover:text-danger hover:bg-danger-dim"
                        title="Release reservation"
                      >
                        {releaseMutation.isPending ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <div ref={loadMoreRef} className="px-4 py-3 border-t border-border-subtle">
          {isFetchingNextPage ? (
            <div className="flex items-center justify-center gap-2 text-text-tertiary text-xs">
              <Spinner className="h-3.5 w-3.5" />
              Loading more...
            </div>
          ) : hasNextPage ? (
            <Button
              variant="ghost"
              onClick={() => fetchNextPage()}
              className="w-full text-center text-xs text-text-tertiary hover:text-accent py-1"
            >
              Load more addresses
            </Button>
          ) : allAddresses.length > 0 ? (
            <p className="text-center text-xs text-text-tertiary">
              {searchFilter ? `${filteredAddresses.length} of ${allAddresses.length} addresses` : `${allAddresses.length} addresses`}
            </p>
          ) : null}
        </div>
      </Card>

      {/* Multi-Reserve SlidePanel */}
      <SlidePanel
        open={showReservePanel}
        onOpenChange={setShowReservePanel}
        title="Reserve IP Addresses"
        description={`Assign static IPs from ${pool.name} to SIMs.`}
        width="lg"
      >
        <div className="space-y-4">
          <div>
            <label className="text-xs text-text-secondary block mb-1.5">Search & add SIMs</label>
            <SimSearch value="" onChange={handleAddToQueue} />
          </div>

          {reserveQueue.length > 0 && (
            <div>
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs text-text-secondary">{reserveQueue.length} SIM{reserveQueue.length !== 1 ? 's' : ''} to reserve</span>
                <Button variant="ghost" size="sm" onClick={() => setReserveQueue([])} className="text-[11px] text-text-tertiary hover:text-accent h-auto py-0 px-1">Clear all</Button>
              </div>
              <div className="space-y-1.5 max-h-48 overflow-y-auto">
                {reserveQueue.map((sim) => (
                  <div key={sim.id} className="flex items-center justify-between rounded-[var(--radius-sm)] border border-border bg-bg-primary px-3 py-2">
                    <div className="min-w-0">
                      <span className="font-mono text-xs text-accent">{sim.iccid}</span>
                      <div className="flex items-center gap-2 mt-0.5">
                        <span className="font-mono text-[10px] text-text-tertiary">{sim.imsi}</span>
                        {sim.msisdn && <span className="font-mono text-[10px] text-text-tertiary">{sim.msisdn}</span>}
                      </div>
                    </div>
                    <Button variant="ghost" size="icon" onClick={() => handleRemoveFromQueue(sim.id)} className="h-6 w-6 text-text-tertiary hover:text-danger shrink-0">
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {reservedAddresses.length > 0 && (
            <div>
              <div className="text-xs text-text-secondary mb-2">Currently reserved ({reservedAddresses.length})</div>
              <div className="space-y-1 max-h-40 overflow-y-auto">
                {reservedAddresses.map((addr) => (
                  <div key={addr.id} className="flex items-center justify-between rounded-[var(--radius-sm)] border border-border-subtle bg-bg-primary px-3 py-1.5 text-[11px]">
                    <span className="font-mono text-text-secondary">{addr.address_v4 || addr.address_v6}</span>
                    <span className="text-text-tertiary">→</span>
                    {addr.sim_iccid ? (
                      <span className="font-mono text-accent">{addr.sim_iccid}</span>
                    ) : (
                      <span className="font-mono text-text-tertiary">{addr.sim_id?.slice(0, 12)}</span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        <SlidePanelFooter>
          <Button variant="outline" onClick={() => setShowReservePanel(false)}>Cancel</Button>
          <Button onClick={handleReserveAll} disabled={reserveQueue.length === 0 || reserving} className="gap-2">
            {reserving && <Loader2 className="h-4 w-4 animate-spin" />}
            Reserve {reserveQueue.length > 0 ? `${reserveQueue.length} IP${reserveQueue.length !== 1 ? 's' : ''}` : 'IPs'}
          </Button>
        </SlidePanelFooter>
      </SlidePanel>
    </div>
  )
}
