import { cn } from '@/lib/utils'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { QuotaBar } from '@/components/admin/quota-bar'
import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { TenantUsageItem, TenantPlan, TenantState } from '@/types/admin'

interface TenantUsageCardProps {
  item: TenantUsageItem
  onClick?: () => void
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

function overallStatus(item: TenantUsageItem): 'ok' | 'warning' | 'critical' {
  const metrics = [item.sims, item.sessions, item.api_rps, item.storage_bytes]
  if (metrics.some((m) => m.status === 'critical')) return 'critical'
  if (metrics.some((m) => m.status === 'warning')) return 'warning'
  return 'ok'
}

function isCritical(item: TenantUsageItem): boolean {
  return [item.sims, item.sessions, item.api_rps, item.storage_bytes].some(
    (m) => m.pct >= 95,
  )
}

function isWarning(item: TenantUsageItem): boolean {
  return [item.sims, item.sessions, item.api_rps, item.storage_bytes].some(
    (m) => m.pct >= 80 && m.pct < 95,
  )
}

function StatusIndicator({ status }: { status: 'ok' | 'warning' | 'critical' }) {
  if (status === 'critical') {
    return (
      <span className="inline-flex items-center gap-1 text-xs font-medium text-danger">
        <span className="h-2 w-2 rounded-full bg-danger animate-pulse" />
        CRITICAL
      </span>
    )
  }
  if (status === 'warning') {
    return (
      <span className="inline-flex items-center gap-1 text-xs font-medium text-warning">
        <span className="h-2 w-2 rounded-full bg-warning" />
        WARNING
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs font-medium text-success">
      <span className="h-2 w-2 rounded-full bg-success" />
      OK
    </span>
  )
}

function TenantUsageCard({ item, onClick }: TenantUsageCardProps) {
  const status = overallStatus(item)
  const critical = isCritical(item)
  const warning = !critical && isWarning(item)

  return (
    <Card
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onClick?.()
        }
      }}
      aria-label={`${item.tenant_name} usage details`}
      className={cn(
        'cursor-pointer transition-all duration-300 hover:shadow-[var(--shadow-card-hover)] hover:border-accent-primary/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary/50',
        critical && 'ring-2 ring-danger/50 motion-safe:animate-pulse motion-reduce:animate-none',
        warning && 'ring-2 ring-warning/40 motion-safe:animate-pulse motion-reduce:animate-none',
      )}
    >
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-text-primary truncate leading-tight">
              {item.tenant_name}
            </h3>
            <div className="flex items-center gap-1.5 mt-1.5 flex-wrap">
              <Badge variant={PLAN_VARIANT[item.plan]} className="capitalize">
                {item.plan}
              </Badge>
              <Badge variant={STATE_VARIANT[item.state]} className="uppercase">
                {item.state}
              </Badge>
            </div>
          </div>
          <StatusIndicator status={status} />
        </div>
      </CardHeader>

      <CardContent className="space-y-3">
        <QuotaBar
          label="SIMs"
          current={item.sims.current}
          max={item.sims.max}
          unit="count"
        />
        <QuotaBar
          label="Sessions"
          current={item.sessions.current}
          max={item.sessions.max}
          unit="count"
        />
        <QuotaBar
          label="API RPS"
          current={item.api_rps.current}
          max={item.api_rps.max}
          unit="rps"
        />
        <QuotaBar
          label="Storage"
          current={item.storage_bytes.current}
          max={item.storage_bytes.max}
          unit="bytes"
        />

        <div className="pt-1 flex items-center justify-between border-t border-border">
          <p className="text-xs text-text-secondary">
            State:{' '}
            <span className="font-medium text-text-primary uppercase">{item.state}</span>
          </p>
          <Link
            to={`/system/tenants/${item.tenant_id}`}
            onClick={(e) => e.stopPropagation()}
            className={cn(
              buttonVariants({ variant: 'ghost', size: 'sm' }),
              'h-auto px-1.5 py-0.5 text-xs text-accent hover:text-accent',
            )}
          >
            Edit limits <ChevronRight className="ml-0.5 h-3 w-3" />
          </Link>
        </div>
      </CardContent>
    </Card>
  )
}

export { TenantUsageCard }
export type { TenantUsageCardProps }
