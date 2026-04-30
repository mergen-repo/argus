import { Download, ChevronDown, Loader2 } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { Button } from '@/components/ui/button'
import { useAlertExport, type AlertExportFormat } from '@/hooks/use-alert-export'

interface ExportMenuProps {
  filters: Record<string, string>
}

const EXPORT_OPTIONS: { format: AlertExportFormat; label: string }[] = [
  { format: 'csv', label: 'CSV' },
  { format: 'json', label: 'JSON' },
  { format: 'pdf', label: 'PDF' },
]

export function ExportMenu({ filters }: ExportMenuProps) {
  const { exportAs, exporting } = useAlertExport()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          aria-label="Export alerts"
          disabled={exporting}
          className="gap-1.5"
        >
          {exporting
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : <Download className="h-3.5 w-3.5" />}
          Export
          <ChevronDown className="h-3 w-3 text-text-tertiary" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="bg-bg-elevated border-border rounded-[var(--radius-sm)] w-32"
      >
        {EXPORT_OPTIONS.map(({ format, label }) => (
          <DropdownMenuItem
            key={format}
            disabled={exporting}
            onClick={() => exportAs(format, filters)}
            className="text-sm text-text-primary hover:bg-bg-hover"
          >
            {label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
