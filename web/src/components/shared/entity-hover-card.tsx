import * as React from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Popover, PopoverContent } from '@/components/ui/popover'
import { Badge } from '@/components/ui/badge'
import type { EntityType } from './entity-link'
import type { Operator } from '@/types/operator'
import type { SIM } from '@/types/sim'
import type { APN } from '@/types/apn'
import type { TenantUser } from '@/types/settings'
import type { ApiResponse } from '@/types/sim'

const HOVER_DELAY_MS = 200
const STALE_TIME_MS = 5 * 60 * 1000

type SupportedPreviewType = 'operator' | 'sim' | 'apn' | 'user'

function isSupportedPreviewType(type: string): type is SupportedPreviewType {
  return ['operator', 'sim', 'apn', 'user'].includes(type)
}

function healthVariant(status: string): 'success' | 'warning' | 'danger' | 'secondary' {
  switch (status) {
    case 'healthy': return 'success'
    case 'degraded': return 'warning'
    case 'down': return 'danger'
    default: return 'secondary'
  }
}

interface SummaryRowProps {
  label: string
  value: React.ReactNode
}

function SummaryRow({ label, value }: SummaryRowProps) {
  return (
    <div className="flex items-center justify-between gap-3 min-w-0">
      <span className="text-text-secondary text-[11px] shrink-0">{label}</span>
      <span className="text-text-primary text-[13px] font-mono truncate">{value}</span>
    </div>
  )
}

interface OperatorSummaryProps {
  data: Operator
}

function OperatorSummary({ data }: OperatorSummaryProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <SummaryRow label="Code" value={data.code} />
      <SummaryRow label="MCC/MNC" value={`${data.mcc}/${data.mnc}`} />
      <div className="flex items-center justify-between gap-3">
        <span className="text-text-secondary text-[11px]">Health</span>
        <Badge variant={healthVariant(data.health_status)} className="text-[10px]">
          {data.health_status.toUpperCase()}
        </Badge>
      </div>
    </div>
  )
}

interface SimSummaryProps {
  data: SIM
}

function SimSummary({ data }: SimSummaryProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <SummaryRow label="ICCID" value={data.iccid} />
      <SummaryRow label="State" value={data.state} />
      {data.apn_name && <SummaryRow label="APN" value={data.apn_name} />}
    </div>
  )
}

interface ApnSummaryProps {
  data: APN
}

function ApnSummary({ data }: ApnSummaryProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <SummaryRow label="Code" value={data.name} />
      <SummaryRow label="Type" value={data.apn_type} />
      {data.sim_count != null && <SummaryRow label="Subscribers" value={String(data.sim_count)} />}
    </div>
  )
}

interface UserSummaryProps {
  data: TenantUser
}

function UserSummary({ data }: UserSummaryProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <SummaryRow label="Email" value={data.email} />
      <SummaryRow label="Role" value={data.role} />
    </div>
  )
}

async function fetchEntitySummary(
  entityType: SupportedPreviewType,
  entityId: string,
): Promise<Operator | SIM | APN | TenantUser> {
  switch (entityType) {
    case 'operator': {
      const res = await api.get<ApiResponse<Operator>>(`/operators/${entityId}`)
      return res.data.data
    }
    case 'sim': {
      const res = await api.get<ApiResponse<SIM>>(`/sims/${entityId}`)
      return res.data.data
    }
    case 'apn': {
      const res = await api.get<ApiResponse<APN>>(`/apns/${entityId}`)
      return res.data.data
    }
    case 'user': {
      const res = await api.get<ApiResponse<TenantUser>>(`/users/${entityId}`)
      return res.data.data
    }
  }
}

function renderSummary(
  entityType: SupportedPreviewType,
  data: Operator | SIM | APN | TenantUser,
): React.ReactNode {
  switch (entityType) {
    case 'operator':
      return <OperatorSummary data={data as Operator} />
    case 'sim':
      return <SimSummary data={data as SIM} />
    case 'apn':
      return <ApnSummary data={data as APN} />
    case 'user':
      return <UserSummary data={data as TenantUser} />
  }
}

export interface EntityHoverCardProps {
  entityType: EntityType | string
  entityId: string
  children: React.ReactNode
}

export const EntityHoverCard = React.memo(function EntityHoverCard({
  entityType,
  entityId,
  children,
}: EntityHoverCardProps) {
  const [isOpen, setIsOpen] = React.useState(false)
  const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null)

  const supported = isSupportedPreviewType(entityType)
  const isOnline =
    typeof navigator !== 'undefined' ? navigator.onLine : true

  const { data, isLoading, isError } = useQuery({
    queryKey: ['entity-summary', entityType, entityId],
    queryFn: () => fetchEntitySummary(entityType as SupportedPreviewType, entityId),
    enabled: isOpen && supported && isOnline && !!entityId,
    staleTime: STALE_TIME_MS,
  })

  React.useEffect(() => {
    return () => {
      if (timerRef.current !== null) {
        clearTimeout(timerRef.current)
      }
    }
  }, [])

  const handleMouseEnter = React.useCallback(() => {
    if (!supported || !isOnline || !entityId) return
    timerRef.current = setTimeout(() => {
      setIsOpen(true)
    }, HOVER_DELAY_MS)
  }, [supported, isOnline, entityId])

  const handleMouseLeave = React.useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
    setIsOpen(false)
  }, [])

  return (
    <Popover open={isOpen} onOpenChange={setIsOpen}>
      <div
        className="inline-block"
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
      >
        {children}
        {isOpen && (
          <PopoverContent
            className="p-3 min-w-[200px] max-w-[280px] bg-bg-elevated border border-border rounded-md shadow-lg"
            sideOffset={6}
          >
            {isLoading && (
              <span className="text-text-secondary text-[12px]">Loading...</span>
            )}
            {isError && (
              <span className="text-text-secondary text-[12px]">Entity not found</span>
            )}
            {!isLoading && !isError && data && supported && renderSummary(entityType as SupportedPreviewType, data)}
            {!supported && (
              <span className="text-text-secondary text-[12px]">No preview available</span>
            )}
          </PopoverContent>
        )}
      </div>
    </Popover>
  )
})
