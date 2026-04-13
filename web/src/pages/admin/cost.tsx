import { RefreshCw, AlertCircle, DollarSign } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { useCostByTenant } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'

function SparkLine({ data }: { data: number[] }) {
  if (!data?.length) return <span className="text-text-tertiary text-xs">—</span>
  const max = Math.max(...data, 1)
  const w = 60
  const h = 20
  const points = data
    .map((v, i) => `${(i / (data.length - 1)) * w},${h - (v / max) * h}`)
    .join(' ')
  return (
    <svg width={w} height={h} className="overflow-visible">
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        className="text-accent-primary"
      />
    </svg>
  )
}

function fmtCurrency(val: number, currency = 'USD') {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency, maximumFractionDigits: 0 }).format(val)
}

export default function CostByTenantPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'

  const { data: costs, isLoading, isError, refetch } = useCostByTenant()

  if (!isSuperAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">super_admin role required.</p>
        </div>
      </div>
    )
  }

  const totalCost = (costs ?? []).reduce((sum, c) => sum + c.total, 0)

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Cost by Tenant</h1>
          <p className="text-sm text-text-secondary mt-0.5">6-month cost breakdown per tenant</p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      {!isLoading && !isError && (
        <Card className="bg-bg-surface border-border">
          <CardContent className="pt-4">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-accent-primary/10">
                <DollarSign className="h-5 w-5 text-accent-primary" />
              </div>
              <div>
                <div className="text-2xl font-bold text-text-primary">
                  {fmtCurrency(totalCost)}
                </div>
                <div className="text-xs text-text-secondary">Total across all tenants</div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load cost data.
        </div>
      )}

      <Card className="bg-bg-surface border-border">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tenant</TableHead>
                <TableHead className="text-right">Total</TableHead>
                <TableHead className="text-right">RADIUS</TableHead>
                <TableHead className="text-right">Operator</TableHead>
                <TableHead className="text-right">SMS</TableHead>
                <TableHead className="text-right">Storage</TableHead>
                <TableHead>6-mo Trend</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(costs ?? []).map((c) => (
                <TableRow key={c.tenant_id}>
                  <TableCell>
                    <div className="font-medium text-text-primary">{c.tenant_name}</div>
                  </TableCell>
                  <TableCell className="text-right font-semibold">{fmtCurrency(c.total, c.currency)}</TableCell>
                  <TableCell className="text-right text-text-secondary">{fmtCurrency(c.radius_cost, c.currency)}</TableCell>
                  <TableCell className="text-right text-text-secondary">{fmtCurrency(c.operator_cost, c.currency)}</TableCell>
                  <TableCell className="text-right text-text-secondary">{fmtCurrency(c.sms_cost, c.currency)}</TableCell>
                  <TableCell className="text-right text-text-secondary">{fmtCurrency(c.storage_cost, c.currency)}</TableCell>
                  <TableCell><SparkLine data={c.trend} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  )
}
