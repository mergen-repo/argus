import { useNavigate } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

interface EventEntityButtonProps {
  entity?: { type: string; id: string; display_name?: string }
  entityTypeFallback?: string
  entityIdFallback?: string
  onNavigate?: () => void
  className?: string
}

// D13 route map — unknown types render as non-clickable span, no 404 risk.
const ROUTE_MAP: Record<string, (id: string) => string> = {
  sim: (id) => `/sims/${id}`,
  operator: (id) => `/operators/${id}`,
  apn: (id) => `/apns/${id}`,
  policy: (id) => `/policies/${id}`,
  session: (id) => `/sessions/${id}`,
  agreement: (id) => `/roaming-agreements/${id}`,
  roaming_agreement: (id) => `/roaming-agreements/${id}`,
  consumer: () => '/ops/infra',
  system: () => '/system/health',
  job: () => '/jobs',
  anomaly: () => '/analytics/anomalies',
  violation: (id) => `/violations/${id}`,
  alert: (id) => `/alerts/${id}`,
  tenant: (id) => `/system/tenants/${id}`,
  user: (id) => `/settings/users/${id}`,
}

export function EventEntityButton({
  entity,
  entityTypeFallback,
  entityIdFallback,
  onNavigate,
  className,
}: EventEntityButtonProps) {
  const navigate = useNavigate()

  const type = entity?.type || entityTypeFallback
  const id = entity?.id || entityIdFallback
  const label = entity?.display_name || id

  if (!type || !id || !label) return null

  const routeFn = ROUTE_MAP[type]

  if (!routeFn) {
    return (
      <span className={cn('inline-flex items-center gap-1 text-[11px] text-text-secondary', className)}>
        <span className="opacity-60">{type}</span>
        <span className="font-mono">{label}</span>
      </span>
    )
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      onClick={(e) => {
        e.stopPropagation()
        onNavigate?.()
        navigate(routeFn(id))
      }}
      className={cn(
        'h-auto gap-0.5 px-0.5 py-0 text-[11px] text-accent hover:bg-transparent hover:text-accent-bright hover:underline',
        'focus-visible:ring-1 focus-visible:ring-accent',
        className,
      )}
    >
      <span className="font-mono">{label}</span>
      <ChevronRight className="h-2.5 w-2.5 opacity-70" />
    </Button>
  )
}
