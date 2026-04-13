import { useState, useMemo } from 'react'
import { toast } from 'sonner'
import {
  Shield,
  Lock,
  FileText,
  ShieldCheck,
  Activity,
  TrendingDown,
  Cpu,
  Globe,
  FileBarChart,
  Plus,
  Calendar,
  Clock,
  Download,
  Mail,
  CheckCircle2,
  Loader2,
  CalendarClock,
  Play,
  Pause,
  Trash2,
} from 'lucide-react'
import { useScheduledReports, useGenerateReport, useDeleteScheduledReport, useUpdateScheduledReport, type ScheduledReport as ApiScheduledReport } from '@/hooks/use-reports'
import type { LucideIcon } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { cn } from '@/lib/utils'

interface ReportDefinition {
  id: string
  category: 'COMPLIANCE' | 'OPERATIONS' | 'INVENTORY'
  name: string
  description: string
  icon: string
  format: string
  lastGenerated: string | null
}

const ICON_MAP: Record<string, LucideIcon> = {
  Shield,
  Lock,
  FileText,
  ShieldCheck,
  Activity,
  TrendingDown,
  Cpu,
  Globe,
}

const REPORT_DEFINITIONS: ReportDefinition[] = [
  { id: 'compliance_btk', category: 'COMPLIANCE', name: 'BTK Monthly Report', description: 'Regulatory compliance report for BTK submission', icon: 'Shield', format: 'PDF', lastGenerated: null },
  { id: 'compliance_kvkk', category: 'COMPLIANCE', name: 'KVKK Data Inventory', description: 'Personal data inventory per KVKK requirements', icon: 'Lock', format: 'PDF', lastGenerated: null },
  { id: 'compliance_gdpr', category: 'COMPLIANCE', name: 'GDPR Data Processing', description: 'Data processing activities report', icon: 'FileText', format: 'PDF', lastGenerated: null },
  { id: 'sla_monthly', category: 'OPERATIONS', name: 'SLA Compliance Report', description: 'Per-operator SLA compliance and breach analysis', icon: 'ShieldCheck', format: 'PDF', lastGenerated: null },
  { id: 'usage_summary', category: 'OPERATIONS', name: 'Usage Summary', description: 'Aggregate usage by tenant, operator, and APN', icon: 'Activity', format: 'CSV', lastGenerated: null },
  { id: 'cost_analysis', category: 'OPERATIONS', name: 'Cost Analysis', description: 'Cost breakdown with optimization recommendations', icon: 'TrendingDown', format: 'PDF', lastGenerated: null },
  { id: 'sim_inventory', category: 'INVENTORY', name: 'SIM Inventory', description: 'Complete SIM inventory grouped by lifecycle state', icon: 'Cpu', format: 'CSV', lastGenerated: null },
  { id: 'audit_log_export', category: 'INVENTORY', name: 'Audit Log Export', description: 'Audit log export for compliance review', icon: 'FileText', format: 'CSV', lastGenerated: null },
]

const CATEGORY_META: Record<string, { label: string; color: string; border: string }> = {
  COMPLIANCE: { label: 'Compliance', color: 'text-accent', border: 'border-accent/20' },
  OPERATIONS: { label: 'Operations', color: 'text-success', border: 'border-success/20' },
  INVENTORY: { label: 'Inventory', color: 'text-warning', border: 'border-warning/20' },
}

const FORMAT_OPTIONS = [
  { value: 'pdf', label: 'PDF' },
  { value: 'csv', label: 'CSV' },
  { value: 'xlsx', label: 'Excel (.xlsx)' },
]

