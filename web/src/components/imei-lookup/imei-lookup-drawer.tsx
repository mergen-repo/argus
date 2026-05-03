import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { AlertCircle, Plus, RefreshCw, SearchX } from 'lucide-react'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/shared/empty-state'
import { ListMembershipSection } from './list-membership-section'
import { BoundSimsSection } from './bound-sims-section'
import { HistorySection } from './history-section'
import type { IMEILookupResult, IMEIPoolKind } from '@/types/imei-lookup'
import { tacFromIMEI } from '@/types/imei-lookup'

const POOL_LABEL: Record<IMEIPoolKind, string> = {
  whitelist: 'Whitelist',
  greylist: 'Greylist',
  blacklist: 'Blacklist',
}

interface IMEILookupDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  imei: string
  data: IMEILookupResult | undefined
  isLoading: boolean
  isError: boolean
  errorMessage?: string | null
  onRetry?: () => void
}

function LoadingSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-32 w-full" />
      <Skeleton className="h-40 w-full" />
    </div>
  )
}

function ErrorPanel({
  message,
  onRetry,
}: {
  message?: string | null
  onRetry?: () => void
}) {
  return (
    <div className="rounded-[var(--radius-md)] border border-danger/40 bg-danger-dim p-6 text-center">
      <AlertCircle className="h-8 w-8 text-danger mx-auto mb-3" />
      <p className="text-sm font-semibold text-text-primary">Lookup failed</p>
      <p className="mt-1 text-xs text-text-secondary">
        {message ?? 'Unable to fetch lookup result. Please try again.'}
      </p>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry} className="mt-4 gap-2">
          <RefreshCw className="h-3.5 w-3.5" />
          Retry
        </Button>
      )}
    </div>
  )
}

export function IMEILookupDrawer({
  open,
  onOpenChange,
  imei,
  data,
  isLoading,
  isError,
  errorMessage,
  onRetry,
}: IMEILookupDrawerProps) {
  const navigate = useNavigate()
  const tac = tacFromIMEI(imei)

  const handleAddToPool = (kind: IMEIPoolKind) => {
    const params = new URLSearchParams({ prefill_imei: imei, prefill_kind: kind })
    navigate(`/settings/imei-pools?${params.toString()}#${kind}`)
    onOpenChange(false)
  }

  const noMatches =
    !!data &&
    data.lists.length === 0 &&
    data.bound_sims.length === 0 &&
    data.history.length === 0

  const description = tac ? `TAC: ${tac}` : undefined

  return (
    <SlidePanel
      open={open}
      onOpenChange={onOpenChange}
      title={imei ? `IMEI ${imei}` : 'IMEI Lookup'}
      description={description}
      width="lg"
    >
      <div className="space-y-4 pb-4">
        {isLoading && <LoadingSkeleton />}

        {!isLoading && isError && <ErrorPanel message={errorMessage} onRetry={onRetry} />}

        {!isLoading && !isError && data && noMatches && (
          <EmptyState
            icon={SearchX}
            title="No matches in this tenant"
            description="The IMEI has not been observed and is not in any pool. You can still add it to a list."
          />
        )}

        {!isLoading && !isError && data && !noMatches && (
          <>
            <ListMembershipSection lists={data.lists} />
            <BoundSimsSection boundSims={data.bound_sims} />
            <HistorySection history={data.history} />
          </>
        )}

        {!isLoading && !isError && data && noMatches && (
          <>
            <ListMembershipSection lists={[]} />
            <BoundSimsSection boundSims={[]} />
            <HistorySection history={[]} />
          </>
        )}
      </div>

      <SlidePanelFooter>
        <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
          Close
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" disabled={!imei}>
              <Plus className="h-3.5 w-3.5" />
              Add to Pool
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Add this IMEI to</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {(Object.keys(POOL_LABEL) as IMEIPoolKind[]).map((kind) => (
              <DropdownMenuItem key={kind} onClick={() => handleAddToPool(kind)}>
                {POOL_LABEL[kind]}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </SlidePanelFooter>
    </SlidePanel>
  )
}
