import * as React from 'react'
import { Link } from 'react-router-dom'
import { Smartphone, ChevronRight } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { EmptyState } from '@/components/shared/empty-state'
import type {
  IMEIBindingMode,
  IMEIBindingStatus,
  IMEILookupBoundSim,
} from '@/types/imei-lookup'

function bindingStatusVariant(
  status: IMEIBindingStatus,
): 'success' | 'warning' | 'danger' | 'secondary' {
  switch (status) {
    case 'verified':
      return 'success'
    case 'pending':
      return 'warning'
    case 'mismatch':
      return 'danger'
    case 'unbound':
      return 'secondary'
    default:
      return 'secondary'
  }
}

function bindingStatusLabel(status: IMEIBindingStatus): string {
  if (!status) return '—'
  return status.charAt(0).toUpperCase() + status.slice(1).replace(/_/g, ' ')
}

function bindingModeLabel(mode: IMEIBindingMode): string {
  if (!mode) return '—'
  return mode.charAt(0).toUpperCase() + mode.slice(1).replace(/_/g, ' ')
}

interface BoundSimsSectionProps {
  boundSims: IMEILookupBoundSim[]
}

export const BoundSimsSection = React.memo(function BoundSimsSection({
  boundSims,
}: BoundSimsSectionProps) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Smartphone className="h-3.5 w-3.5 text-text-tertiary" />
            <CardTitle className="text-sm">Bound SIMs</CardTitle>
          </div>
          <span className="text-[10px] uppercase tracking-wider text-text-tertiary">
            {boundSims.length} {boundSims.length === 1 ? 'SIM' : 'SIMs'}
          </span>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        {boundSims.length === 0 ? (
          <EmptyState
            icon={Smartphone}
            title="No bound SIMs"
            description="No SIMs in this tenant currently report this IMEI as their bound device."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ICCID</TableHead>
                <TableHead>Mode</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="w-10" aria-label="Open" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {boundSims.map((sim) => (
                <TableRow key={sim.sim_id} className="group">
                  <TableCell className="p-0">
                    <Link
                      to={`/sims/${sim.sim_id}#device-binding`}
                      className="block px-3 py-2 font-mono text-xs text-accent group-hover:underline focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent"
                      aria-label={`Open SIM ${sim.iccid} device binding tab`}
                    >
                      {sim.iccid}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline" className="text-[10px]">
                      {bindingModeLabel(sim.binding_mode)}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge variant={bindingStatusVariant(sim.binding_status)} className="text-[10px]">
                      {bindingStatusLabel(sim.binding_status)}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <ChevronRight className="ml-auto h-3.5 w-3.5 text-text-tertiary transition-transform group-hover:translate-x-0.5 group-hover:text-text-primary" />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
})
