import * as React from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  User,
  Shield,
  Key,
  Activity,
  Monitor,
  AlertCircle,
  Lock,
  RefreshCw,
  LogOut,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { InfoRow } from '@/components/ui/info-row'
import { RelatedAuditTab, RelatedNotificationsPanel, CopyableId } from '@/components/shared'
import { useUserDetail, useUserActivity, useUserSessions, useResetUserPassword, useRevokeUserSessions, useUnlockUser } from '@/hooks/use-settings'
import { timeAgo } from '@/lib/format'
import { toast } from 'sonner'

const ROLE_LABELS: Record<string, string> = {
  super_admin: 'Super Admin',
  tenant_admin: 'Tenant Admin',
  operator_manager: 'Operator Manager',
  sim_manager: 'SIM Manager',
  policy_editor: 'Policy Editor',
  analyst: 'Analyst',
  api_user: 'API User',
}

const ROLE_PERMISSIONS: Record<string, string[]> = {
  super_admin: ['All permissions'],
  tenant_admin: ['Users', 'SIMs', 'APNs', 'Operators', 'Policies', 'Reports', 'Settings'],
  operator_manager: ['Operators', 'SIMs (read)', 'Reports'],
  sim_manager: ['SIMs', 'Sessions', 'Policies (read)'],
  policy_editor: ['Policies', 'SIM Segments', 'Violations'],
  analyst: ['Analytics', 'Reports', 'Audit (read)'],
  api_user: ['API access only'],
}

