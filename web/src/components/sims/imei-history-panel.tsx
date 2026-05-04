import { useMemo, useState } from 'react'
import { AlertTriangle, BellRing, ChevronDown, ListChecks, RefreshCw } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Spinner } from '@/components/ui/spinner'
import { EmptyState } from '@/components/shared/empty-state'
import { timeAgo } from '@/lib/format'
import { useIMEIHistory } from '@/hooks/use-device-binding'
import {
  CAPTURE_PROTOCOLS,
  CAPTURE_PROTOCOL_LABEL,
  type CaptureProtocol,
  type IMEIHistoryFilters,
} from '@/types/device-binding'

interface IMEIHistoryPanelProps {
  simId: string
}

// HTML date input emits YYYY-MM-DD; convert to RFC3339 (start-of-day UTC)
// before pushing to the backend, which requires RFC3339.
function dateToISOOrEmpty(d: string): string {
  if (!d) return ''
  const t = new Date(`${d}T00:00:00Z`).getTime()
  if (!Number.isFinite(t)) return ''
  return new Date(t).toISOString()
}

function ProtocolBadge({ protocol }: { protocol: CaptureProtocol }) {
  const variant: 'default' | 'success' | 'warning' =
    protocol === 'radius' ? 'default' : protocol === 'diameter_s6a' ? 'success' : 'warning'
  return (
    <Badge variant={variant} className="font-mono uppercase tracking-wider text-xs">
      {CAPTURE_PROTOCOL_LABEL[protocol]}
    </Badge>
  )
}

export function IMEIHistoryPanel({ simId }: IMEIHistoryPanelProps) {
  const [protocolFilter, setProtocolFilter] = useState<CaptureProtocol | ''>('')
  const [sinceDate, setSinceDate] = useState('') // YYYY-MM-DD from <Input type="date" />

  const filters = useMemo<IMEIHistoryFilters>(
    () => ({
      protocol: protocolFilter || undefined,
      since: dateToISOOrEmpty(sinceDate) || undefined,
    }),
    [protocolFilter, sinceDate],
  )

  const list = useIMEIHistory(simId, filters)

  const allRows = useMemo(() => {
    if (!list.data) return []
    return list.data.pages.flatMap((p) => p.data)
  }, [list.data])

  const totalLoaded = allRows.length

  if (list.isError) {
    return (
      <Card>
        <CardContent
          role="alert"
          aria-live="polite"
          className="flex flex-col items-center justify-center gap-3 py-16 text-center"
        >
          <AlertTriangle className="h-8 w-8 text-danger" />
          <div>
            <p className="text-sm font-semibold text-text-primary">Failed to load IMEI history</p>
            <p className="mt-1 text-xs text-text-secondary">
              The device-binding service did not respond. Try again in a moment.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => list.refetch()} className="gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />
            Retry
          </Button>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="space-y-4">
      <Card className="relative overflow-hidden before:content-[''] before:absolute before:left-0 before:top-0 before:bottom-0 before:w-[3px] before:bg-purple">
        <CardContent className="p-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="flex items-center gap-3">
              <span className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
                <ListChecks className="h-4 w-4 text-text-secondary" />
              </span>
              <div>
                <h2 className="text-sm font-semibold text-text-primary">IMEI History</h2>
                <p className="text-xs text-text-tertiary font-mono">
                  {list.isLoading
                    ? 'Loading…'
                    : `${totalLoaded.toLocaleString()} observation${totalLoaded === 1 ? '' : 's'} loaded`}
                </p>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={protocolFilter}
                onChange={(e) => setProtocolFilter(e.target.value as CaptureProtocol | '')}
                options={[
                  { value: '', label: 'All protocols' },
                  ...CAPTURE_PROTOCOLS.map((p) => ({
                    value: p,
                    label: CAPTURE_PROTOCOL_LABEL[p],
                  })),
                ]}
                className="h-8 w-[150px] text-xs"
                aria-label="Filter by capture protocol"
              />
              <Input
                type="date"
                value={sinceDate}
                onChange={(e) => setSinceDate(e.target.value)}
                className="h-8 w-[150px] text-xs"
                aria-label="Show observations since date"
              />
              {(protocolFilter || sinceDate) && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setProtocolFilter('')
                    setSinceDate('')
                  }}
                >
                  Clear
                </Button>
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          {list.isLoading ? (
            <div className="p-4 space-y-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : allRows.length === 0 ? (
            <EmptyState
              icon={ListChecks}
              title="No IMEI observations yet"
              description="When this SIM authenticates over RADIUS, Diameter, or 5G SBA, the observed IMEI will be recorded here."
            />
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="border-b border-border-subtle hover:bg-transparent">
                    <TableHead className="text-xs uppercase tracking-[0.5px] text-text-secondary font-medium py-2">
                      Observed
                    </TableHead>
                    <TableHead className="text-xs uppercase tracking-[0.5px] text-text-secondary font-medium py-2">
                      IMEI
                    </TableHead>
                    <TableHead className="text-xs uppercase tracking-[0.5px] text-text-secondary font-medium py-2">
                      Protocol
                    </TableHead>
                    <TableHead className="text-xs uppercase tracking-[0.5px] text-text-secondary font-medium py-2">
                      NAS IP
                    </TableHead>
                    <TableHead className="text-xs uppercase tracking-[0.5px] text-text-secondary font-medium py-2">
                      Flags
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {allRows.map((row) => (
                    <TableRow
                      key={row.id}
                      className="border-b border-border-subtle hover:bg-bg-hover transition-colors"
                    >
                      <TableCell className="py-2">
                        <span
                          className="text-xs text-text-secondary"
                          title={new Date(row.observed_at).toLocaleString()}
                        >
                          {timeAgo(row.observed_at)}
                        </span>
                      </TableCell>
                      <TableCell>
                        <span className="font-mono text-xs text-text-primary tracking-wider">
                          {row.observed_imei}
                        </span>
                      </TableCell>
                      <TableCell>
                        <ProtocolBadge protocol={row.capture_protocol} />
                      </TableCell>
                      <TableCell>
                        <span className="font-mono text-xs text-text-tertiary">
                          {row.nas_ip_address ?? '—'}
                        </span>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-1.5">
                          {row.was_mismatch && (
                            <Badge
                              variant="danger"
                              className="gap-1 uppercase tracking-wider text-xs"
                              title="Observed IMEI did not match the bound IMEI"
                            >
                              <AlertTriangle className="h-3 w-3" />
                              Mismatch
                            </Badge>
                          )}
                          {row.alarm_raised && (
                            <Badge
                              variant="warning"
                              className="gap-1 uppercase tracking-wider text-xs"
                              title="A binding alarm was raised for this observation"
                            >
                              <BellRing className="h-3 w-3" />
                              Alarm
                            </Badge>
                          )}
                          {!row.was_mismatch && !row.alarm_raised && (
                            <span className="text-text-tertiary text-xs">—</span>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {list.hasNextPage && (
                <div className="flex items-center justify-center border-t border-border-subtle p-3">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => list.fetchNextPage()}
                    disabled={list.isFetchingNextPage}
                    className="gap-1.5"
                  >
                    {list.isFetchingNextPage && <Spinner className="h-3 w-3" />}
                    {list.isFetchingNextPage ? 'Loading…' : 'Load more'}
                    {!list.isFetchingNextPage && <ChevronDown className="h-3.5 w-3.5" />}
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
