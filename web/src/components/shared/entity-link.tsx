import * as React from 'react'
import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { Tooltip } from '@/components/ui/tooltip'

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

const ENTITY_ROUTE_MAP: Record<EntityType, (id: string) => string> = {
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
}

interface EntityLinkProps {
  entityType: EntityType | string
  entityId: string
  label?: string
  className?: string
  truncate?: boolean
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
}: EntityLinkProps) {
  const routeFn = ENTITY_ROUTE_MAP[entityType as EntityType]
  const displayText = label ?? (truncate ? truncateMiddle(entityId) : entityId)
  const needsTooltip = truncate && !label

  if (!routeFn) {
    return (
      <span className={cn('font-mono text-[12px] text-text-secondary', className)}>
        {displayText}
      </span>
    )
  }

  const href = routeFn(entityId)

  const linkEl = (
    <Link
      to={href}
      className={cn(
        'font-mono text-[12px] text-accent hover:text-accent/80 underline-offset-2 hover:underline transition-colors duration-200',
        className,
      )}
      aria-label={`View ${entityType} ${entityId}`}
    >
      {displayText}
    </Link>
  )

  if (needsTooltip) {
    return <Tooltip content={entityId}>{linkEl}</Tooltip>
  }

  return linkEl
})
