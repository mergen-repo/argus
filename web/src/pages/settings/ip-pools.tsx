import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Globe,
  AlertCircle,
  RefreshCw,
  ChevronRight,
  Plus,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { useIpPoolList, useCreateIpPool } from '@/hooks/use-settings'
import { useAPNList } from '@/hooks/use-apns'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'
import type { IpPool } from '@/types/settings'

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
        <div className={cn('h-full rounded-full transition-all duration-500', color)} style={{ width: `${Math.min(pct, 100)}%` }} />
      </div>
    </div>
  )
}

function PoolCard({ pool, onClick }: { pool: IpPool; onClick: () => void }) {
  const pct = pool.total_addresses > 0 ? (pool.used_addresses / pool.total_addresses) * 100 : 0
  const statusColor = pct > 90 ? 'var(--color-danger)' : pct > 70 ? 'var(--color-warning)' : 'var(--color-accent)'

  return (
    <Card className="card-hover cursor-pointer relative overflow-hidden" onClick={onClick}>
      <div className="absolute bottom-0 left-0 right-0 h-[2px]" style={{ backgroundColor: statusColor }} />
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm">{pool.name}</CardTitle>
          <ChevronRight className="h-4 w-4 text-text-tertiary" />
        </div>
      </CardHeader>
      <CardContent className="pt-0 space-y-3">
        <div className="font-mono text-xs text-text-secondary">{pool.cidr_v4 || pool.cidr_v6 || ''}</div>
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

function CreatePoolDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { data: apns = [] } = useAPNList({})
  const create = useCreateIpPool()
  const [form, setForm] = useState({ apn_id: '', name: '', cidr_v4: '' })

  const handleCreate = async () => {
    if (!form.apn_id || !form.name || !form.cidr_v4) return
    try {
      await create.mutateAsync({ apn_id: form.apn_id, name: form.name, cidr_v4: form.cidr_v4 })
      toast.success('IP pool created')
      setForm({ apn_id: '', name: '', cidr_v4: '' })
      onClose()
    } catch { /* toast handled by interceptor */ }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create IP Pool</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1">APN *</label>
            <Select
              value={form.apn_id}
              onChange={(e) => setForm((f) => ({ ...f, apn_id: e.target.value }))}
              options={[{ value: '', label: 'Select APN...' }, ...apns.map((a) => ({ value: a.id, label: a.name }))]}
            />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1">Pool Name *</label>
            <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="e.g. iot-pool-v4" />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary block mb-1">CIDR v4 *</label>
            <Input value={form.cidr_v4} onChange={(e) => setForm((f) => ({ ...f, cidr_v4: e.target.value }))} placeholder="e.g. 10.0.0.0/24" className="font-mono" />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
          <Button size="sm" onClick={handleCreate} disabled={!form.apn_id || !form.name || !form.cidr_v4 || create.isPending}>
            {create.isPending ? 'Creating…' : 'Create Pool'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default function IpPoolsPage() {
  const navigate = useNavigate()
  const { data: pools, isLoading, isError, refetch } = useIpPoolList()
  const [showCreate, setShowCreate] = useState(false)

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

  return (
    <div className="space-y-4">
      <CreatePoolDialog open={showCreate} onClose={() => setShowCreate(false)} />
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">IP Pools</h1>
        <Button size="sm" className="gap-1.5" onClick={() => setShowCreate(true)}>
          <Plus className="h-3.5 w-3.5" />
          Create Pool
        </Button>
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
            <p className="text-xs text-text-secondary mb-3">Create your first IP pool to assign static IPs to SIMs.</p>
            <Button size="sm" className="gap-1.5" onClick={() => setShowCreate(true)}>
              <Plus className="h-3.5 w-3.5" />
              Create Pool
            </Button>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {pools.map((pool) => (
            <PoolCard key={pool.id} pool={pool} onClick={() => navigate(`/settings/ip-pools/${pool.id}`)} />
          ))}
        </div>
      )}
    </div>
  )
}
