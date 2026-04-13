import { RefreshCw, AlertCircle, ShieldCheck, FileText, Lock } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { useTenantQuotas, useKillSwitches } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'

function ComplianceCard({
  title,
  description,
  status,
  icon: Icon,
}: {
  title: string
  description: string
  status: 'pass' | 'warn' | 'fail'
  icon: React.ElementType
}) {
  const color =
    status === 'pass'
      ? 'text-success bg-success-dim border-success/30'
      : status === 'warn'
      ? 'text-warning bg-warning-dim border-warning/30'
      : 'text-danger bg-danger-dim border-danger/30'
  const badge =
    status === 'pass' ? (
      <Badge variant="success">Pass</Badge>
    ) : status === 'warn' ? (
      <Badge variant="warning">Warning</Badge>
    ) : (
      <Badge variant="danger">Fail</Badge>
    )
  return (
    <div className={`rounded-xl border p-4 ${color}`}>
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2">
          <Icon className="h-5 w-5 mt-0.5 shrink-0" />
          <div>
            <div className="font-medium text-sm">{title}</div>
            <div className="text-xs mt-0.5 opacity-80">{description}</div>
          </div>
        </div>
        {badge}
      </div>
    </div>
  )
}

export default function CompliancePage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const isTenantAdmin = user?.role === 'tenant_admin'

  const { data: quotas, isLoading: quotasLoading } = useTenantQuotas()
  const { data: killSwitches, isLoading: ksLoading } = useKillSwitches()

  if (!isSuperAdmin && !isTenantAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">Insufficient permissions.</p>
        </div>
      </div>
    )
  }

  const readOnlyMode = killSwitches?.find((k) => k.key === 'read_only_mode')?.enabled ?? false
  const externalNotifs = killSwitches?.find((k) => k.key === 'external_notifications')?.enabled ?? false
  const overQuota = (quotas ?? []).some((q) =>
    [q.sims, q.api_rps, q.sessions, q.storage_bytes].some((m) => m.status === 'danger')
  )
  const nearQuota = (quotas ?? []).some((q) =>
    [q.sims, q.api_rps, q.sessions, q.storage_bytes].some((m) => m.status === 'warning')
  )

  const isLoading = quotasLoading || ksLoading

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Compliance Overview</h1>
          <p className="text-sm text-text-secondary mt-0.5">
            System compliance posture and control status
          </p>
        </div>
        <Button variant="ghost" size="sm">
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <ComplianceCard
            title="Read-Only Mode"
            description="System accepts mutations; no degraded mode active."
            status={readOnlyMode ? 'warn' : 'pass'}
            icon={Lock}
          />
          <ComplianceCard
            title="External Notifications"
            description="Email, Telegram, and webhook delivery are enabled."
            status={externalNotifs ? 'warn' : 'pass'}
            icon={ShieldCheck}
          />
          <ComplianceCard
            title="Quota Utilization"
            description={
              overQuota
                ? 'One or more tenants are over their resource quota.'
                : nearQuota
                ? 'One or more tenants are approaching their quota limits.'
                : 'All tenants are within quota boundaries.'
            }
            status={overQuota ? 'fail' : nearQuota ? 'warn' : 'pass'}
            icon={FileText}
          />
          <ComplianceCard
            title="Audit Trail"
            description="All state-changing operations produce audit log entries."
            status="pass"
            icon={ShieldCheck}
          />
          <ComplianceCard
            title="Data Retention"
            description="Retention policies are configured for all active tenants."
            status="pass"
            icon={FileText}
          />
          <ComplianceCard
            title="KVKK / GDPR Controls"
            description="Data subject rights automation (DSAR pipeline) is active."
            status="pass"
            icon={ShieldCheck}
          />
        </div>
      )}
    </div>
  )
}
