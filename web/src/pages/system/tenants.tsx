import { useState } from 'react'
import {
  Plus,
  AlertCircle,
  RefreshCw,
  Loader2,
  Building,
  Shield,
  Settings,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
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
import { Sheet, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { useTenantList, useCreateTenant, useUpdateTenant } from '@/hooks/use-settings'
import { useAuthStore } from '@/stores/auth'
import type { Tenant } from '@/types/settings'

const PLAN_OPTIONS = [
  { value: 'starter', label: 'Starter' },
  { value: 'professional', label: 'Professional' },
  { value: 'enterprise', label: 'Enterprise' },
]

function planVariant(plan: string): 'default' | 'success' | 'warning' {
  switch (plan) {
    case 'enterprise': return 'default'
    case 'professional': return 'success'
    case 'starter': return 'warning'
    default: return 'default'
  }
}

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse rounded-[var(--radius-sm)] bg-bg-hover ${className ?? ''}`} />
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

export default function TenantManagementPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'

  const { data: tenants, isLoading, isError, refetch } = useTenantList()
  const createMutation = useCreateTenant()
  const updateMutation = useUpdateTenant()

  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [createForm, setCreateForm] = useState({
    name: '',
    slug: '',
    plan: 'starter',
    max_sims: 10000,
    max_users: 50,
  })
  const [selectedTenant, setSelectedTenant] = useState<Tenant | null>(null)
  const [editForm, setEditForm] = useState<{
    retention_days: number
    max_sims: number
    max_users: number
    max_api_keys: number
    plan: string
  } | null>(null)

  if (!isSuperAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <Shield className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Access Denied</h2>
          <p className="text-sm text-text-secondary">You need super_admin role to manage tenants.</p>
        </div>
      </div>
    )
  }

  const handleCreate = async () => {
    try {
      await createMutation.mutateAsync(createForm)
      setShowCreateDialog(false)
      setCreateForm({ name: '', slug: '', plan: 'starter', max_sims: 10000, max_users: 50 })
    } catch {
      // handled by api interceptor
    }
  }

  const handleSelectTenant = (tenant: Tenant) => {
    setSelectedTenant(tenant)
    setEditForm({
      retention_days: tenant.retention_days,
      max_sims: tenant.max_sims,
      max_users: tenant.max_users,
      max_api_keys: tenant.max_api_keys,
      plan: tenant.plan ?? 'standard',
    })
  }

  const handleSaveConfig = async () => {
    if (!selectedTenant || !editForm) return
    try {
      await updateMutation.mutateAsync({
        id: selectedTenant.id,
        plan: editForm.plan,
        retention_days: editForm.retention_days,
        max_sims: editForm.max_sims,
        max_users: editForm.max_users,
        max_api_keys: editForm.max_api_keys,
      })
      setSelectedTenant(null)
      setEditForm(null)
    } catch {
      // handled by api interceptor
    }
  }

  const handleSlugFromName = (name: string) => {
    const slug = name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '')
    setCreateForm((f) => ({ ...f, name, slug }))
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load tenants</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch tenant data.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Tenant Management</h1>
        <Button size="sm" className="gap-2" onClick={() => setShowCreateDialog(true)}>
          <Plus className="h-3.5 w-3.5" />
          Create Tenant
        </Button>
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Slug</TableHead>
                <TableHead>Plan</TableHead>
                <TableHead>SIM Count</TableHead>
                <TableHead>Users</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-12" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 4 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 7 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && (!tenants || tenants.length === 0) && (
                <TableRow>
                  <TableCell colSpan={7}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Building className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No tenants</h3>
                        <p className="text-xs text-text-secondary">Create your first tenant to get started.</p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {(tenants ?? []).map((tenant) => (
                <TableRow
                  key={tenant.id}
                  className="cursor-pointer hover:bg-bg-hover"
                  onClick={() => handleSelectTenant(tenant)}
                >
                  <TableCell>
                    <span className="text-sm font-medium text-text-primary">{tenant.name}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{tenant.slug}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={planVariant(tenant.plan ?? 'standard')} className="text-[10px]">
                      {tenant.plan?.toUpperCase() ?? 'STANDARD'}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-primary">{formatNumber(tenant.sim_count)}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{tenant.user_count}</span>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(tenant.created_at).toLocaleDateString()}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Settings className="h-3.5 w-3.5 text-text-tertiary" />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {!isLoading && tenants && tenants.length > 0 && (
          <div className="px-4 py-3 border-t border-border-subtle">
            <p className="text-center text-xs text-text-tertiary">
              {tenants.length} tenant{tenants.length !== 1 ? 's' : ''}
            </p>
          </div>
        )}
      </Card>

      {/* Create Tenant Dialog */}
      <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
        <DialogContent onClose={() => setShowCreateDialog(false)}>
          <DialogHeader>
            <DialogTitle>Create Tenant</DialogTitle>
            <DialogDescription>
              Add a new tenant organization to the platform.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-xs text-text-secondary block mb-1.5">Name</label>
              <Input
                value={createForm.name}
                onChange={(e) => handleSlugFromName(e.target.value)}
                placeholder="Acme Corp"
              />
            </div>
            <div>
              <label className="text-xs text-text-secondary block mb-1.5">Slug</label>
              <Input
                value={createForm.slug}
                onChange={(e) => setCreateForm((f) => ({ ...f, slug: e.target.value }))}
                placeholder="acme-corp"
                className="font-mono"
              />
            </div>
            <div>
              <label className="text-xs text-text-secondary block mb-1.5">Plan</label>
              <Select
                options={PLAN_OPTIONS}
                value={createForm.plan}
                onChange={(e) => setCreateForm((f) => ({ ...f, plan: e.target.value }))}
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs text-text-secondary block mb-1.5">Max SIMs</label>
                <Input
                  type="number"
                  value={createForm.max_sims}
                  onChange={(e) => setCreateForm((f) => ({ ...f, max_sims: parseInt(e.target.value) || 0 }))}
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1.5">Max Users</label>
                <Input
                  type="number"
                  value={createForm.max_users}
                  onChange={(e) => setCreateForm((f) => ({ ...f, max_users: parseInt(e.target.value) || 0 }))}
                />
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCreateDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleCreate}
              disabled={!createForm.name || !createForm.slug || createMutation.isPending}
              className="gap-2"
            >
              {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Create Tenant
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Tenant Config Panel */}
      <Sheet open={!!selectedTenant} onOpenChange={() => { setSelectedTenant(null); setEditForm(null) }}>
        {selectedTenant && editForm && (
          <div className="space-y-6">
            <SheetHeader>
              <SheetTitle>{selectedTenant.name}</SheetTitle>
            </SheetHeader>

            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-xs text-text-secondary">Slug</span>
                <span className="font-mono text-xs text-text-primary">{selectedTenant.slug}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs text-text-secondary">SIM Count</span>
                <span className="font-mono text-xs text-text-primary">{formatNumber(selectedTenant.sim_count)}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs text-text-secondary">User Count</span>
                <span className="font-mono text-xs text-text-primary">{selectedTenant.user_count}</span>
              </div>

              <div className="border-t border-border pt-4">
                <p className="text-[10px] uppercase tracking-[1px] text-text-tertiary mb-3 font-medium">Configuration</p>

                <div className="space-y-3">
                  <div>
                    <label className="text-xs text-text-secondary block mb-1">Plan</label>
                    <Select
                      options={PLAN_OPTIONS}
                      value={editForm.plan}
                      onChange={(e) => setEditForm((f) => f && ({ ...f, plan: e.target.value }))}
                      className="h-8 text-xs"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-text-secondary block mb-1">Retention Days</label>
                    <Input
                      type="number"
                      value={editForm.retention_days}
                      onChange={(e) => setEditForm((f) => f && ({ ...f, retention_days: parseInt(e.target.value) || 0 }))}
                      className="h-8 text-xs"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-text-secondary block mb-1">Max SIMs</label>
                    <Input
                      type="number"
                      value={editForm.max_sims}
                      onChange={(e) => setEditForm((f) => f && ({ ...f, max_sims: parseInt(e.target.value) || 0 }))}
                      className="h-8 text-xs"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-text-secondary block mb-1">Max Users</label>
                    <Input
                      type="number"
                      value={editForm.max_users}
                      onChange={(e) => setEditForm((f) => f && ({ ...f, max_users: parseInt(e.target.value) || 0 }))}
                      className="h-8 text-xs"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-text-secondary block mb-1">Max API Keys</label>
                    <Input
                      type="number"
                      value={editForm.max_api_keys}
                      onChange={(e) => setEditForm((f) => f && ({ ...f, max_api_keys: parseInt(e.target.value) || 0 }))}
                      className="h-8 text-xs"
                    />
                  </div>
                </div>
              </div>

              <div className="flex gap-2 pt-2 border-t border-border">
                <Button
                  variant="outline"
                  size="sm"
                  className="flex-1"
                  onClick={() => { setSelectedTenant(null); setEditForm(null) }}
                >
                  Cancel
                </Button>
                <Button
                  size="sm"
                  className="flex-1 gap-1.5"
                  onClick={handleSaveConfig}
                  disabled={updateMutation.isPending}
                >
                  {updateMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  Save
                </Button>
              </div>
            </div>
          </div>
        )}
      </Sheet>
    </div>
  )
}
