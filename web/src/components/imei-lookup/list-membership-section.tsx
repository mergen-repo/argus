import * as React from 'react'
import { Link } from 'react-router-dom'
import { Check, Minus, ListChecks } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { IMEILookupListEntry, IMEIPoolKind } from '@/types/imei-lookup'

const POOL_ORDER: IMEIPoolKind[] = ['whitelist', 'greylist', 'blacklist']

const POOL_LABEL: Record<IMEIPoolKind, string> = {
  whitelist: 'Whitelist',
  greylist: 'Greylist',
  blacklist: 'Blacklist',
}

function poolBadgeVariant(kind: IMEIPoolKind): 'success' | 'warning' | 'danger' {
  switch (kind) {
    case 'whitelist':
      return 'success'
    case 'greylist':
      return 'warning'
    case 'blacklist':
      return 'danger'
  }
}

function matchedViaTone(via: string): { variant: 'success' | 'warning'; label: string } {
  if (via === 'exact') return { variant: 'success', label: 'Exact match' }
  if (via === 'tac_range') return { variant: 'warning', label: 'TAC range' }
  return { variant: 'warning', label: via }
}

interface ListMembershipSectionProps {
  lists: IMEILookupListEntry[]
}

export const ListMembershipSection = React.memo(function ListMembershipSection({
  lists,
}: ListMembershipSectionProps) {
  const byKind = React.useMemo(() => {
    const map = new Map<IMEIPoolKind, IMEILookupListEntry>()
    for (const entry of lists) {
      if (!map.has(entry.kind)) map.set(entry.kind, entry)
    }
    return map
  }, [lists])

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center gap-2">
          <ListChecks className="h-3.5 w-3.5 text-text-tertiary" />
          <CardTitle className="text-sm">Pool Membership</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-2">
        {POOL_ORDER.map((kind) => {
          const entry = byKind.get(kind)
          const matched = !!entry
          const tone = entry ? matchedViaTone(entry.matched_via) : null
          return (
            <div
              key={kind}
              className={cn(
                'flex items-center justify-between rounded-[var(--radius-sm)] border px-3 py-2 transition-colors',
                matched
                  ? 'border-border bg-bg-elevated'
                  : 'border-border-subtle bg-transparent opacity-70',
              )}
            >
              <div className="flex items-center gap-3">
                <span
                  className={cn(
                    'inline-flex h-5 w-5 items-center justify-center rounded-full',
                    matched ? 'bg-success-dim text-success' : 'bg-bg-hover text-text-tertiary',
                  )}
                  aria-hidden="true"
                >
                  {matched ? (
                    <Check className="h-3 w-3" />
                  ) : (
                    <Minus className="h-3 w-3" />
                  )}
                </span>
                <Badge variant={poolBadgeVariant(kind)} className="uppercase tracking-wider text-[10px]">
                  {POOL_LABEL[kind]}
                </Badge>
                {!matched && (
                  <span className="text-xs text-text-tertiary">Not present</span>
                )}
              </div>
              {matched && tone ? (
                <div className="flex items-center gap-2">
                  <Badge variant={tone.variant} className="text-[10px]">
                    {tone.label}
                  </Badge>
                  <Link
                    to={`/settings/imei-pools#${kind}`}
                    className="font-mono text-[11px] text-accent hover:text-accent/80 hover:underline transition-colors"
                    aria-label={`View ${POOL_LABEL[kind]} entry`}
                  >
                    View entry
                  </Link>
                </div>
              ) : null}
            </div>
          )
        })}
      </CardContent>
    </Card>
  )
})
