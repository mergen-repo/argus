import { useState, useMemo, useEffect, useRef } from 'react'
import {
  Globe,
  AlertCircle,
  RefreshCw,
  Loader2,
  ChevronRight,
  ArrowLeft,
  Bookmark,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { useIpPoolList, useIpPoolAddresses, useReserveIp } from '@/hooks/use-settings'
import { cn } from '@/lib/utils'
import type { IpPool } from '@/types/settings'

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse rounded-[var(--radius-sm)] bg-bg-hover ${className ?? ''}`} />
}

function UtilizationBar({ used, total }: { used: number; total: number }) {
  const pct = total > 0 ? (used / total) * 100 : 0
  const color = pct > 90 ? 'bg-danger' : pct > 70 ? 'bg-warning' : 'bg-accent'
  return (
    <div className="w-full">
      <div className="flex items-center justify-between mb-1">
        <span className="font-mono text-[11px] text-text-secondary">
          {used.toLocaleString()} / {total.toLocaleString()}
        </span>
        <span className="font-mono text-[11px] text-text-tertiary">{pct.toFixed(1)}%</span>
      </div>
      <div className="w-full h-2 bg-bg-hover rounded-full overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-500', color)}
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
    </div>
  )
}

function addressStateVariant(state: string): 'success' | 'warning' | 'secondary' {
  switch (state) {
    case 'available': return 'success'
    case 'assigned': return 'warning'
    case 'reserved': return 'secondary'
    default: return 'secondary'
  }
}

function PoolCard({ pool, onClick }: { pool: IpPool; onClick: () => void }) {
  const pct = pool.total_addresses > 0 ? (pool.used_addresses / pool.total_addresses) * 100 : 0
  const statusColor = pct > 90 ? 'var(--color-danger)' : pct > 70 ? 'var(--color-warning)' : 'var(--color-accent)'

  return (
    <Card
      className="card-hover cursor-pointer relative overflow-hidden"
      onClick={onClick}
    >
      <div className="absolute bottom-0 left-0 right-0 h-[2px]" style={{ backgroundColor: statusColor }} />
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm">{pool.name}</CardTitle>
          <ChevronRight className="h-4 w-4 text-text-tertiary" />
        </div>
      </CardHeader>
      <CardContent className="pt-0 space-y-3">
        <div className="font-mono text-xs text-text-secondary">{pool.cidr}</div>
        <div className="grid grid-cols-3 gap-2 text-center">
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Total</span>
            <span className="font-mono text-sm text-text-primary">{pool.total_addresses.toLocaleString()}</span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Used</span>
            <span className="font-mono text-sm text-text-primary">{pool.used_addresses.toLocaleString()}</span>
          </div>
          <div>
            <span className="text-[10px] uppercase tracking-[1px] text-text-tertiary block">Free</span>
            <span className="font-mono text-sm text-success">{pool.available_addresses.toLocaleString()}</span>
          </div>
        </div>
        <UtilizationBar used={pool.used_addresses} total={pool.total_addresses} />
      </CardContent>
    </Card>
  )
}

export default function IpPoolsPage() {
  const { data: pools, isLoading, isError, refetch } = useIpPoolList()
  const [selectedPool, setSelectedPool] = useState<IpPool | null>(null)
  const [showReserveDialog, setShowReserveDialog] = useState(false)
  const [reserveSimId, setReserveSimId] = useState('')

  const {
    data: addressPages,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useIpPoolAddresses(selectedPool?.id ?? '')

  const reserveMutation = useReserveIp()

  const observerRef = useRef<IntersectionObserver | null>(null)
  const loadMoreRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = loadMoreRef.current
    if (!el) return
    observerRef.current = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage()
        }
      },
      { threshold: 0.1 },
    )
    observerRef.current.observe(el)
    return () => { observerRef.current?.disconnect() }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const allAddresses = useMemo(() => {
    if (!addressPages?.pages) return []
    return addressPages.pages.flatMap((page) => page.data)
  }, [addressPages])

  const handleReserve = async () => {
    if (!selectedPool || !reserveSimId) return
    try {
      await reserveMutation.mutateAsync({ poolId: selectedPool.id, simId: reserveSimId })
      setShowReserveDialog(false)
      setReserveSimId('')
    } catch {
      // handled by api interceptor
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load IP pools</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch IP pool data.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  if (selectedPool) {
    return (
      <div className="p-6 space-y-4">
        <div className="flex items-center gap-3 mb-2">
          <Button
            variant="ghost"
            size="sm"
            className="gap-1.5"
            onClick={() => setSelectedPool(null)}
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Back
          </Button>
          <div>
            <h1 className="text-[16px] font-semibold text-text-primary">{selectedPool.name}</h1>
            <p className="text-xs text-text-secondary font-mono">{selectedPool.cidr}</p>
          </div>
          <div className="ml-auto">
            <Button size="sm" className="gap-2" onClick={() => setShowReserveDialog(true)}>
              <Bookmark className="h-3.5 w-3.5" />
              Reserve IP
            </Button>
          </div>
        </div>

        <div className="max-w-sm">
          <UtilizationBar used={selectedPool.used_addresses} total={selectedPool.total_addresses} />
        </div>

        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <Table>
              <TableHeader className="bg-bg-elevated">
                <TableRow>
                  <TableHead>IP Address</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Assigned SIM</TableHead>
                  <TableHead>Assigned At</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {allAddresses.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={4}>
                      <div className="flex flex-col items-center justify-center py-16 text-center">
                        <Globe className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <p className="text-xs text-text-secondary">No addresses loaded yet</p>
                      </div>
                    </TableCell>
                  </TableRow>
                )}
                {allAddresses.map((addr) => (
                  <TableRow key={addr.id}>
                    <TableCell>
                      <span className="font-mono text-sm text-text-primary">{addr.address}</span>
                    </TableCell>
                    <TableCell>
                      <Badge variant={addressStateVariant(addr.state)} className="text-[10px]">
                        {addr.state.toUpperCase()}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs text-text-secondary">
                        {addr.sim_iccid ?? '-'}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-xs text-text-secondary">
                        {addr.assigned_at ? new Date(addr.assigned_at).toLocaleString() : '-'}
                      </span>
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
              <button
                onClick={() => fetchNextPage()}
                className="w-full text-center text-xs text-text-tertiary hover:text-accent transition-colors py-1"
              >
                Load more addresses
              </button>
            ) : allAddresses.length > 0 ? (
              <p className="text-center text-xs text-text-tertiary">
                Showing {allAddresses.length} addresses
              </p>
            ) : null}
          </div>
        </Card>

        {/* Reserve IP Dialog */}
        <Dialog open={showReserveDialog} onOpenChange={setShowReserveDialog}>
          <DialogContent onClose={() => setShowReserveDialog(false)}>
            <DialogHeader>
              <DialogTitle>Reserve IP Address</DialogTitle>
              <DialogDescription>
                Assign a static IP from this pool to a specific SIM.
              </DialogDescription>
            </DialogHeader>
            <div>
              <label className="text-xs text-text-secondary block mb-1.5">SIM ID</label>
              <Input
                value={reserveSimId}
                onChange={(e) => setReserveSimId(e.target.value)}
                placeholder="Enter SIM UUID or ICCID"
              />
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setShowReserveDialog(false)}>
                Cancel
              </Button>
              <Button
                onClick={handleReserve}
                disabled={!reserveSimId || reserveMutation.isPending}
                className="gap-2"
              >
                {reserveMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                Reserve
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">IP Pools</h1>
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i}>
              <CardHeader className="pb-2"><Skeleton className="h-4 w-32" /></CardHeader>
              <CardContent className="pt-0 space-y-3">
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-12 w-full" />
                <Skeleton className="h-3 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : (!pools || pools.length === 0) ? (
        <div className="flex flex-col items-center justify-center py-24">
          <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)] text-center">
            <Globe className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
            <h3 className="text-sm font-semibold text-text-primary mb-1">No IP pools</h3>
            <p className="text-xs text-text-secondary">IP pools will appear here when configured.</p>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {pools.map((pool) => (
            <PoolCard key={pool.id} pool={pool} onClick={() => setSelectedPool(pool)} />
          ))}
        </div>
      )}
    </div>
  )
}