function formatDate(iso: string | null): string {
  if (!iso) return 'Never'
  const d = new Date(iso)
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

function formatBadgeVariant(format: string): 'default' | 'secondary' | 'warning' {
  switch (format.toUpperCase()) {
    case 'PDF': return 'default'
    case 'CSV': return 'secondary'
    default: return 'warning'
  }
}

function ReportCard({
  report,
  onGenerate,
}: {
  report: ReportDefinition
  onGenerate: (report: ReportDefinition) => void
}) {
  const IconComp = ICON_MAP[report.icon] || FileBarChart

  const handleGenerate = () => {
    onGenerate(report)
  }

  return (
    <Card className="card-hover p-5 flex flex-col gap-3 h-full">
      <div className="flex items-start gap-3">
        <div className="h-9 w-9 rounded-lg bg-bg-hover border border-border flex items-center justify-center flex-shrink-0">
          <IconComp className="h-4 w-4 text-accent" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold text-text-primary truncate">{report.name}</h3>
          <p className="text-xs text-text-secondary mt-0.5 line-clamp-2">{report.description}</p>
        </div>
      </div>

      <div className="flex items-center gap-2 text-[11px] text-text-tertiary">
        <Calendar className="h-3 w-3" />
        <span>
          Last generated: <span className="text-text-secondary font-medium">{formatDate(report.lastGenerated)}</span>
        </span>
      </div>

      <div className="flex items-center gap-2 text-[11px] text-text-tertiary">
        <FileText className="h-3 w-3" />
        <span>Format:</span>
        <Badge variant={formatBadgeVariant(report.format)} className="text-[10px] px-1.5 py-0">
          {report.format}
        </Badge>
      </div>

      <div className="flex items-center gap-2 mt-auto pt-3 border-t border-border">
        <Button
          size="sm"
          variant="outline"
          className="flex-1 gap-1.5 text-xs"
          onClick={handleGenerate}
        >
          <Download className="h-3 w-3" />
          Generate
        </Button>
        <Button size="sm" variant="ghost" className="text-xs gap-1.5">
          <CalendarClock className="h-3 w-3" />
          Schedule
        </Button>
      </div>
    </Card>
  )
}

function ScheduledReportsTable({
  reports,
  onToggleState,
  onDelete,
}: {
  reports: ApiScheduledReport[]
  onToggleState: (r: ApiScheduledReport) => void
  onDelete: (id: string) => void
}) {
  if (reports.length === 0) {
    return (
      <div className="border border-border rounded-[var(--radius-md)] p-8 text-center text-text-tertiary text-sm">
        No scheduled reports yet. Create one from the Generate Report panel.
      </div>
    )
  }
  return (
    <div className="border border-border rounded-[var(--radius-md)] overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow className="border-b border-border bg-bg-elevated/50 hover:bg-bg-elevated/50">
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Report Type</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Schedule</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Recipients</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Next Run</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Status</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider w-[120px]">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {reports.map((report, i) => (
            <TableRow
              key={report.id}
              className={cn(
                'border-b border-border last:border-b-0 hover:bg-bg-hover/50 transition-colors',
                'animate-in fade-in',
              )}
              style={{ animationDelay: `${i * 40}ms` }}
            >
              <TableCell>
                <span className="text-sm font-medium text-text-primary">{report.report_type}</span>
                <Badge variant="outline" className="ml-2 text-[10px]">{report.format.toUpperCase()}</Badge>
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-1.5">
                  <Clock className="h-3 w-3 text-text-tertiary" />
                  <span className="text-xs text-text-secondary font-mono">{report.schedule_cron}</span>
                </div>
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-1.5">
                  <Mail className="h-3 w-3 text-text-tertiary" />
                  <span className="text-xs text-text-secondary font-mono truncate max-w-[180px]">{report.recipients.join(', ') || '—'}</span>
                </div>
              </TableCell>
              <TableCell>
                <span className="text-xs text-text-secondary font-mono">{formatDate(report.next_run_at)}</span>
              </TableCell>
              <TableCell>
                {report.state === 'active' ? (
                  <Badge variant="success" className="text-[10px] gap-1">
                    <Play className="h-2.5 w-2.5" />
                    Active
                  </Badge>
                ) : (
                  <Badge variant="warning" className="text-[10px] gap-1">
                    <Pause className="h-2.5 w-2.5" />
                    Paused
                  </Badge>
                )}
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => onToggleState(report)} className="h-7 w-7 p-0">
                    {report.state === 'active' ? <Pause className="h-3 w-3" /> : <Play className="h-3 w-3" />}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => onDelete(report.id)} className="h-7 w-7 p-0 text-error hover:text-error">
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function GenerateReportPanel({
  open,
  onClose,
  preselectedReport,
}: {
  open: boolean
  onClose: () => void
  preselectedReport: ReportDefinition | null
}) {
  const generateMutation = useGenerateReport()
  const [form, setForm] = useState({
    reportType: preselectedReport?.id || '',
    dateFrom: '',
    dateTo: '',
    format: 'pdf',
    recipients: '',
  })
  const [generating, setGenerating] = useState(false)
  const [generated, setGenerated] = useState(false)

  const handleGenerate = async () => {
    if (!form.reportType) return
    setGenerating(true)
    try {
      const filters: Record<string, unknown> = {}
      if (form.dateFrom) filters.date_from = form.dateFrom
      if (form.dateTo) filters.date_to = form.dateTo
      const res = await generateMutation.mutateAsync({
        report_type: form.reportType,
        format: form.format,
        filters,
      })
      toast.success(`Report queued (job ${res.job_id.slice(0, 8)}). Check Jobs page for status.`)
      setGenerated(true)
      setTimeout(() => {
        setGenerated(false)
        onClose()
      }, 1500)
    } catch (err) {
      toast.error('Failed to queue report')
      setGenerated(false)
      console.error(err)
    } finally {
      setGenerating(false)
    }
  }

  const reportOptions = useMemo(
    () => REPORT_DEFINITIONS.map((r: ReportDefinition) => ({ value: r.id, label: r.name })),
    [],
  )

  return (
    <SlidePanel
      open={open}
      onOpenChange={(v) => { if (!v) onClose() }}
      title="Generate Report"
      description="Configure and generate a new report"
      width="md"
    >
      <div className="space-y-5">
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Report Type *</label>
          <Select
            value={form.reportType}
            onChange={(e) => setForm((f) => ({ ...f, reportType: e.target.value }))}
            options={reportOptions}
            placeholder="Select a report..."
            className="h-8 text-sm"
          />
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">Date From</label>
            <Input
              type="date"
              value={form.dateFrom}
              onChange={(e) => setForm((f) => ({ ...f, dateFrom: e.target.value }))}
              className="h-8 text-sm"
            />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">Date To</label>
            <Input
              type="date"
              value={form.dateTo}
              onChange={(e) => setForm((f) => ({ ...f, dateTo: e.target.value }))}
              className="h-8 text-sm"
            />
          </div>
        </div>

        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Format</label>
          <Select
            value={form.format}
            onChange={(e) => setForm((f) => ({ ...f, format: e.target.value }))}
            options={FORMAT_OPTIONS}
            className="h-8 text-sm"
          />
        </div>

        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Recipients</label>
          <Input
            type="email"
            placeholder="email@example.com"
            value={form.recipients}
            onChange={(e) => setForm((f) => ({ ...f, recipients: e.target.value }))}
            className="h-8 text-sm"
          />
          <p className="text-[11px] text-text-tertiary mt-1">Report will be emailed to this address</p>
        </div>

        {generated && (
          <div className="rounded-lg border border-success/30 bg-success-dim p-4 flex items-center gap-3">
            <CheckCircle2 className="h-5 w-5 text-success flex-shrink-0" />
            <div>
              <p className="text-sm font-medium text-success">Report generated successfully</p>
              <p className="text-xs text-text-secondary mt-0.5">Download will start automatically</p>
            </div>
          </div>
        )}
      </div>

      <SlidePanelFooter className="mt-6">
        <Button variant="outline" size="sm" onClick={onClose} disabled={generating}>
          Cancel
        </Button>
        <Button size="sm" onClick={handleGenerate} disabled={generating || !form.reportType} className="gap-1.5">
          {generating ? (
            <>
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Generating...
            </>
          ) : (
            <>
              <Download className="h-3.5 w-3.5" />
              Generate Report
            </>
          )}
        </Button>
      </SlidePanelFooter>
    </SlidePanel>
  )
}

export default function ReportsPage() {
  const [panelOpen, setPanelOpen] = useState(false)
  const [selectedReport, setSelectedReport] = useState<ReportDefinition | null>(null)
  const scheduledQuery = useScheduledReports()
  const updateMutation = useUpdateScheduledReport()
  const deleteMutation = useDeleteScheduledReport()
  const scheduledReports: ApiScheduledReport[] = scheduledQuery.data?.data ?? []

  const handleToggleState = async (report: ApiScheduledReport) => {
    const newState = report.state === 'active' ? 'paused' : 'active'
    try {
      await updateMutation.mutateAsync({ id: report.id, patch: { state: newState } })
      toast.success(`Report ${newState}`)
    } catch {
      toast.error('Failed to update report')
    }
  }

  const handleDelete = async (id: string) => {
    if (!window.confirm('Delete this scheduled report?')) return
    try {
      await deleteMutation.mutateAsync(id)
      toast.success('Scheduled report deleted')
    } catch {
      toast.error('Failed to delete report')
    }
  }

  const grouped = useMemo(() => {
    const map: Record<string, ReportDefinition[]> = {}
    for (const r of REPORT_DEFINITIONS) {
      if (!map[r.category]) map[r.category] = []
      map[r.category].push(r)
    }
    return map
  }, [])

  const handleGenerate = (report: ReportDefinition) => {
    setSelectedReport(report)
    setPanelOpen(true)
  }

  const handleOpenPanel = () => {
    setSelectedReport(null)
    setPanelOpen(true)
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <Breadcrumb
          items={[
            { label: 'Dashboard', href: '/' },
            { label: 'Reports' },
          ]}
        />
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-[16px] font-semibold text-text-primary">Reports Center</h1>
            <p className="text-xs text-text-tertiary mt-0.5">
              Generate, schedule, and manage compliance and operational reports
            </p>
          </div>
          <Button size="sm" className="gap-1.5" onClick={handleOpenPanel}>
            <Plus className="h-3.5 w-3.5" />
            Generate Report
          </Button>
        </div>
      </div>

      {(['COMPLIANCE', 'OPERATIONS', 'INVENTORY'] as const).map((category) => {
        const meta = CATEGORY_META[category]
        const reports = grouped[category] || []

        return (
          <div key={category} className="stagger-item">
            <div className="flex items-center gap-2 mb-3">
              <div className={cn('h-1.5 w-1.5 rounded-full', meta.color.replace('text-', 'bg-'))} />
              <h2 className="text-xs font-semibold text-text-tertiary uppercase tracking-wider">
                {meta.label}
              </h2>
              <Badge variant="outline" className="text-[10px]">{reports.length}</Badge>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
              {reports.map((report, i) => (
                <div
                  key={report.id}
                  className="animate-in fade-in slide-in-from-bottom-1"
                  style={{ animationDelay: `${i * 50}ms` }}
                >
                  <ReportCard report={report} onGenerate={handleGenerate} />
                </div>
              ))}
            </div>
          </div>
        )
      })}

      <div className="stagger-item">
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center gap-2">
            <CalendarClock className="h-4 w-4 text-text-tertiary" />
            <h2 className="text-xs font-semibold text-text-tertiary uppercase tracking-wider">
              Scheduled Reports
            </h2>
            <Badge variant="outline" className="text-[10px]">{scheduledReports.length}</Badge>
          </div>
        </div>
        <ScheduledReportsTable
          reports={scheduledReports}
          onToggleState={handleToggleState}
          onDelete={handleDelete}
        />
      </div>

      <GenerateReportPanel
        open={panelOpen}
        onClose={() => setPanelOpen(false)}
        preselectedReport={selectedReport}
      />
    </div>
  )
}
