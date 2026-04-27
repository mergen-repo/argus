import { Link } from 'react-router-dom'
import { ExternalLink, TrendingUp } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SlidePanel } from '@/components/ui/slide-panel'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { QuotaBar } from '@/components/admin/quota-bar'
import { formatBytes, formatNumber } from '@/lib/format'
import type { TenantUsageItem, TenantPlan, TenantState } from '@/types/admin'

// D-NNN (deferred): useTenantUsageTrend hook + GET /api/v1/admin/tenants/{id}/usage-trend?days=7
// endpoint not yet implemented in backend. Sparkline renders "Trend unavailable" placeholder.
// D-NNN (deferred): recent_breaches field not yet on TenantUsageItem — open_breach_count shown only.
// Unblock: add BreachEvent[] to TenantUsageItem + populate in ListTenantUsage handler.

interface TenantUsageDetailPanelProps {
  tenant: TenantUsageItem | null
  open: boolean
  onClose: () => void
}

const PLAN_VARIANT: Record<TenantPlan, 'default' | 'success' | 'warning'> = {
  starter: 'default',
  standard: 'success',
  enterprise: 'warning',
}

const STATE_VARIANT: Record<TenantState, 'success' | 'danger' | 'warning'> = {
  active: 'success',
  suspended: 'danger',
  trial: 'warning',
}

interface MetricSectionProps {
  label: string
  current: number
  max: number
  unit: 'count' | 'bytes' | 'rps'
}

function MetricSection({ label, current, max, unit }: MetricSectionProps) {
  const currentDisplay =
    unit === 'bytes'
      ? formatBytes(current)
      : unit === 'rps'
        ? `${formatNumber(current)}/s`
        : formatNumber(current)
  const maxDisplay =
    unit === 'bytes'
      ? formatBytes(max)
      : unit === 'rps'
        ? `${formatNumber(max)}/s`
        : formatNumber(max)

  return (
    <div className="space-y-2 rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-3">
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold text-text-primary uppercase tracking-wide">
          {label}
        </span>
        <span className="text-xs text-text-secondary tabular-nums">
          {currentDisplay} / {maxDisplay}
        </span>
      </div>
      <QuotaBar label={label} current={current} max={max} unit={unit} />
      <div className="flex items-center gap-1.5 pt-0.5">
        <TrendingUp className="h-3 w-3 text-text-tertiary" />
        <p className="text-xs text-text-tertiary italic">
          Trend unavailable — 7-day history endpoint pending (tech debt D-170)
        </p>
      </div>
    </div>
  )
}

function TenantUsageDetailPanel({ tenant, open, onClose }: TenantUsageDetailPanelProps) {
  const description = tenant
    ? `${tenant.plan.charAt(0).toUpperCase() + tenant.plan.slice(1)} plan · ${tenant.user_count} users`
    : undefined

  return (
    <SlidePanel
      open={open}
      onOpenChange={(v) => { if (!v) onClose() }}
      title={tenant?.tenant_name ?? 'Tenant Usage'}
      description={description}
      width="lg"
    >
      {tenant && (
        <div className="space-y-5">
          <div className="flex items-center gap-2 flex-wrap">
            <Badge variant={PLAN_VARIANT[tenant.plan]} className="capitalize">
              {tenant.plan}
            </Badge>
            <Badge variant={STATE_VARIANT[tenant.state]} className="uppercase">
              {tenant.state}
            </Badge>
            {tenant.open_breach_count > 0 && (
              <Badge variant="danger">
                {tenant.open_breach_count} open breach{tenant.open_breach_count > 1 ? 'es' : ''}
              </Badge>
            )}
          </div>

          <div>
            <h4 className="text-xs font-semibold text-text-secondary uppercase tracking-wider mb-3">
              Quota Utilization — 7-Day Trend
            </h4>
            <div className="space-y-3">
              <MetricSection
                label="SIMs"
                current={tenant.sims.current}
                max={tenant.sims.max}
                unit="count"
              />
              <MetricSection
                label="Sessions"
                current={tenant.sessions.current}
                max={tenant.sessions.max}
                unit="count"
              />
              <MetricSection
                label="API RPS"
                current={tenant.api_rps.current}
                max={tenant.api_rps.max}
                unit="rps"
              />
              <MetricSection
                label="Storage"
                current={tenant.storage_bytes.current}
                max={tenant.storage_bytes.max}
                unit="bytes"
              />
            </div>
          </div>

          <div>
            <h4 className="text-xs font-semibold text-text-secondary uppercase tracking-wider mb-3">
              Recent Breach Events
            </h4>
            <div className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-4">
              {tenant.open_breach_count > 0 ? (
                <p className="text-sm text-text-secondary">
                  {tenant.open_breach_count} active breach{tenant.open_breach_count > 1 ? 'es' : ''} recorded.
                  <br />
                  <span className="text-xs text-text-tertiary italic mt-1 block">
                    Detailed breach event list pending (tech debt D-171 — add{' '}
                    <code className="font-mono">recent_breaches</code> to TenantUsageItem).
                  </span>
                </p>
              ) : (
                <p className="text-sm text-text-secondary">No active breach events.</p>
              )}
            </div>
          </div>

          <div>
            <h4 className="text-xs font-semibold text-text-secondary uppercase tracking-wider mb-2">
              Activity (30 days)
            </h4>
            <div className="flex items-center gap-6 rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-4 py-3">
              <div className="text-center">
                <p className="text-lg font-semibold text-text-primary tabular-nums">
                  {formatNumber(tenant.user_count)}
                </p>
                <p className="text-xs text-text-secondary">Users</p>
              </div>
              <div className="text-center">
                <p className="text-lg font-semibold text-text-primary tabular-nums">
                  {formatBytes(tenant.cdr_bytes_30d)}
                </p>
                <p className="text-xs text-text-secondary">CDR Data (30d)</p>
              </div>
              <div className="text-center">
                <p className="text-lg font-semibold text-text-primary tabular-nums">
                  {formatNumber(tenant.sims.current)}
                </p>
                <p className="text-xs text-text-secondary">Active SIMs</p>
              </div>
            </div>
          </div>

          <div className="flex justify-end pt-1">
            <Link
              to={`/system/tenants/${tenant.tenant_id}`}
              className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}
            >
              Edit limits
              <ExternalLink className="ml-1.5 h-3.5 w-3.5" />
            </Link>
          </div>
        </div>
      )}
    </SlidePanel>
  )
}

export { TenantUsageDetailPanel }
export type { TenantUsageDetailPanelProps }
