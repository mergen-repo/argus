import * as React from 'react'
import { Link } from 'react-router-dom'
import {
  Smartphone,
  Radio,
  Cloud,
  ShieldCheck,
  User,
  Activity,
  Building2,
  Network,
  AlertTriangle,
  Bell,
  Sparkles,
  Briefcase,
  Key,
} from 'lucide-react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Tooltip } from '@/components/ui/tooltip'
import { EntityHoverCard } from './entity-hover-card'

export type EntityType =
  | 'sim'
  | 'apn'
  | 'operator'
  | 'policy'
  | 'user'
  | 'session'
  | 'tenant'
  | 'violation'
  | 'alert'
  | 'anomaly'
  | 'job'
  | 'apikey'
  | 'ippool'
  | 'esim_profile'

export const ENTITY_ROUTE_MAP: Record<EntityType, (id: string) => string> = {
  sim: (id) => `/sims/${id}`,
  apn: (id) => `/apns/${id}`,
  operator: (id) => `/operators/${id}`,
  policy: (id) => `/policies/${id}`,
  user: (id) => `/settings/users/${id}`,
  session: (id) => `/sessions/${id}`,
  tenant: (id) => `/system/tenants/${id}`,
  violation: (id) => `/violations/${id}`,
  alert: (id) => `/alerts/${id}`,
  anomaly: (id) => `/alerts/${id}`,
  job: (id) => `/jobs?highlight=${id}`,
  apikey: (id) => `/settings/api-keys?highlight=${id}`,
  ippool: (id) => `/settings/ip-pools/${id}`,
  esim_profile: (id) => `/esim?profile_id=${id}`,
}

const ENTITY_ICON_MAP: Partial<Record<EntityType, React.ElementType>> = {
  sim: Smartphone,
  operator: Radio,
  apn: Cloud,
  policy: ShieldCheck,
  user: User,
  session: Activity,
  tenant: Building2,
  ippool: Network,
  esim_profile: Smartphone,
  violation: AlertTriangle,
  alert: Bell,
  anomaly: Sparkles,
  job: Briefcase,
  apikey: Key,
}

interface EntityLinkProps {
  entityType: EntityType | string
  entityId: string
  label?: string
  className?: string
  truncate?: boolean
  showIcon?: boolean
  icon?: React.ReactNode
  hoverCard?: boolean
  copyOnRightClick?: boolean
}

function truncateMiddle(value: string, prefixLen = 8, suffixLen = 6): string {
  if (value.length <= prefixLen + suffixLen + 3) return value
  return `${value.slice(0, prefixLen)}…${value.slice(-suffixLen)}`
}

export const EntityLink = React.memo(function EntityLink({
  entityType,
  entityId,
  label,
  className,
  truncate = false,
  showIcon,
  icon,
  hoverCard = false,
  copyOnRightClick = true,
}: EntityLinkProps) {
  const routeFn = ENTITY_ROUTE_MAP[entityType as EntityType]

  if (!label && !entityId) {
    return (
      <span title="Entity reference broken" className="text-muted-foreground">
        —
      </span>
    )
  }

  const displayText = label ?? (truncate ? truncateMiddle(entityId) : entityId)
  const needsTooltip = truncate && !label

  const handleContextMenu = React.useCallback(
    (e: React.MouseEvent) => {
      if (!copyOnRightClick) return
      e.preventDefault()
      if (navigator.clipboard && entityId) {
        navigator.clipboard.writeText(entityId).then(() => toast.success('UUID copied'))
      }
    },
    [copyOnRightClick, entityId],
  )

  const resolvedIcon = React.useMemo(() => {
    if (!showIcon) return null
    if (icon !== undefined) return icon
    const IconComponent = ENTITY_ICON_MAP[entityType as EntityType]
    if (!IconComponent) return null
    return <IconComponent className="w-3.5 h-3.5 shrink-0" />
  }, [showIcon, icon, entityType])

  if (!routeFn) {
    return (
      <span
        className={cn('font-mono text-[12px] text-text-secondary inline-flex items-center gap-1', className)}
        onContextMenu={handleContextMenu}
      >
        {resolvedIcon}
        {displayText}
      </span>
    )
  }

  const href = routeFn(entityId)

  const linkEl = (
    <Link
      to={href}
      className={cn(
        'font-mono text-[12px] text-accent hover:text-accent/80 underline-offset-2 hover:underline transition-colors duration-200 inline-flex items-center gap-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1',
        className,
      )}
      aria-label={label ? `View ${entityType} ${label}` : `View ${entityType}`}
      onContextMenu={handleContextMenu}
    >
      {resolvedIcon}
      {displayText}
    </Link>
  )

  const wrappedLink = needsTooltip ? <Tooltip content={entityId}>{linkEl}</Tooltip> : linkEl

  if (hoverCard) {
    return (
      <EntityHoverCard entityType={entityType} entityId={entityId}>
        {wrappedLink}
      </EntityHoverCard>
    )
  }

  return wrappedLink
})
