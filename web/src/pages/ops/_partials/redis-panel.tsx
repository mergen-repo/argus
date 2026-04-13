import { HardDrive, AlertTriangle } from 'lucide-react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { RedisBlock } from '@/types/ops'

interface RedisPanelProps {
  data: RedisBlock | undefined
}

function formatBytes(bytes: number) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`
}

export function RedisPanel({ data }: RedisPanelProps) {
  if (!data) {
    return (
      <div className="p-6 text-center text-[13px] text-text-tertiary">
        <HardDrive className="h-8 w-8 mx-auto mb-2" />
        Loading Redis data...
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

  const memPct = data.memory_max_bytes > 0 ? (data.memory_used_bytes / data.memory_max_bytes) * 100 : 0

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
        {[
          { label: 'Ops/sec', value: data.ops_per_sec.toLocaleString() },
          { label: 'Hit Rate', value: `${(data.hit_rate * 100).toFixed(1)}%` },
          { label: 'Miss Rate', value: `${(data.miss_rate * 100).toFixed(1)}%` },
          { label: 'Evictions', value: data.evictions_total.toLocaleString() },
          { label: 'Clients', value: data.connected_clients.toLocaleString() },
          { label: 'p99 Latency', value: `${data.latency_p99_ms.toFixed(2)}ms` },
        ].map(({ label, value }) => (
          <div key={label} className="rounded-[10px] border border-border bg-bg-elevated p-4">
            <div className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary mb-1">{label}</div>
            <div className="text-[22px] font-mono font-bold text-text-primary">{value}</div>
          </div>
        ))}
      </div>

      <div className="rounded-[10px] border border-border overflow-hidden">
        <div className="flex items-center justify-between px-4 py-2 bg-bg-elevated border-b border-border">
          <span className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary">Memory Usage</span>
          <span className="text-[12px] font-mono text-text-secondary">
            {formatBytes(data.memory_used_bytes)} / {formatBytes(data.memory_max_bytes)}
          </span>
        </div>
        <div className="h-2 bg-bg-surface">
          <div
            className={`h-full transition-all ${memPct > 80 ? 'bg-danger' : memPct > 60 ? 'bg-warning' : 'bg-accent'}`}
            style={{ width: `${Math.min(memPct, 100)}%` }}
          />
        </div>
        <div className="px-4 py-1">
          <span className="text-[11px] text-text-tertiary">{memPct.toFixed(1)}% used</span>
        </div>
      </div>

      {data.keys_by_db && data.keys_by_db.length > 0 && (
        <div>
          <div className="text-[10px] uppercase tracking-[1.5px] text-text-tertiary mb-2">Keys by Database</div>
          <Table>
            <TableHeader>
              <TableRow className="border-border hover:bg-transparent">
                <TableHead className="text-[11px] text-text-tertiary">DB</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right">Keys</TableHead>
                <TableHead className="text-[11px] text-text-tertiary text-right pr-4">Expires</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.keys_by_db.map((db) => (
                <TableRow key={db.db} className="border-border hover:bg-bg-hover">
                  <TableCell className="text-[12px] font-mono text-text-secondary">db{db.db}</TableCell>
                  <TableCell className="text-right text-[12px] font-mono text-text-primary">{db.keys.toLocaleString()}</TableCell>
                  <TableCell className="text-right pr-4 text-[12px] font-mono text-text-secondary">{db.expires.toLocaleString()}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
