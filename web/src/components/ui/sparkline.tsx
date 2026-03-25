import { cn } from '@/lib/utils'

interface SparklineProps {
  data: number[]
  color: string
  height?: number
  width?: number
  filled?: boolean
  className?: string
}

function Sparkline({ data, color, height = 24, width = 80, filled = true, className }: SparklineProps) {
  if (data.length < 2) return null

  const max = Math.max(...data)
  const min = Math.min(...data)
  const range = max - min || 1
  const padding = 1

  const points = data.map((v, i) => {
    const x = (i / (data.length - 1)) * (width - padding * 2) + padding
    const y = height - ((v - min) / range) * (height - padding * 2) - padding
    return `${x},${y}`
  })

  const linePath = `M${points.join(' L')}`
  const fillPath = filled
    ? `${linePath} L${width - padding},${height} L${padding},${height} Z`
    : undefined

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={cn('overflow-visible', className)}
    >
      {filled && fillPath && (
        <path
          d={fillPath}
          fill={color}
          opacity={0.1}
        />
      )}
      <path
        d={linePath}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle
        cx={parseFloat(points[points.length - 1].split(',')[0])}
        cy={parseFloat(points[points.length - 1].split(',')[1])}
        r={2}
        fill={color}
      />
    </svg>
  )
}

export { Sparkline }
