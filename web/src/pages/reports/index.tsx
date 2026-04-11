import { useState, useMemo } from 'react'
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
} from 'lucide-react'
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

interface ScheduledReport {
  id: string
  reportName: string
  schedule: string
  recipient: string
  nextRun: string
  status: 'active' | 'paused'
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
  { id: 'btk-monthly', category: 'COMPLIANCE', name: 'BTK Monthly Report', description: 'Regulatory compliance report for BTK submission', icon: 'Shield', format: 'PDF', lastGenerated: '2024-03-01T09:00:00Z' },
  { id: 'kvkk-inventory', category: 'COMPLIANCE', name: 'KVKK Data Inventory', description: 'Personal data inventory per KVKK requirements', icon: 'Lock', format: 'PDF', lastGenerated: '2024-02-15T09:00:00Z' },
  { id: 'gdpr-processing', category: 'COMPLIANCE', name: 'GDPR Data Processing', description: 'Data processing activities report', icon: 'FileText', format: 'PDF', lastGenerated: '2024-02-15T09:00:00Z' },
  { id: 'sla-compliance', category: 'OPERATIONS', name: 'SLA Compliance Report', description: 'Per-operator SLA compliance and breach analysis', icon: 'ShieldCheck', format: 'PDF', lastGenerated: '2024-03-01T09:00:00Z' },
  { id: 'operator-perf', category: 'OPERATIONS', name: 'Operator Performance', description: 'Operator latency, auth rates, and health trends', icon: 'Activity', format: 'CSV', lastGenerated: '2024-03-15T09:00:00Z' },
  { id: 'cost-optimization', category: 'OPERATIONS', name: 'Cost Optimization', description: 'Cost breakdown with optimization recommendations', icon: 'TrendingDown', format: 'PDF', lastGenerated: '2024-03-20T09:00:00Z' },
  { id: 'sim-inventory', category: 'INVENTORY', name: 'SIM Inventory by State', description: 'Complete SIM inventory grouped by lifecycle state', icon: 'Cpu', format: 'CSV', lastGenerated: null },
  { id: 'ip-utilization', category: 'INVENTORY', name: 'IP Pool Utilization', description: 'IP address allocation and utilization report', icon: 'Globe', format: 'CSV', lastGenerated: '2024-03-22T09:00:00Z' },
  { id: 'policy-coverage', category: 'INVENTORY', name: 'Policy Coverage', description: 'Policy assignment coverage across SIM inventory', icon: 'Shield', format: 'PDF', lastGenerated: '2024-03-18T09:00:00Z' },
]

const SCHEDULED_REPORTS: ScheduledReport[] = [
  { id: '1', reportName: 'BTK Monthly Report', schedule: 'Monthly (1st)', recipient: 'compliance@argus.io', nextRun: '2024-04-01T09:00:00Z', status: 'active' },
  { id: '2', reportName: 'SLA Compliance Report', schedule: 'Weekly (Monday)', recipient: 'ops-team@argus.io', nextRun: '2024-03-25T09:00:00Z', status: 'active' },
  { id: '3', reportName: 'Operator Performance', schedule: 'Daily', recipient: 'noc@argus.io', nextRun: '2024-03-25T06:00:00Z', status: 'active' },
  { id: '4', reportName: 'KVKK Data Inventory', schedule: 'Monthly (15th)', recipient: 'legal@argus.io', nextRun: '2024-04-15T09:00:00Z', status: 'paused' },
  { id: '5', reportName: 'Cost Optimization', schedule: 'Weekly (Friday)', recipient: 'finance@argus.io', nextRun: '2024-03-29T09:00:00Z', status: 'active' },
]

const CATEGORY_META: Record<string, { label: string; color: string; border: string }> = {
  COMPLIANCE: { label: 'Compliance', color: 'text-accent', border: 'border-accent/20' },
  OPERATIONS: { label: 'Operations', color: 'text-success', border: 'border-success/20' },
  INVENTORY: { label: 'Inventory', color: 'text-warning', border: 'border-warning/20' },
}

const FORMAT_OPTIONS = [
  { value: 'pdf', label: 'PDF' },
  { value: 'csv', label: 'CSV' },
  { value: 'xlsx', label: 'Excel (XLSX)' },
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
  const [generating, setGenerating] = useState(false)
  const IconComp = ICON_MAP[report.icon] || FileBarChart

  const handleGenerate = () => {
    setGenerating(true)
    setTimeout(() => setGenerating(false), 2000)
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
          disabled={generating}
        >
          {generating ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Download className="h-3 w-3" />
          )}
          {generating ? 'Generating...' : 'Generate'}
        </Button>
        <Button size="sm" variant="ghost" className="text-xs gap-1.5">
          <CalendarClock className="h-3 w-3" />
          Schedule
        </Button>
      </div>
    </Card>
  )
}

function ScheduledReportsTable({ reports }: { reports: ScheduledReport[] }) {
  return (
    <div className="border border-border rounded-[var(--radius-md)] overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow className="border-b border-border bg-bg-elevated/50 hover:bg-bg-elevated/50">
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Report Name</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Schedule</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Recipient</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Next Run</TableHead>
            <TableHead className="text-[11px] font-semibold text-text-tertiary uppercase tracking-wider">Status</TableHead>
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
                <span className="text-sm font-medium text-text-primary">{report.reportName}</span>
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-1.5">
                  <Clock className="h-3 w-3 text-text-tertiary" />
                  <span className="text-xs text-text-secondary font-mono">{report.schedule}</span>
                </div>
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-1.5">
                  <Mail className="h-3 w-3 text-text-tertiary" />
                  <span className="text-xs text-text-secondary font-mono">{report.recipient}</span>
                </div>
              </TableCell>
              <TableCell>
                <span className="text-xs text-text-secondary font-mono">{formatDate(report.nextRun)}</span>
              </TableCell>
              <TableCell>
                {report.status === 'active' ? (
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
  const [form, setForm] = useState({
    reportType: preselectedReport?.id || '',
    dateFrom: '',
    dateTo: '',
    format: 'pdf',
    recipients: '',
  })
  const [generating, setGenerating] = useState(false)
  const [generated, setGenerated] = useState(false)

  const handleGenerate = () => {
    setGenerating(true)
    setTimeout(() => {
      setGenerating(false)
      setGenerated(true)
      setTimeout(() => {
        setGenerated(false)
        onClose()
      }, 1500)
    }, 2000)
  }

  const reportOptions = useMemo(
    () => REPORT_DEFINITIONS.map((r) => ({ value: r.id, label: r.name })),
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
            <Badge variant="outline" className="text-[10px]">{SCHEDULED_REPORTS.length}</Badge>
          </div>
        </div>
        <ScheduledReportsTable reports={SCHEDULED_REPORTS} />
      </div>

      <GenerateReportPanel
        open={panelOpen}
        onClose={() => setPanelOpen(false)}
        preselectedReport={selectedReport}
      />
    </div>
  )
}
