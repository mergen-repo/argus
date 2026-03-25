import { useState, useRef, useEffect, useCallback } from 'react'
import { Search, Loader2, X } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { SIM } from '@/types/sim'

interface SimSearchProps {
  value: string
  onChange: (simId: string, sim?: SIM) => void
  placeholder?: string
  className?: string
}

function SimSearch({ value, onChange, placeholder = 'Search by ICCID, IMSI, MSISDN or IP...', className }: SimSearchProps) {
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [selectedSim, setSelectedSim] = useState<SIM | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null)
  const [debouncedQuery, setDebouncedQuery] = useState('')

  const { data: results, isLoading } = useQuery({
    queryKey: ['sim-search', debouncedQuery],
    queryFn: async () => {
      const res = await api.get<{ data: SIM[] }>(`/sims?q=${encodeURIComponent(debouncedQuery)}&limit=8`)
      return res.data.data ?? []
    },
    enabled: debouncedQuery.length >= 2,
    staleTime: 10_000,
  })

  const handleInputChange = useCallback((val: string) => {
    setQuery(val)
    setSelectedSim(null)
    onChange('', undefined)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      setDebouncedQuery(val.trim())
    }, 300)
  }, [onChange])

  const handleSelect = useCallback((sim: SIM) => {
    setSelectedSim(sim)
    setQuery(sim.iccid)
    onChange(sim.id, sim)
    setOpen(false)
  }, [onChange])

  const handleClear = useCallback(() => {
    setQuery('')
    setDebouncedQuery('')
    setSelectedSim(null)
    onChange('', undefined)
    inputRef.current?.focus()
  }, [onChange])

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  const showDropdown = open && debouncedQuery.length >= 2

  return (
    <div ref={containerRef} className={cn('relative', className)}>
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary pointer-events-none" />
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => handleInputChange(e.target.value)}
          onFocus={() => setOpen(true)}
          placeholder={placeholder}
          className={cn(
            'w-full h-9 rounded-[var(--radius-sm)] border border-border bg-bg-primary pl-9 pr-8 text-sm text-text-primary placeholder:text-text-tertiary focus:outline-none focus:border-accent transition-colors',
            selectedSim && 'border-success/50 bg-success-dim',
          )}
        />
        {isLoading && (
          <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-tertiary animate-spin" />
        )}
        {!isLoading && query && (
          <button
            onClick={handleClear}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-text-tertiary hover:text-text-primary"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {selectedSim && (
        <div className="mt-2 flex items-center gap-3 rounded-[var(--radius-sm)] border border-success/30 bg-success-dim px-3 py-2 text-xs">
          <span className="font-mono text-text-primary">{selectedSim.iccid}</span>
          <span className="text-text-tertiary">|</span>
          <span className="font-mono text-text-secondary">{selectedSim.imsi}</span>
          {selectedSim.msisdn && (
            <>
              <span className="text-text-tertiary">|</span>
              <span className="font-mono text-text-secondary">{selectedSim.msisdn}</span>
            </>
          )}
          <span className={cn(
            'ml-auto text-[10px] font-medium uppercase px-1.5 py-0.5 rounded',
            selectedSim.state === 'active' ? 'bg-success/20 text-success' : 'bg-warning/20 text-warning',
          )}>
            {selectedSim.state}
          </span>
        </div>
      )}

      {showDropdown && (
        <div className="absolute z-50 left-0 right-0 top-full mt-1 max-h-64 overflow-y-auto rounded-[var(--radius-md)] border border-border bg-bg-elevated shadow-lg animate-fade-in">
          {isLoading ? (
            <div className="flex items-center justify-center py-6">
              <Loader2 className="h-4 w-4 animate-spin text-text-tertiary" />
            </div>
          ) : results && results.length > 0 ? (
            <div className="py-1">
              {results.map((sim) => (
                <button
                  key={sim.id}
                  onClick={() => handleSelect(sim)}
                  className="w-full text-left px-3 py-2 hover:bg-bg-hover transition-colors flex items-center gap-3"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs text-accent">{sim.iccid}</span>
                      <span className={cn(
                        'text-[9px] font-medium uppercase px-1 py-0.5 rounded',
                        sim.state === 'active' ? 'bg-success/20 text-success' : 'bg-warning/20 text-warning',
                      )}>
                        {sim.state}
                      </span>
                    </div>
                    <div className="flex items-center gap-3 mt-0.5">
                      <span className="font-mono text-[11px] text-text-tertiary">IMSI: {sim.imsi}</span>
                      {sim.msisdn && <span className="font-mono text-[11px] text-text-tertiary">MSISDN: {sim.msisdn}</span>}
                      {sim.ip_address && <span className="font-mono text-[11px] text-text-tertiary">IP: {sim.ip_address}</span>}
                    </div>
                  </div>
                </button>
              ))}
            </div>
          ) : (
            <div className="py-6 text-center text-xs text-text-tertiary">
              No SIMs found for "{debouncedQuery}"
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export { SimSearch }
