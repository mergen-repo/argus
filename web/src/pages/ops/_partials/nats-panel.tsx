import { Radio, AlertTriangle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { NATSBlock } from '@/types/ops'

interface NATSPanelProps {
  data: NATSBlock | undefined
}

export function NATSPanel({ data }: NATSPanelProps) {
  if (!data) {
    return (
      <div className="p-6 text-center text-[13px] text-text-tertiary">
        <Radio className="h-8 w-8 mx-auto mb-2" />
        Loading NATS data...
      </div>
    )
  }

  if (data.error) {
    return (
      <div className="p-6 text-center">
        <AlertTriangle className="h-8 w-8 text-warning mx-auto mb-2" />
        <p className="text-[13px] text-text-secondary">{data.error}</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-[11px] text-text-tertiary">DLQ Depth:</span>
        <Badge className={data.dlq_depth > 0 ? 'bg-danger-dim text-danger border-0' : 'bg-success-dim text-success border-0'}>
          {data.dlq_depth}
        </Badge>
      </div>

      {(!data.streams || data.streams.length === 0) ? (
        <p className="text-[13px] text-text-tertiary text-center py-4">No streams discovered</p>
      ) : (
        data.streams.map((stream) => (
          <div key={stream.name} className="rounded-[10px] border border-border overflow-hidden">
            <div className="flex items-center justify-between px-4 py-3 bg-bg-elevated border-b border-border">
              <div className="flex items-center gap-2">
                <Radio className="h-4 w-4 text-accent" />
                <span className="text-[13px] font-mono text-text-primary">{stream.name}</span>
              </div>
              <div className="flex items-center gap-4 text-[11px] text-text-tertiary">
                <span>msgs: <span className="text-text-secondary font-mono">{stream.messages?.toLocaleString()}</span></span>
                <span>consumers: <span className="text-text-secondary">{stream.consumers}</span></span>
                <Badge className="bg-success-dim text-success border-0 text-[10px]">HEALTHY</Badge>
              </div>
            </div>

            {stream.consumer_lag && stream.consumer_lag.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow className="border-border hover:bg-transparent">
                    <TableHead className="text-[11px] text-text-tertiary pl-4">Consumer</TableHead>
                    <TableHead className="text-[11px] text-text-tertiary text-right">Pending</TableHead>
                    <TableHead className="text-[11px] text-text-tertiary text-right">Ack Pending</TableHead>
                    <TableHead className="text-[11px] text-text-tertiary text-right pr-4">Status</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {stream.consumer_lag.map((c) => (
                    <TableRow
                      key={c.consumer}
                      className={`border-border ${c.slow ? 'bg-warning-dim' : 'hover:bg-bg-hover'}`}
                    >
                      <TableCell className="pl-4 text-[12px] font-mono text-text-primary">{c.consumer}</TableCell>
                      <TableCell className="text-right text-[12px] font-mono text-text-secondary">{c.pending?.toLocaleString()}</TableCell>
                      <TableCell className="text-right text-[12px] font-mono text-text-secondary">{c.ack_pending}</TableCell>
                      <TableCell className="text-right pr-4">
                        {c.slow ? (
                          <Badge className="bg-warning-dim text-warning border-0 text-[10px]">SLOW</Badge>
                        ) : (
                          <Badge className="bg-success-dim text-success border-0 text-[10px]">OK</Badge>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
        ))
      )}
    </div>
  )
}
