import React from 'react'
import { ArrowUpRight, ArrowDownRight, Minus } from 'lucide-react'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { AnimatedCounter } from '@/components/ui/animated-counter'
import { Sparkline } from '@/components/ui/sparkline'
import { cn } from '@/lib/utils'

export interface KPICardProps {
  title: string
  value: number
  label?: string
  formatter?: (n: number) => string
  sparklineData: number[]
  color: string
  delta?: number
  deltaFormat?: 'percent' | 'absolute'
  live?: boolean
  suffix?: string
  subtitle?: string
  onClick?: () => void
  delay: number
}

export const KPICard = React.memo(function KPICard({
  title,
  value,
  label,
  formatter,
  sparklineData,
  color,
  delta,
  deltaFormat = 'percent',
  live,
  suffix,
  subtitle,
  onClick,
  delay,
}: KPICardProps) {
  const deltaColor = delta === undefined || delta === 0
    ? 'text-text-tertiary'
    : delta > 0
      ? 'text-success'
      : 'text-danger'

  const deltaIcon = delta === undefined || delta === 0
    ? <Minus className="h-3 w-3" />
    : delta > 0
      ? <ArrowUpRight className="h-3 w-3" />
      : <ArrowDownRight className="h-3 w-3" />

  const deltaText = delta === undefined
    ? null
    : deltaFormat === 'percent'
      ? `${delta > 0 ? '+' : ''}${delta.toFixed(1)}%`
      : `${delta > 0 ? '+' : ''}${delta.toFixed(1)}`

  return (
    <Card
      className="card-hover cursor-pointer relative overflow-hidden stagger-item group"
      style={{ animationDelay: `${delay}ms` }}
      onClick={onClick}
    >
      <div className="absolute bottom-0 left-0 right-0 h-[2px] transition-all" style={{ backgroundColor: color }} />
      <CardHeader className="flex flex-row items-center justify-between pb-1 pt-3 px-4">
        <span className="text-[10px] uppercase tracking-[1.5px] text-text-secondary font-medium">
          {title}
        </span>
        <div className="flex items-center gap-2">
          {live && (
            <span className="flex items-center gap-1">
              <span
                className="h-1.5 w-1.5 rounded-full pulse-dot"
                style={{ backgroundColor: color, boxShadow: `0 0 6px ${color}60` }}
              />
              <span className="text-[9px] font-semibold tracking-[1px]" style={{ color }}>LIVE</span>
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent className="pt-0 pb-3 px-4">
        <div className="flex items-end justify-between gap-2 mb-2">
          <div className="flex items-baseline gap-1">
            {label !== undefined ? (
              <span className="font-mono text-[28px] font-bold text-text-primary leading-none truncate max-w-[140px]" title={label}>
                {label}
              </span>
            ) : (
              <AnimatedCounter
                value={value}
                formatter={formatter}
                className="font-mono text-[28px] font-bold text-text-primary leading-none"
              />
            )}
            {suffix && (
              <span className="text-[12px] text-text-tertiary font-mono">{suffix}</span>
            )}
          </div>
          {deltaText && (
            <span className={cn('flex items-center gap-0.5 text-[11px] font-mono font-medium', deltaColor)}>
              {deltaIcon}
              {deltaText}
            </span>
          )}
        </div>
        <Sparkline data={sparklineData} color={color} height={24} width={200} className="w-full" />
        {subtitle && (
          <p className="mt-1.5 text-[10px] font-mono text-text-tertiary truncate">{subtitle}</p>
        )}
      </CardContent>
    </Card>
  )
})
