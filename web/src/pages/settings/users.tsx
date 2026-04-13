import { useState } from 'react'
import {
  UserPlus,
  AlertCircle,
  RefreshCw,
  Loader2,
  Search,
  X,
  Shield,
  MoreHorizontal,
  LockOpen,
  LogOut,
  KeyRound,
  Copy,
  Check,
  Download,
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
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { SlidePanel } from '@/components/ui/slide-panel'
import {
  useUserList,
  useInviteUser,
  useUpdateUser,
  useUnlockUser,
  useRevokeUserSessions,
  useResetUserPassword,
} from '@/hooks/use-settings'
import { useAuthStore } from '@/stores/auth'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import type { TenantUser } from '@/types/settings'
import { EmptyState } from '@/components/shared/empty-state'
import { useExport } from '@/hooks/use-export'

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

function isLocked(u: TenantUser): boolean {
  if (!u.locked_until) return false
  return new Date(u.locked_until) > new Date()
}

export default function UsersPage() {
  const user = useAuthStore((s) => s.user)
  const isTenantAdmin = user?.role === 'tenant_admin' || user?.role === 'super_admin'

  const { data: users, isLoading, isError, refetch } = useUserList()
  const { exportCSV, exporting } = useExport('users')
  const inviteMutation = useInviteUser()
  const updateMutation = useUpdateUser()
  const unlockMutation = useUnlockUser()
  const revokeSessionsMutation = useRevokeUserSessions()
  const resetPasswordMutation = useResetUserPassword()

  const [searchQuery, setSearchQuery] = useState('')
  const [showInviteDialog, setShowInviteDialog] = useState(false)
  const [inviteForm, setInviteForm] = useState({ email: '', name: '', role: 'viewer' })
  const [editingRole, setEditingRole] = useState<{ userId: string; role: string } | null>(null)
  const [confirmDeactivate, setConfirmDeactivate] = useState<TenantUser | null>(null)
  const [confirmRevokeSessions, setConfirmRevokeSessions] = useState<TenantUser | null>(null)
  const [confirmResetPassword, setConfirmResetPassword] = useState<TenantUser | null>(null)
  const [tempPasswordModal, setTempPasswordModal] = useState<{ name: string; password: string } | null>(null)
  const [copied, setCopied] = useState(false)

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
    }
  }

  const handleRoleChange = async (userId: string, newRole: string) => {
    try {
      await updateMutation.mutateAsync({ id: userId, role: newRole })
      setEditingRole(null)
    } catch {
    }
  }

  const handleDeactivate = async () => {
    if (!confirmDeactivate) return
    try {
      await updateMutation.mutateAsync({ id: confirmDeactivate.id, status: 'deactivated' })
      setConfirmDeactivate(null)
    } catch {
    }
  }

  const handleUnlock = async (u: TenantUser) => {
    try {
      await unlockMutation.mutateAsync(u.id)
    } catch {
    }
  }

  const handleRevokeSessions = async () => {
    if (!confirmRevokeSessions) return
    try {
      await revokeSessionsMutation.mutateAsync(confirmRevokeSessions.id)
      setConfirmRevokeSessions(null)
    } catch {
    }
  }

  const handleResetPassword = async () => {
    if (!confirmResetPassword) return
    try {
      const result = await resetPasswordMutation.mutateAsync(confirmResetPassword.id)
      setConfirmResetPassword(null)
      setTempPasswordModal({ name: confirmResetPassword.name, password: result.temp_password })
    } catch {
    }
  }

  const handleCopyPassword = async () => {
    if (!tempPasswordModal) return
    try {
      await navigator.clipboard.writeText(tempPasswordModal.password)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
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
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Users & Roles</h1>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" className="gap-2" onClick={() => exportCSV()} disabled={exporting}>
            {exporting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
            Export
          </Button>
          <Button size="sm" className="gap-2" onClick={() => setShowInviteDialog(true)}>
            <UserPlus className="h-3.5 w-3.5" />
            Invite User
          </Button>
        </div>
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
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setSearchQuery('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 h-5 w-5 text-text-tertiary hover:text-text-primary"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>

      <Card className="overflow-hidden density-compact">
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
                    {searchQuery ? (
                      <EmptyState
                        icon={Search}
                        title="No users match your search"
                        description="Try adjusting your search query."
                      />
                    ) : (
                      <EmptyState
                        icon={UserPlus}
                        title="No users yet"
                        description="Invite your first team member to get started."
                        ctaLabel="Invite User"
                        onCta={() => setShowInviteDialog(true)}
                      />
                    )}
                  </TableCell>
                </TableRow>
              )}

              {filtered.map((u, idx) => {
                const locked = isLocked(u)
                return (
                  <TableRow key={u.id} data-row-index={idx} data-href={`/settings/users/${u.id}`}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium text-text-primary">{u.name}</span>
                        {locked && (
                          <Badge variant="warning" className="text-[9px] px-1 py-0">LOCKED</Badge>
                        )}
                      </div>
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
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setEditingRole({ userId: u.id, role: u.role })}
                          className="text-xs text-text-secondary hover:text-accent transition-colors h-auto p-0"
                        >
                          {roleLabel(u.role)}
                        </Button>
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
                      <div className="flex items-center gap-1">
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
                        {u.id !== user?.id && (
                          <DropdownMenu>
                            <DropdownMenuTrigger
                              className={cn(
                                'inline-flex items-center justify-center h-7 w-7 rounded-[4px]',
                                'text-text-tertiary hover:text-text-primary hover:bg-bg-hover',
                                'transition-colors focus:outline-none',
                              )}
                              aria-label="More actions"
                            >
                              <MoreHorizontal className="h-3.5 w-3.5" />
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              {locked && (
                                <>
                                  <DropdownMenuItem
                                    onClick={() => handleUnlock(u)}
                                    disabled={unlockMutation.isPending}
                                  >
                                    <LockOpen className="h-3.5 w-3.5 text-success" />
                                    <span>Unlock account</span>
                                  </DropdownMenuItem>
                                  <DropdownMenuSeparator />
                                </>
                              )}
                              <DropdownMenuItem onClick={() => setConfirmRevokeSessions(u)}>
                                <LogOut className="h-3.5 w-3.5 text-warning" />
                                <span>Revoke sessions</span>
                              </DropdownMenuItem>
                              <DropdownMenuItem onClick={() => setConfirmResetPassword(u)}>
                                <KeyRound className="h-3.5 w-3.5 text-accent" />
                                <span>Reset password</span>
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
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

      {/* Invite User Panel */}
      <SlidePanel open={showInviteDialog} onOpenChange={setShowInviteDialog} title="Invite User" description="Send an invitation email to add a new team member." width="md">
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
        <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
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
        </div>
      </SlidePanel>

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

      {/* Revoke Sessions Confirmation */}
      <Dialog open={!!confirmRevokeSessions} onOpenChange={() => setConfirmRevokeSessions(null)}>
        <DialogContent onClose={() => setConfirmRevokeSessions(null)}>
          <DialogHeader>
            <DialogTitle>Revoke All Sessions?</DialogTitle>
            <DialogDescription>
              This will immediately sign out all active sessions for{' '}
              <strong>{confirmRevokeSessions?.name}</strong>. They will need to log in again.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmRevokeSessions(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleRevokeSessions}
              disabled={revokeSessionsMutation.isPending}
              className="gap-2"
            >
              {revokeSessionsMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Revoke Sessions
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reset Password Confirmation */}
      <Dialog open={!!confirmResetPassword} onOpenChange={() => setConfirmResetPassword(null)}>
        <DialogContent onClose={() => setConfirmResetPassword(null)}>
          <DialogHeader>
            <DialogTitle>Reset Password?</DialogTitle>
            <DialogDescription>
              A new temporary password will be generated for{' '}
              <strong>{confirmResetPassword?.name}</strong>. All their active sessions will be
              revoked. They will be required to change their password on next login.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmResetPassword(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleResetPassword}
              disabled={resetPasswordMutation.isPending}
              className="gap-2"
            >
              {resetPasswordMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Reset Password
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Temp Password Modal */}
      <Dialog open={!!tempPasswordModal} onOpenChange={() => setTempPasswordModal(null)}>
        <DialogContent onClose={() => setTempPasswordModal(null)}>
          <DialogHeader>
            <DialogTitle>Temporary Password</DialogTitle>
            <DialogDescription>
              Password reset successful for <strong>{tempPasswordModal?.name}</strong>. Share this
              temporary password securely — it will not be shown again after you close this dialog.
            </DialogDescription>
          </DialogHeader>
          <div className="mt-2 rounded-lg border border-border bg-bg-surface p-3 flex items-center justify-between gap-3">
            <span className="font-mono text-sm text-text-primary tracking-wider select-all">
              {tempPasswordModal?.password}
            </span>
            <Button
              variant="ghost"
              size="icon"
              className={cn(
                'h-8 w-8 shrink-0 transition-colors',
                copied ? 'text-success' : 'text-text-tertiary hover:text-text-primary',
              )}
              onClick={handleCopyPassword}
              aria-label="Copy password"
            >
              {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
            </Button>
          </div>
          <p className="text-xs text-text-tertiary mt-2">
            An email notification has been sent to the user.
          </p>
          <DialogFooter>
            <Button onClick={() => setTempPasswordModal(null)}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
