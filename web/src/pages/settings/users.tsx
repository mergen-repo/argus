import { useState } from 'react'
import {
  UserPlus,
  AlertCircle,
  RefreshCw,
  Loader2,
  Search,
  X,
  Shield,
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
import { useUserList, useInviteUser, useUpdateUser } from '@/hooks/use-settings'
import { useAuthStore } from '@/stores/auth'
import { Skeleton } from '@/components/ui/skeleton'
import type { TenantUser } from '@/types/settings'

const ROLE_OPTIONS = [
  { value: 'viewer', label: 'Viewer' },
  { value: 'analyst', label: 'Analyst' },
  { value: 'sim_manager', label: 'SIM Manager' },
  { value: 'operator_manager', label: 'Operator Manager' },
  { value: 'policy_editor', label: 'Policy Editor' },
  { value: 'tenant_admin', label: 'Tenant Admin' },
]

function statusVariant(status: string): 'success' | 'warning' | 'secondary' {
  switch (status) {
    case 'active': return 'success'
    case 'invited': return 'warning'
    case 'deactivated': return 'secondary'
    default: return 'secondary'
  }
}

function roleLabel(role: string): string {
  const opt = ROLE_OPTIONS.find((o) => o.value === role)
  return opt?.label ?? role
}

export default function UsersPage() {
  const user = useAuthStore((s) => s.user)
  const isTenantAdmin = user?.role === 'tenant_admin' || user?.role === 'super_admin'

  const { data: users, isLoading, isError, refetch } = useUserList()
  const inviteMutation = useInviteUser()
  const updateMutation = useUpdateUser()

  const [searchQuery, setSearchQuery] = useState('')
  const [showInviteDialog, setShowInviteDialog] = useState(false)
  const [inviteForm, setInviteForm] = useState({ email: '', name: '', role: 'viewer' })
  const [editingRole, setEditingRole] = useState<{ userId: string; role: string } | null>(null)
  const [confirmDeactivate, setConfirmDeactivate] = useState<TenantUser | null>(null)

  if (!isTenantAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <Shield className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Access Denied</h2>
          <p className="text-sm text-text-secondary">You need tenant_admin or higher role to view this page.</p>
        </div>
      </div>
    )
  }

  const filtered = (users ?? []).filter((u) => {
    if (!searchQuery) return true
    const q = searchQuery.toLowerCase()
    return u.name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q)
  })

  const handleInvite = async () => {
    try {
      await inviteMutation.mutateAsync(inviteForm)
      setShowInviteDialog(false)
      setInviteForm({ email: '', name: '', role: 'viewer' })
    } catch {
      // handled by api interceptor
    }
  }

  const handleRoleChange = async (userId: string, newRole: string) => {
    try {
      await updateMutation.mutateAsync({ id: userId, role: newRole })
      setEditingRole(null)
    } catch {
      // handled by api interceptor
    }
  }

  const handleDeactivate = async () => {
    if (!confirmDeactivate) return
    try {
      await updateMutation.mutateAsync({ id: confirmDeactivate.id, status: 'deactivated' })
      setConfirmDeactivate(null)
    } catch {
      // handled by api interceptor
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load users</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch user data. Please try again.</p>
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
        <h1 className="text-[16px] font-semibold text-text-primary">Users & Roles</h1>
        <Button size="sm" className="gap-2" onClick={() => setShowInviteDialog(true)}>
          <UserPlus className="h-3.5 w-3.5" />
          Invite User
        </Button>
      </div>

      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search by name or email..."
            className="pl-9 h-8 text-sm"
          />
          {searchQuery && (
            <button
              onClick={() => setSearchQuery('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary transition-colors"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Email</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last Login</TableHead>
                <TableHead className="w-28">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 6 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && filtered.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <UserPlus className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No users found</h3>
                        <p className="text-xs text-text-secondary">
                          {searchQuery ? 'Try adjusting your search.' : 'Invite your first team member to get started.'}
                        </p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {filtered.map((u) => (
                <TableRow key={u.id}>
                  <TableCell>
                    <span className="text-sm font-medium text-text-primary">{u.name}</span>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">{u.email}</span>
                  </TableCell>
                  <TableCell>
                    {editingRole?.userId === u.id ? (
                      <Select
                        options={ROLE_OPTIONS}
                        value={editingRole.role}
                        onChange={(e) => handleRoleChange(u.id, e.target.value)}
                        className="h-7 text-xs w-40"
                      />
                    ) : (
                      <button
                        onClick={() => setEditingRole({ userId: u.id, role: u.role })}
                        className="text-xs text-text-secondary hover:text-accent transition-colors cursor-pointer"
                      >
                        {roleLabel(u.role)}
                      </button>
                    )}
                  </TableCell>
                  <TableCell>
                    <Badge variant={statusVariant(u.status)} className="text-[10px]">
                      {u.status.toUpperCase()}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {u.last_login_at ? new Date(u.last_login_at).toLocaleDateString() : 'Never'}
                    </span>
                  </TableCell>
                  <TableCell>
                    {u.status !== 'deactivated' && u.id !== user?.id && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-xs text-danger hover:text-danger h-7"
                        onClick={() => setConfirmDeactivate(u)}
                      >
                        Deactivate
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {!isLoading && filtered.length > 0 && (
          <div className="px-4 py-3 border-t border-border-subtle">
            <p className="text-center text-xs text-text-tertiary">
              Showing {filtered.length} user{filtered.length !== 1 ? 's' : ''}
            </p>
          </div>
        )}
      </Card>

      {/* Invite User Dialog */}
      <Dialog open={showInviteDialog} onOpenChange={setShowInviteDialog}>
        <DialogContent onClose={() => setShowInviteDialog(false)}>
          <DialogHeader>
            <DialogTitle>Invite User</DialogTitle>
            <DialogDescription>
              Send an invitation email to add a new team member.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-xs text-text-secondary block mb-1.5">Name</label>
              <Input
                value={inviteForm.name}
                onChange={(e) => setInviteForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="Full name"
              />
            </div>
            <div>
              <label className="text-xs text-text-secondary block mb-1.5">Email</label>
              <Input
                type="email"
                value={inviteForm.email}
                onChange={(e) => setInviteForm((f) => ({ ...f, email: e.target.value }))}
                placeholder="user@example.com"
              />
            </div>
            <div>
              <label className="text-xs text-text-secondary block mb-1.5">Role</label>
              <Select
                options={ROLE_OPTIONS}
                value={inviteForm.role}
                onChange={(e) => setInviteForm((f) => ({ ...f, role: e.target.value }))}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowInviteDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleInvite}
              disabled={!inviteForm.email || !inviteForm.name || inviteMutation.isPending}
              className="gap-2"
            >
              {inviteMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Send Invite
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Deactivate Confirmation Dialog */}
      <Dialog open={!!confirmDeactivate} onOpenChange={() => setConfirmDeactivate(null)}>
        <DialogContent onClose={() => setConfirmDeactivate(null)}>
          <DialogHeader>
            <DialogTitle>Deactivate User?</DialogTitle>
            <DialogDescription>
              This will revoke access for {confirmDeactivate?.name} ({confirmDeactivate?.email}).
              They will no longer be able to log in.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDeactivate(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeactivate}
              disabled={updateMutation.isPending}
              className="gap-2"
            >
              {updateMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Deactivate
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
