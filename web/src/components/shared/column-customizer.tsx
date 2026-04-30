import * as React from 'react'
import { Settings2, ChevronUp, ChevronDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { SlidePanel } from '@/components/ui/slide-panel'
import { cn } from '@/lib/utils'

interface ColumnDef {
  key: string
  label: string
  required?: boolean
}

interface ColumnCustomizerProps {
  columns: ColumnDef[]
  visible: string[]
  onChange: (cols: string[]) => void
  className?: string
}

export const ColumnCustomizer = React.memo(function ColumnCustomizer({
  columns,
  visible,
  onChange,
  className,
}: ColumnCustomizerProps) {
  const [open, setOpen] = React.useState(false)
  const [order, setOrder] = React.useState<string[]>(() =>
    [
      ...visible.filter((k) => columns.some((c) => c.key === k)),
      ...columns.filter((c) => !visible.includes(c.key)).map((c) => c.key),
    ],
  )
  const [checked, setChecked] = React.useState<Set<string>>(new Set(visible))

  React.useEffect(() => {
    setOrder([
      ...visible.filter((k) => columns.some((c) => c.key === k)),
      ...columns.filter((c) => !visible.includes(c.key)).map((c) => c.key),
    ])
    setChecked(new Set(visible))
  }, [visible, columns])

  const toggle = (key: string) => {
    const col = columns.find((c) => c.key === key)
    if (col?.required) return
    setChecked((prev) => {
      const next = new Set(prev)
      next.has(key) ? next.delete(key) : next.add(key)
      return next
    })
  }

  const move = (key: string, dir: -1 | 1) => {
    setOrder((prev) => {
      const idx = prev.indexOf(key)
      if (idx < 0) return prev
      const next = [...prev]
      const swap = idx + dir
      if (swap < 0 || swap >= next.length) return prev
      ;[next[idx], next[swap]] = [next[swap], next[idx]]
      return next
    })
  }

  const apply = () => {
    const result = order.filter((k) => checked.has(k))
    onChange(result)
    setOpen(false)
  }

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className={cn('gap-1.5 h-8', className)}
        onClick={() => setOpen(true)}
      >
        <Settings2 className="h-3.5 w-3.5" />
        Columns
      </Button>

      <SlidePanel open={open} onOpenChange={setOpen} title="Customize Columns">
        <div className="flex flex-col gap-1 py-2">
          {order.map((key, idx) => {
            const col = columns.find((c) => c.key === key)
            if (!col) return null
            return (
              <div key={key} className="flex items-center gap-2 px-2 py-1.5 rounded-[var(--radius-sm)] hover:bg-bg-hover">
                <Checkbox
                  id={`col-${key}`}
                  checked={checked.has(key)}
                  onChange={() => toggle(key)}
                  disabled={col.required}
                />
                <label htmlFor={`col-${key}`} className="flex-1 text-sm cursor-pointer select-none">
                  {col.label}
                  {col.required && <span className="ml-1 text-[10px] text-text-tertiary">(required)</span>}
                </label>
                <span className="flex gap-0.5">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-5 w-5 p-0.5 text-text-tertiary hover:text-text-primary disabled:opacity-30"
                    onClick={() => move(key, -1)}
                    disabled={idx === 0}
                    aria-label="Move up"
                  >
                    <ChevronUp className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-5 w-5 p-0.5 text-text-tertiary hover:text-text-primary disabled:opacity-30"
                    onClick={() => move(key, 1)}
                    disabled={idx === order.length - 1}
                    aria-label="Move down"
                  >
                    <ChevronDown className="h-3.5 w-3.5" />
                  </Button>
                </span>
              </div>
            )
          })}
        </div>
        <div className="flex justify-end gap-2 pt-4 border-t border-border">
          <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
          <Button size="sm" onClick={apply}>Apply</Button>
        </div>
      </SlidePanel>
    </>
  )
})
