import { Database, AlertTriangle } from 'lucide-react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { DBBlock } from '@/types/ops'

interface DBPanelProps {
  data: DBBlock | undefined
}

function formatBytes(bytes: number) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`
}

export function DBPanel({ data }: DBPanelProps) {
  if (!data) {
    return (
      <div className="p-6 text-center text-[13px] text-text-tertiary">
        <Database className="h-8 w-8 mx-auto mb-2" />
        Loading database data...
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

  const pool = data.pool ?? { max: 0, in_use: 0, idle: 0, waiting: 0 }
  const poolUsePct = pool.max > 0 ? (pool.in_use / pool.max) * 100 : 0

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {[
          { label: 'Pool Max', value: pool.max },
          { label: 'In Use', value: pool.in_use },
          { label: 'Idle', value: pool.idle },
          { label: 'Waiting', value: pool.waiting },
        ].map(({ label, value }) => (
          <div key={label} className="rounded-[10px] border border-border bg-bg-elevated p-4">
            <div className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary mb-1">{label}</div>
            <div className="text-[22px] font-mono font-bold text-text-primary">{value}</div>
          </div>
        ))}
      </div>

      <div className="rounded-[10px] border border-border overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 bg-bg-elevated border-b border-border">
          <span className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary">Connection Pool</span>
          <span className="text-[12px] font-mono text-text-secondary">{poolUsePct.toFixed(0)}% used</span>
        </div>
        <div className="h-2 bg-bg-surface">
          <div
            className="h-full bg-accent transition-all"
            style={{ width: `${Math.min(poolUsePct, 100)}%` }}
          />
        </div>
      </div>

      {data.replication_lag_seconds != null && (
        <div className="rounded-[10px] border border-border p-4 bg-bg-elevated">
          <span className="text-[11px] text-text-tertiary">Replication Lag: </span>
          <span className="text-[13px] font-mono text-text-primary">{data.replication_lag_seconds}s</span>
        </div>
      )}

      {data.tables && data.tables.length > 0 && (
        <div>
          <div className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary mb-2">Table Sizes (Top 10)</div>
          <Table>
            <TableHeader>
              <TableRow className="border-border hover:bg-transparent">
                <TableHead className="text-[11px] text-text-tertiary">Table</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right">Size</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right pr-4">Rows (est)</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.tables.map((t) => (
                <TableRow key={t.name} className="border-border hover:bg-bg-hover">
                  <TableCell className="text-[12px] font-mono text-text-primary">{t.name}</TableCell>
                  <TableCell className="text-right text-[12px] font-mono text-text-secondary">{formatBytes(t.size_bytes)}</TableCell>
                  <TableCell className="text-right pr-4 text-[12px] font-mono text-text-secondary">{t.row_estimate.toLocaleString()}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {data.partitions && data.partitions.length > 0 && (
        <div>
          <div className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary mb-2">Partitions</div>
          <Table>
            <TableHeader>
              <TableRow className="border-border hover:bg-transparent">
                <TableHead className="text-[11px] text-text-tertiary">Parent</TableHead>
                <TableHead className="text-[11px] text-text-tertiary pr-4">Child</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.partitions.map((p, i) => (
                <TableRow key={`${p.parent}-${p.child}-${i}`} className="border-border hover:bg-bg-hover">
                  <TableCell className="text-[12px] font-mono text-text-secondary">{p.parent}</TableCell>
                  <TableCell className="text-[12px] font-mono text-text-primary pr-4">{p.child}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
