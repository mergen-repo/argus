import { useState, useCallback } from 'react'
import {
  Search,
  Download,
  LayoutList,
  LayoutGrid,
  AlignJustify,
  X,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useUIStore } from '@/stores/ui'

type Density = 'compact' | 'comfortable' | 'spacious'

interface FilterOption {
  label: string
  value: string
}

interface ActiveFilter {
  key: string
  label: string
  value: string
  displayValue: string
}

interface TableToolbarProps {
  search?: string
  onSearchChange?: (value: string) => void
  searchPlaceholder?: string
  activeFilters?: ActiveFilter[]
  onRemoveFilter?: (key: string) => void
  onClearFilters?: () => void
  onExport?: (format: 'csv' | 'excel' | 'pdf') => void
  showDensity?: boolean
  showExport?: boolean
  className?: string
  children?: React.ReactNode
}

const densityIcons: Record<Density, React.ElementType> = {
  compact: AlignJustify,
  comfortable: LayoutList,
  spacious: LayoutGrid,
}

const densityLabels: Record<Density, string> = {
  compact: 'Compact',
  comfortable: 'Comfortable',
  spacious: 'Spacious',
}

function TableToolbar({
  search,
  onSearchChange,
  searchPlaceholder = 'Search...',
  activeFilters = [],
  onRemoveFilter,
  onClearFilters,
  onExport,
  showDensity = true,
  showExport = true,
  className,
  children,
}: TableToolbarProps) {
  const { tableDensity, setTableDensity } = useUIStore()
  const [exportOpen, setExportOpen] = useState(false)

  const cycleDensity = useCallback(() => {
    const order: Density[] = ['compact', 'comfortable', 'spacious']
    const idx = order.indexOf(tableDensity)
    setTableDensity(order[(idx + 1) % order.length])
  }, [tableDensity, setTableDensity])

  const DensityIcon = densityIcons[tableDensity]

  return (
    <div className={cn('space-y-2', className)}>
      <div className="flex items-center gap-2 flex-wrap">
        {onSearchChange && (
          <div className="relative flex-1 min-w-[200px] max-w-sm">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary" />
            <input
              type="text"
              value={search || ''}
              onChange={(e) => onSearchChange(e.target.value)}
              placeholder={searchPlaceholder}
              className="w-full h-8 rounded-[var(--radius-sm)] border border-border bg-bg-surface pl-8 pr-3 text-xs text-text-primary placeholder:text-text-tertiary focus:outline-none focus:border-accent transition-colors"
            />
            {search && (
              <button
                onClick={() => onSearchChange('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>
        )}

        {children}

        <div className="flex items-center gap-1.5 ml-auto">
          {showDensity && (
            <button
              onClick={cycleDensity}
              className="flex items-center gap-1.5 h-8 px-2.5 rounded-[var(--radius-sm)] border border-border text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors text-xs"
              title={`Density: ${densityLabels[tableDensity]}`}
            >
              <DensityIcon className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">{densityLabels[tableDensity]}</span>
            </button>
          )}

          {showExport && onExport && (
            <div className="relative">
              <button
                onClick={() => setExportOpen(!exportOpen)}
                className="flex items-center gap-1.5 h-8 px-2.5 rounded-[var(--radius-sm)] border border-border text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors text-xs"
              >
                <Download className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">Export</span>
              </button>
              {exportOpen && (
                <>
                  <div className="fixed inset-0 z-40" onClick={() => setExportOpen(false)} />
                  <div className="absolute right-0 top-full mt-1 z-50 w-36 rounded-[var(--radius-sm)] border border-border bg-bg-elevated shadow-lg py-1 animate-fade-in">
                    {(['csv', 'excel', 'pdf'] as const).map((fmt) => (
                      <button
                        key={fmt}
                        onClick={() => {
                          onExport(fmt)
                          setExportOpen(false)
                        }}
                        className="w-full text-left px-3 py-1.5 text-xs text-text-secondary hover:text-text-primary hover:bg-bg-hover transition-colors"
                      >
                        Export as {fmt.toUpperCase()}
                      </button>
                    ))}
                  </div>
                </>
              )}
            </div>
          )}
        </div>
      </div>

      {activeFilters.length > 0 && (
        <div className="flex items-center gap-1.5 flex-wrap">
          <span className="text-[10px] text-text-tertiary uppercase tracking-wider">Filters:</span>
          {activeFilters.map((f) => (
            <span
              key={f.key}
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-accent-dim text-accent text-[11px]"
            >
              <span>
                {f.label}: {f.displayValue}
              </span>
              {onRemoveFilter && (
                <button
                  onClick={() => onRemoveFilter(f.key)}
                  className="hover:text-text-primary"
                >
                  <X className="h-2.5 w-2.5" />
                </button>
              )}
            </span>
          ))}
          {onClearFilters && activeFilters.length > 1 && (
            <button
              onClick={onClearFilters}
              className="text-[11px] text-text-tertiary hover:text-accent transition-colors"
            >
              Clear all
            </button>
          )}
        </div>
      )}
    </div>
  )
}

export { TableToolbar, type ActiveFilter, type FilterOption }
