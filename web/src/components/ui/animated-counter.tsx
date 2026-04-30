import { useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

interface AnimatedCounterProps {
  value: number
  formatter?: (n: number) => string
  className?: string
  duration?: number
}

function AnimatedCounter({ value, formatter, className, duration = 600 }: AnimatedCounterProps) {
  const [display, setDisplay] = useState(value)
  const prevRef = useRef(value)
  const rafRef = useRef<number>(0)

  useEffect(() => {
    const from = prevRef.current
    const to = value
    if (from === to) return

    const start = performance.now()
    const animate = (now: number) => {
      const elapsed = now - start
      const progress = Math.min(elapsed / duration, 1)
      const eased = 1 - Math.pow(1 - progress, 3)
      const current = from + (to - from) * eased
      setDisplay(current)

      if (progress < 1) {
        rafRef.current = requestAnimationFrame(animate)
      } else {
        prevRef.current = to
      }
    }
    rafRef.current = requestAnimationFrame(animate)

    return () => cancelAnimationFrame(rafRef.current)
  }, [value, duration])

  // Preserve float precision for custom formatters (percentages, decimals,
  // currency). Only the default path rounds — integer KPIs should pass
  // formatNumber explicitly rather than relying on rounding here.
  const formatted = formatter ? formatter(display) : Math.round(display).toLocaleString()

  return <span className={cn('tabular-nums', className)}>{formatted}</span>
}

export { AnimatedCounter }
