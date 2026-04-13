import * as React from 'react'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'

interface CompareField {
  key: string
  label: string
  render?: (value: unknown) => React.ReactNode
}

interface CompareViewProps<T extends { id: string; name?: string }> {
  entities: T[]
  fields: CompareField[]
  title?: string
  className?: string
}

function cellValue(entity: Record<string, unknown>, key: string): unknown {
  return key.split('.').reduce((acc, k) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[k] : undefined), entity as unknown)
}

export function CompareView<T extends { id: string; name?: string }>({
  entities,
  fields,
  title,
  className,
}: CompareViewProps<T>) {
  if (entities.length < 2) return null

  const allSame = (values: unknown[]) => values.every((v) => String(v) === String(values[0]))

  return (
    <div className={cn('overflow-x-auto', className)}>
      {title && <h3 className="text-sm font-semibold text-text-primary mb-3">{title}</h3>}
      <Table className="w-full text-xs">
        <TableHeader sticky={false}>
          <TableRow>
            <TableHead className="text-text-tertiary uppercase tracking-wider text-[10px] font-medium w-32">
              Field
            </TableHead>
            {entities.map((e) => (
              <TableHead key={e.id} className="text-text-primary font-semibold bg-bg-elevated border border-border">
                {e.name ?? e.id}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {fields.map((field) => {
            const values = entities.map((e) => cellValue(e as unknown as Record<string, unknown>, field.key))
            const same = allSame(values)
            return (
              <TableRow key={field.key} className={cn(!same && 'bg-warning/5')}>
                <TableCell className="text-text-secondary font-medium">{field.label}</TableCell>
                {values.map((val, idx) => (
                  <TableCell key={idx} className={cn('border-x border-border-subtle font-mono', !same && 'text-warning')}>
                    {field.render ? field.render(val) : String(val ?? '—')}
                  </TableCell>
                ))}
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