export default function UserDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = React.useState('overview')

  const { data: user, isLoading, isError } = useUserDetail(id)
  const { data: activity = [] } = useUserActivity(id)
  const { data: sessions = [] } = useUserSessions(id)

  const resetPassword = useResetUserPassword()
  const revokeSessions = useRevokeUserSessions()
  const unlock = useUnlockUser()

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (isError || !user) {
    return (
      <div className="p-6 flex flex-col items-center justify-center py-20 text-center">
        <AlertCircle className="h-12 w-12 text-danger mb-4 opacity-60" />
        <p className="text-[15px] font-semibold text-text-primary mb-2">User not found</p>
        <Button variant="outline" onClick={() => navigate('/settings/users')}>
          <ArrowLeft className="h-4 w-4 mr-2" /> Back to Users
        </Button>
      </div>
    )
  }

  const permissions = ROLE_PERMISSIONS[user.role] ?? ['Custom role']

  return (
    <div className="p-6 space-y-6">
      <Breadcrumb
        items={[
          { label: 'Settings', href: '/settings' },
          { label: 'Users', href: '/settings/users' },
          { label: user.name || user.email },
        ]}
      />

      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate(-1)} className="mt-0.5">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <div className="flex items-center gap-2 mb-1">
              <div className="h-9 w-9 rounded-full bg-accent/15 flex items-center justify-center">
                <User className="h-4.5 w-4.5 text-accent" />
              </div>
              <div>
                <h1 className="text-[15px] font-semibold text-text-primary">{user.name}</h1>
                <p className="text-[12px] text-text-secondary">{user.email}</p>
              </div>
            </div>
            <div className="flex items-center gap-2 mt-1">
              <Badge variant="secondary" className="text-[11px]">
                {ROLE_LABELS[user.role] ?? user.role}
              </Badge>
              <Badge
                variant={user.state === 'active' ? 'success' : user.state === 'deactivated' ? 'secondary' : 'warning'}
                className="text-[11px]"
              >
                {user.state}
              </Badge>
              {user.totp_enabled && (
                <Badge variant="success" className="text-[10px] gap-1">
                  <Shield className="h-2.5 w-2.5" /> 2FA
                </Badge>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {user.locked_until && (
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={() => {
                if (!id) return
                unlock.mutate(id, {
                  onSuccess: () => toast.success('User unlocked'),
                  onError: () => toast.error('Failed to unlock'),
                })
              }}
            >
              <Lock className="h-3.5 w-3.5" />
              Unlock
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5"
            onClick={() => {
              if (!id) return
              resetPassword.mutate(id, {
                onSuccess: () => toast.success('Password reset email sent'),
                onError: () => toast.error('Failed to reset password'),
              })
            }}
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Reset Password
          </Button>
          <Button
            variant="destructive"
            size="sm"
            className="gap-1.5"
            onClick={() => {
              if (!id) return
              revokeSessions.mutate(id, {
                onSuccess: () => toast.success('All sessions revoked'),
                onError: () => toast.error('Failed to revoke sessions'),
              })
            }}
          >
            <LogOut className="h-3.5 w-3.5" />
            Revoke Sessions
          </Button>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <User className="h-3.5 w-3.5" />
            Overview
          </TabsTrigger>
          <TabsTrigger value="activity" className="gap-1.5">
            <Activity className="h-3.5 w-3.5" />
            Activity
          </TabsTrigger>
          <TabsTrigger value="sessions" className="gap-1.5">
            <Monitor className="h-3.5 w-3.5" />
            Sessions
          </TabsTrigger>
          <TabsTrigger value="permissions" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Permissions
          </TabsTrigger>
          <TabsTrigger value="notifications" className="gap-1.5">
            <Key className="h-3.5 w-3.5" />
            Notifications
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
              <CardHeader className="py-3 px-4 border-b border-border-subtle">
                <CardTitle className="text-[13px] font-medium text-text-primary">Account Details</CardTitle>
              </CardHeader>
              <CardContent className="p-4 space-y-2">
                <InfoRow label="Email" value={<CopyableId value={user.email} mono={false} />} />
                <InfoRow label="Role" value={<Badge variant="secondary" className="text-[11px]">{ROLE_LABELS[user.role] ?? user.role}</Badge>} />
                <InfoRow label="State" value={<Badge variant={user.state === 'active' ? 'success' : 'secondary'} className="text-[11px]">{user.state}</Badge>} />
                <InfoRow label="2FA" value={<span className={`text-[12px] ${user.totp_enabled ? 'text-success' : 'text-text-tertiary'}`}>{user.totp_enabled ? 'Enabled' : 'Disabled'}</span>} />
                <InfoRow label="Created" value={<span className="text-[12px] text-text-secondary" title={user.created_at}>{timeAgo(user.created_at)}</span>} />
                {user.last_login_at && (
                  <InfoRow label="Last Login" value={<span className="text-[12px] text-text-secondary" title={user.last_login_at}>{timeAgo(user.last_login_at)}</span>} />
                )}
                {user.locked_until && (
                  <InfoRow label="Locked Until" value={<span className="text-[12px] text-warning">{new Date(user.locked_until).toLocaleString()}</span>} />
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="activity" className="mt-4">
          {activity.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-10 text-center">
              <Activity className="h-8 w-8 text-text-tertiary mx-auto mb-3 opacity-40" />
              <p className="text-[13px] text-text-secondary">No activity recorded for this user</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="border-b border-border-subtle hover:bg-transparent">
                  <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Action</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Entity</TableHead>
                  <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Time</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {activity.map((entry, i) => (
                  <TableRow key={(entry.id as string | number | undefined) ?? i} className="hover:bg-bg-hover transition-colors duration-150">
                    <TableCell className="py-2.5">
                      <Badge variant="secondary" className="text-[10px]">
                        {entry.action as string}
                      </Badge>
                    </TableCell>
                    <TableCell className="py-2.5">
                      <span className="text-[12px] font-mono text-text-secondary">
                        {(entry.entity_type as string | undefined) && `${entry.entity_type}/`}{entry.entity_id as string | undefined}
                      </span>
                    </TableCell>
                    <TableCell className="py-2.5">
                      <span className="text-[11px] text-text-tertiary">{timeAgo(entry.created_at as string)}</span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </TabsContent>

        <TabsContent value="sessions" className="mt-4">
          <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
            <CardHeader className="py-3 px-4 border-b border-border-subtle">
              <CardTitle className="text-[13px] font-medium text-text-primary">Active Browser Sessions</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {sessions.length === 0 ? (
                <div className="py-8 text-center">
                  <Monitor className="h-8 w-8 text-text-tertiary mx-auto mb-2 opacity-40" />
                  <p className="text-[13px] text-text-secondary">No active sessions</p>
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow className="border-b border-border-subtle hover:bg-transparent">
                      <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">IP Address</TableHead>
                      <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Created</TableHead>
                      <TableHead className="text-[10px] uppercase tracking-[0.5px] text-text-secondary font-medium py-2">Expires</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sessions.map((s) => (
                      <TableRow key={s.id} className="hover:bg-bg-hover transition-colors duration-150">
                        <TableCell className="py-2.5">
                          <span className="text-[12px] font-mono text-text-primary">{s.ip_address ?? '-'}</span>
                        </TableCell>
                        <TableCell className="py-2.5">
                          <span className="text-[11px] text-text-tertiary">{timeAgo(s.created_at)}</span>
                        </TableCell>
                        <TableCell className="py-2.5">
                          <span className="text-[11px] text-text-tertiary">{new Date(s.expires_at).toLocaleDateString()}</span>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="permissions" className="mt-4">
          <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
            <CardHeader className="py-3 px-4 border-b border-border-subtle">
              <CardTitle className="text-[13px] font-medium text-text-primary">
                Role: {ROLE_LABELS[user.role] ?? user.role}
              </CardTitle>
            </CardHeader>
            <CardContent className="p-4">
              <div className="space-y-2">
                {permissions.map((perm) => (
                  <div key={perm} className="flex items-center gap-2 p-2.5 rounded-[10px] bg-bg-primary border border-border">
                    <Shield className="h-3.5 w-3.5 text-success flex-shrink-0" />
                    <span className="text-[13px] text-text-primary">{perm}</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="notifications" className="mt-4">
          {id && <RelatedNotificationsPanel entityId={id} limit={10} />}
        </TabsContent>
      </Tabs>
    </div>
  )
}
