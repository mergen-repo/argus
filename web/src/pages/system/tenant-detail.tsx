import * as React from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Building2,
  Users,
  Wifi,
  Server,
  Activity,
  DollarSign,
  HardDrive,
  AlertCircle,
  Shield,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/skeleton'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { InfoRow } from '@/components/ui/info-row'
import { AnimatedCounter } from '@/components/ui/animated-counter'
import { RelatedAuditTab, RelatedAlertsPanel } from '@/components/shared'
import { useTenantDetail, useTenantStats } from '@/hooks/use-tenant-detail'
import { useAuthStore } from '@/stores/auth'
import { formatCurrency, formatBytes, timeAgo } from '@/lib/format'

export default function TenantDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = React.useState('overview')
  const user = useAuthStore((s) => s.user)

  const { data: tenant, isLoading: tenantLoading } = useTenantDetail(id)
  const { data: stats, isLoading: statsLoading } = useTenantStats(id)

  const isSuperAdmin = user?.role === 'super_admin'

  if (!isSuperAdmin) {
    return (
      <div className="p-6 flex flex-col items-center justify-center py-20 text-center">
        <Shield className="h-12 w-12 text-danger mb-4 opacity-60" />
        <p className="text-[15px] font-semibold text-text-primary mb-2">Access Denied</p>
        <p className="text-[13px] text-text-secondary mb-6">Super admin role required to view tenant details.</p>
        <Button variant="outline" onClick={() => navigate('/system/tenants')}>
          <ArrowLeft className="h-4 w-4 mr-2" /> Back to Tenants
        </Button>
      </div>
    )
  }

  if (tenantLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-64" />
        <div className="grid grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-24 w-full" />)}
        </div>
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (!tenant) {
    return (
      <div className="p-6 flex flex-col items-center justify-center py-20 text-center">
        <AlertCircle className="h-12 w-12 text-danger mb-4 opacity-60" />
        <p className="text-[15px] font-semibold text-text-primary mb-2">Tenant not found</p>
        <Button variant="outline" onClick={() => navigate('/system/tenants')}>
          <ArrowLeft className="h-4 w-4 mr-2" /> Back to Tenants
        </Button>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6">
      <Breadcrumb
        items={[
          { label: 'System', href: '/system' },
          { label: 'Tenants', href: '/system/tenants' },
          { label: tenant.name },
        ]}
      />

      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Button variant="ghost" size="icon" onClick={() => navigate(-1)} className="mt-0.5">
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <div className="flex items-center gap-2 mb-1">
              <Building2 className="h-5 w-5 text-accent" />
              <h1 className="text-[15px] font-semibold text-text-primary">{tenant.name}</h1>
            </div>
            <div className="flex items-center gap-2">
              {tenant.slug && (
                <span className="text-[12px] font-mono text-text-tertiary">{tenant.slug}</span>
              )}
              <Badge
                variant={tenant.state === 'active' ? 'success' : 'secondary'}
                className="text-[11px]"
              >
                {tenant.state}
              </Badge>
              {tenant.plan && (
                <Badge variant="secondary" className="text-[11px]">{tenant.plan}</Badge>
              )}
            </div>
          </div>
        </div>
      </div>

      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
          {[
            { icon: Wifi, label: 'SIMs', value: stats.sim_count, color: 'text-accent' },
            { icon: Server, label: 'APNs', value: stats.apn_count, color: 'text-purple' },
            { icon: Users, label: 'Users', value: stats.user_count, color: 'text-success' },
            { icon: Building2, label: 'Operators', value: stats.operator_count, color: 'text-warning' },
            { icon: Activity, label: 'Active Sessions', value: stats.active_sessions, color: 'text-accent' },
            { icon: DollarSign, label: 'Monthly Cost', value: stats.monthly_cost, color: 'text-success', formatter: (n: number) => formatCurrency(n) },
          ].map(({ icon: Icon, label, value, color, formatter }) => (
            <Card key={label} className="bg-bg-surface border-border shadow-card rounded-[10px]">
              <CardContent className="p-4">
                <div className="flex items-center gap-2 mb-1">
                  <Icon className={`h-3.5 w-3.5 ${color}`} />
                  <span className="text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium">{label}</span>
                </div>
                <p className={`text-[28px] font-bold font-mono ${color}`}>
                  <AnimatedCounter value={value ?? 0} formatter={formatter} />
                </p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {stats?.storage_bytes !== undefined && (
        <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
          <CardHeader className="py-3 px-4 border-b border-border-subtle">
            <CardTitle className="text-[13px] font-medium text-text-primary flex items-center gap-2">
              <HardDrive className="h-4 w-4 text-text-tertiary" />
              Storage
            </CardTitle>
          </CardHeader>
          <CardContent className="p-4">
            <InfoRow label="Storage Used" value={<span className="text-[13px] font-mono text-text-primary">{formatBytes(stats.storage_bytes)}</span>} />
            {stats.quota_utilization !== undefined && (
              <div className="mt-3">
                <div className="flex justify-between mb-1">
                  <span className="text-[11px] text-text-tertiary">Quota Utilization</span>
                  <span className="text-[11px] text-text-secondary">{stats.quota_utilization.toFixed(1)}%</span>
                </div>
                <div className="w-full bg-bg-primary rounded-full h-2 overflow-hidden">
                  <div
                    className={`h-2 rounded-full transition-all duration-500 ${stats.quota_utilization >= 90 ? 'bg-danger' : stats.quota_utilization >= 70 ? 'bg-warning' : 'bg-success'}`}
                    style={{ width: `${Math.min(stats.quota_utilization, 100)}%` }}
                  />
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview" className="gap-1.5">
            <Building2 className="h-3.5 w-3.5" />
            Overview
          </TabsTrigger>
          <TabsTrigger value="audit" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Audit
          </TabsTrigger>
          <TabsTrigger value="alerts" className="gap-1.5">
            <AlertCircle className="h-3.5 w-3.5" />
            Alerts
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="mt-4">
          <Card className="bg-bg-surface border-border shadow-card rounded-[10px]">
            <CardHeader className="py-3 px-4 border-b border-border-subtle">
              <CardTitle className="text-[13px] font-medium text-text-primary">Tenant Configuration</CardTitle>
            </CardHeader>
            <CardContent className="p-4 space-y-2">
              <InfoRow label="Name" value={<span className="text-[13px] text-text-primary">{tenant.name}</span>} />
              {tenant.slug && <InfoRow label="Slug" value={<span className="text-[12px] font-mono text-text-secondary">{tenant.slug}</span>} />}
              {tenant.plan && <InfoRow label="Plan" value={<Badge variant="secondary" className="text-[11px]">{tenant.plan}</Badge>} />}
              {tenant.created_at && <InfoRow label="Created" value={<span className="text-[12px] text-text-secondary" title={tenant.created_at}>{timeAgo(tenant.created_at)}</span>} />}
              {tenant.max_sims && <InfoRow label="SIM Quota" value={<span className="text-[12px] text-text-secondary">{tenant.max_sims.toLocaleString()}</span>} />}
              {tenant.max_users && <InfoRow label="User Quota" value={<span className="text-[12px] text-text-secondary">{tenant.max_users.toLocaleString()}</span>} />}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="audit" className="mt-4">
          {id && <RelatedAuditTab entityId={id} entityType="tenant" />}
        </TabsContent>

        <TabsContent value="alerts" className="mt-4">
          {id && <RelatedAlertsPanel entityId={id} entityType="tenant" />}
        </TabsContent>
      </Tabs>

      {statsLoading && (
        <div className="text-center py-4">
          <span className="text-[11px] text-text-tertiary">Loading stats…</span>
        </div>
      )}
    </div>
  )
}
