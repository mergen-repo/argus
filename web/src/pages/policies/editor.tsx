import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Save,
  Play,
  Loader2,
  AlertCircle,
  RefreshCw,
  Shield,
  Keyboard,
  CheckCircle2,
  HelpCircle,
  Layers,
  FileWarning,
  Copy,
  Download,
  Wifi,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Tooltip } from '@/components/ui/tooltip'
import { DSLEditor } from '@/components/policy/dsl-editor'
import { DSLErrorSummary } from '@/components/policy/dsl-error-summary'
import type { Diagnostic } from '@codemirror/lint'
import { validateDSL } from '@/lib/api/policies'
import { ErrorBoundary } from '@/components/error-boundary'
import { PreviewTab } from '@/components/policy/preview-tab'
import { VersionsTab } from '@/components/policy/versions-tab'
import { RolloutTab } from '@/components/policy/rollout-tab'
import {
  usePolicy,
  useUpdateVersion,
  useActivateVersion,
  useCreateVersion,
  useDryRunMutation,
} from '@/hooks/use-policies'
import type { PolicyVersion, DryRunResult } from '@/types/policy'
import { RelatedAuditTab, RelatedViolationsTab, FavoriteToggle } from '@/components/shared'
import { useUIStore } from '@/stores/ui'
import { AssignedSimsTab } from './_tabs/assigned-sims-tab'

export default function PolicyEditorPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const { data: policy, isLoading, isError, refetch } = usePolicy(id!)

  const [activeTab, setActiveTab] = useState('dsl-help')
  const [dslContent, setDslContent] = useState('')
  const [selectedVersionId, setSelectedVersionId] = useState<string | undefined>()
  const [isDirty, setIsDirty] = useState(false)
  const [activateDialog, setActivateDialog] = useState(false)
  const [dryRunResult, setDryRunResult] = useState<DryRunResult | null>(null)
  const [dividerPosition, setDividerPosition] = useState(55)
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved'>('idle')
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([])

  const editorScrollRef = useRef<HTMLDivElement>(null)
  const dryRunTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const hasErrors = diagnostics.some((d) => d.severity === 'error')

  const handleJumpToDiagnostic = useCallback((pos: number) => {
    const container = editorScrollRef.current
    if (!container) return
    // CodeMirror line markers carry data-line; fall back to scrolling the editor
    // pane to the top of the relevant region by approximating with character pos.
    const cm = container.querySelector('.cm-scroller') as HTMLElement | null
    const cmContent = container.querySelector('.cm-content') as HTMLElement | null
    if (!cm || !cmContent) return
    // Approximate scroll: line height * (pos / avgCharsPerLine).
    const lineHeight = parseFloat(getComputedStyle(cmContent).lineHeight || '20') || 20
    const docText = cmContent.textContent || ''
    const charsBefore = docText.slice(0, pos)
    const lineIndex = (charsBefore.match(/\n/g) || []).length
    cm.scrollTo({ top: Math.max(0, lineIndex * lineHeight - cm.clientHeight / 3), behavior: 'smooth' })
  }, [])

  const addRecentItem = useUIStore((s) => s.addRecentItem)

  useEffect(() => {
    if (policy && id) {
      addRecentItem({ type: 'policy', id, label: `Policy: ${policy.name}`, path: `/policies/${id}` })
    }
  }, [policy, id, addRecentItem])

  const updateVersionMutation = useUpdateVersion()
  const activateVersionMutation = useActivateVersion(id!)
  const createVersionMutation = useCreateVersion(id!)
  const dryRunMutation = useDryRunMutation()

  const selectedVersion = policy?.versions?.find((v) => v.id === selectedVersionId)
  const isDraft = selectedVersion?.state === 'draft'

  useEffect(() => {
    if (policy?.versions && policy.versions.length > 0 && !selectedVersionId) {
      const draft = policy.versions.find((v) => v.state === 'draft')
      const active = policy.versions.find((v) => v.state === 'active')
      const target = draft || active || policy.versions[0]
      setSelectedVersionId(target.id)
      setDslContent(target.dsl_content || '')
    }
  }, [policy, selectedVersionId])

  const handleSelectVersion = useCallback((version: PolicyVersion) => {
    setSelectedVersionId(version.id)
    setDslContent(version.dsl_content || '')
    setIsDirty(false)
    setDryRunResult(null)
    setSaveStatus('idle')
  }, [])

  const handleDslChange = useCallback((value: string) => {
    setDslContent(value)
    setIsDirty(true)
    setSaveStatus('idle')

    if (dryRunTimerRef.current) {
      clearTimeout(dryRunTimerRef.current)
    }
  }, [])

  const handleSave = useCallback(async () => {
    if (!selectedVersionId || !isDirty) return
    setSaveStatus('saving')
    try {
      await updateVersionMutation.mutateAsync({
        versionId: selectedVersionId,
        dsl_source: dslContent,
      })
      setIsDirty(false)
      setSaveStatus('saved')
      setTimeout(() => setSaveStatus('idle'), 2000)
      refetch()

      if (dryRunTimerRef.current) clearTimeout(dryRunTimerRef.current)
      dryRunTimerRef.current = setTimeout(() => {
        handleDryRun()
      }, 500)
    } catch {
      setSaveStatus('idle')
    }
  }, [selectedVersionId, isDirty, dslContent, updateVersionMutation, refetch])

  const handleDryRun = useCallback(async () => {
    if (!selectedVersionId) return
    try {
      const result = await dryRunMutation.mutateAsync(selectedVersionId)
      setDryRunResult(result)
      setActiveTab('preview')
    } catch {
      setDryRunResult(null)
    }
  }, [selectedVersionId, dryRunMutation])

  // FIX-243 Wave D — Ctrl+Shift+F formats the buffer via the validate
  // endpoint with ?format=true. On non-error response we replace the
  // editor content with the canonicalised source. On error we silently
  // no-op (the linter already surfaces the underlying parse error).
  const handleFormat = useCallback(async () => {
    if (!isDraft) return
    if (!dslContent.trim()) return
    try {
      const result = await validateDSL(dslContent, { format: true })
      if (result.formatted_source && result.formatted_source !== dslContent) {
        setDslContent(result.formatted_source)
        setIsDirty(true)
        setSaveStatus('idle')
      }
    } catch {
      // intentionally swallow — linter has already flagged the error
    }
  }, [dslContent, isDraft])

  // FIX-243 Wave D — Ctrl+Enter is now "validate now". The DSLEditor
  // also calls forceLinting() internally; this callback is mostly a
  // no-op hook for analytics / future UX (e.g. flash the status badge).
  const handleValidateNow = useCallback(() => {
    // Linter is already triggered inside the editor. Future: surface a
    // brief "validated" toast or pulse the status indicator.
  }, [])

  const handleActivate = async () => {
    if (!selectedVersionId) return
    try {
      await activateVersionMutation.mutateAsync(selectedVersionId)
      setActivateDialog(false)
      refetch()
    } catch {
      // handled by api interceptor
    }
  }

  const handleCreateVersion = async () => {
    try {
      const version = await createVersionMutation.mutateAsync({
        dsl_source: dslContent || 'POLICY "new" {\n    MATCH {\n        apn = "default"\n    }\n\n    RULES {\n        bandwidth_down = 1mbps\n    }\n}\n',
        clone_from_version_id: selectedVersionId,
      })
      refetch()
      setSelectedVersionId(version.id)
      setDslContent(version.dsl_content || '')
      setIsDirty(false)
      setDryRunResult(null)
    } catch {
      // handled by api interceptor
    }
  }

  const handleExport = () => {
    if (!policy || !selectedVersion) return
    const dslBlob = new Blob([dslContent], { type: 'text/plain' })
    const jsonBlob = new Blob(
      [JSON.stringify({ policy, version: selectedVersion }, null, 2)],
      { type: 'application/json' }
    )
    const dslUrl = URL.createObjectURL(dslBlob)
    const jsonUrl = URL.createObjectURL(jsonBlob)
    const prefix = `${policy.name.replace(/\s+/g, '_')}_v${selectedVersion.version}`
    const a1 = document.createElement('a')
    a1.href = dslUrl
    a1.download = `${prefix}.dsl`
    a1.click()
    URL.revokeObjectURL(dslUrl)
    const a2 = document.createElement('a')
    a2.href = jsonUrl
    a2.download = `${prefix}.json`
    a2.click()
    URL.revokeObjectURL(jsonUrl)
  }

  // FIX-243 Wave D — global page-level shortcuts. Mirrors the in-editor
  // keymap so the bindings work even when the editor isn't focused.
  //   Ctrl+S         → save
  //   Ctrl+Enter     → (handled inside the editor — forceLinting)
  //   Ctrl+Shift+Enter → dry-run
  //   Ctrl+Shift+F   → format
  useEffect(() => {
    const handleKeydown = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (!mod) return
      if (e.key === 's') {
        e.preventDefault()
        handleSave()
        return
      }
      if (e.shiftKey && e.key === 'Enter') {
        e.preventDefault()
        handleDryRun()
        return
      }
      // Ctrl+Shift+F — match by lowercased key; some layouts emit 'F'.
      if (e.shiftKey && e.key.toLowerCase() === 'f') {
        e.preventDefault()
        handleFormat()
        return
      }
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [handleSave, handleDryRun, handleFormat])

  const handleDividerMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    const startX = e.clientX
    const startPos = dividerPosition

    const handleMouseMove = (e: MouseEvent) => {
      const container = document.getElementById('editor-container')
      if (!container) return
      const rect = container.getBoundingClientRect()
      const delta = e.clientX - startX
      const newPos = startPos + (delta / rect.width) * 100
      setDividerPosition(Math.max(25, Math.min(75, newPos)))
    }

    const handleMouseUp = () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
  }, [dividerPosition])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-6 w-6 animate-spin text-accent" />
      </div>
    )
  }

  if (isError || !policy) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load policy</h2>
          <p className="text-sm text-text-secondary mb-4">Policy not found or an error occurred.</p>
          <div className="flex gap-2 justify-center">
            <Button onClick={() => navigate('/policies')} variant="outline" className="gap-2">
              <ArrowLeft className="h-4 w-4" />
              Back to List
            </Button>
            <Button onClick={() => refetch()} variant="outline" className="gap-2">
              <RefreshCw className="h-4 w-4" />
              Retry
            </Button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-[calc(100vh-var(--header-h))]">
      {/* Header Bar */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border bg-bg-surface shrink-0">
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="icon"
            aria-label="Go back"
            onClick={() => navigate('/policies')}
            className="h-8 w-8"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <Shield className="h-4 w-4 text-accent" />
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-sm font-semibold text-text-primary">{policy.name}</h1>
              <FavoriteToggle
                type="policy"
                id={id ?? ''}
                label={`Policy: ${policy.name}`}
                path={`/policies/${id}`}
              />
              <Badge variant={policy.state === 'active' ? 'success' : 'secondary'} className="text-[10px]">
                {policy.state.toUpperCase()}
              </Badge>
              {selectedVersion && (
                <Badge variant={selectedVersion.state === 'draft' ? 'warning' : selectedVersion.state === 'active' ? 'success' : 'secondary'} className="text-[10px]">
                  v{selectedVersion.version} {selectedVersion.state.toUpperCase()}
                </Badge>
              )}
              {isDirty && (
                <span className="text-[10px] text-warning font-medium">UNSAVED</span>
              )}
              {saveStatus === 'saving' && (
                <span className="text-[10px] text-text-tertiary flex items-center gap-1">
                  <Loader2 className="h-3 w-3 animate-spin" /> Saving...
                </span>
              )}
              {saveStatus === 'saved' && (
                <span className="text-[10px] text-success flex items-center gap-1">
                  <CheckCircle2 className="h-3 w-3" /> Saved
                </span>
              )}
            </div>
            <p className="text-xs text-text-tertiary">
              {policy.scope} scope
              {policy.description && ` \u2014 ${policy.description}`}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Tooltip
            content="Ctrl+S Save · Ctrl+Enter Validate · Ctrl+Shift+Enter Dry-run · Ctrl+Shift+F Format"
            side="bottom"
          >
            <Button
              variant="ghost"
              size="icon"
              aria-label="Keyboard shortcuts"
              className="h-8 w-8 text-text-tertiary"
            >
              <Keyboard className="h-4 w-4" />
            </Button>
          </Tooltip>

          <Button
            variant="outline"
            size="sm"
            className="gap-1.5 text-xs"
            onClick={handleDryRun}
            disabled={dryRunMutation.isPending || !selectedVersionId}
          >
            {dryRunMutation.isPending ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Play className="h-3 w-3" />
            )}
            Dry Run
          </Button>

          <Tooltip
            content={hasErrors ? 'Fix DSL errors before saving' : 'Save the current draft (Ctrl+S) · Ctrl+Shift+F to format'}
            side="bottom"
          >
            <span>
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5 text-xs"
                onClick={handleSave}
                disabled={!isDirty || !isDraft || updateVersionMutation.isPending || hasErrors}
              >
                {updateVersionMutation.isPending ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Save className="h-3 w-3" />
                )}
                Save Draft
              </Button>
            </span>
          </Tooltip>

          <Button
            variant="outline"
            size="sm"
            className="gap-1.5 text-xs"
            onClick={handleCreateVersion}
            disabled={createVersionMutation.isPending}
            title="Clone current version into a new draft"
          >
            {createVersionMutation.isPending ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
            Clone
          </Button>

          <Button
            variant="outline"
            size="sm"
            className="gap-1.5 text-xs"
            onClick={handleExport}
            disabled={!selectedVersion}
            title="Download .dsl and .json files"
          >
            <Download className="h-3 w-3" />
            Export
          </Button>

          <Button
            size="sm"
            className="gap-1.5 text-xs"
            onClick={() => setActivateDialog(true)}
            disabled={!isDraft || isDirty}
          >
            Activate
          </Button>
        </div>
      </div>

      {/* Split Pane Editor */}
      <div id="editor-container" className="flex flex-1 min-h-0">
        {/* Left Pane: Code Editor */}
        <div style={{ width: `${dividerPosition}%` }} className="flex flex-col min-w-0">
          <div className="px-3 py-1.5 bg-bg-elevated border-b border-border flex items-center justify-between shrink-0">
            <span className="text-xs text-text-tertiary font-mono">
              {policy.name}.dsl
              {selectedVersion && ` (v${selectedVersion.version})`}
            </span>
            <span className="text-[10px] text-text-tertiary">
              {dslContent.split('\n').length} lines
            </span>
          </div>
          <div ref={editorScrollRef} className="flex-1 min-h-0 overflow-hidden">
            <ErrorBoundary>
              <DSLEditor
                value={dslContent}
                onChange={handleDslChange}
                onSave={handleSave}
                onDryRun={handleDryRun}
                onValidateNow={handleValidateNow}
                onFormat={handleFormat}
                onDiagnostics={setDiagnostics}
                readOnly={!isDraft}
                className="h-full"
              />
            </ErrorBoundary>
          </div>
          <DSLErrorSummary diagnostics={diagnostics} onJumpTo={handleJumpToDiagnostic} />
        </div>

        {/* Divider */}
        <div
          className="w-1 bg-border hover:bg-accent/50 cursor-col-resize transition-colors shrink-0 relative group"
          onMouseDown={handleDividerMouseDown}
        >
          <div className="absolute inset-y-0 -left-1 -right-1" />
        </div>

        {/* Right Pane: Tabs */}
        <div style={{ width: `${100 - dividerPosition}%` }} className="flex flex-col min-w-0 bg-bg-surface">
          <Tabs value={activeTab} onValueChange={setActiveTab} className="flex flex-col h-full">
            <div className="px-3 py-1.5 border-b border-border shrink-0">
              <TabsList>
                <TabsTrigger value="preview">Preview</TabsTrigger>
                <TabsTrigger value="versions">
                  Versions {policy.versions ? `(${policy.versions.length})` : ''}
                </TabsTrigger>
                <TabsTrigger value="rollout">Rollout</TabsTrigger>
                <TabsTrigger value="audit" className="gap-1.5">
            <Layers className="h-3.5 w-3.5" />
            Audit
          </TabsTrigger>
          <TabsTrigger value="violations" className="gap-1.5">
            <FileWarning className="h-3.5 w-3.5" />
            Violations
          </TabsTrigger>
          <TabsTrigger value="assigned-sims" className="gap-1.5">
            <Wifi className="h-3.5 w-3.5" />
            SIMs
          </TabsTrigger>
          <TabsTrigger value="dsl-help">
                  <HelpCircle className="h-3 w-3 mr-1" />
                  DSL Help
                </TabsTrigger>
              </TabsList>
            </div>

            <div className="flex-1 min-h-0 overflow-hidden">
              <TabsContent value="preview" className="h-full mt-0">
                <PreviewTab
                  result={dryRunResult}
                  isLoading={dryRunMutation.isPending}
                  error={dryRunMutation.error}
                />
              </TabsContent>

              <TabsContent value="versions" className="h-full mt-0">
                <VersionsTab
                  versions={policy.versions || []}
                  currentVersionId={selectedVersionId}
                  onSelectVersion={handleSelectVersion}
                  onCreateVersion={handleCreateVersion}
                  isCreating={createVersionMutation.isPending}
                />
              </TabsContent>

              <TabsContent value="rollout" className="h-full mt-0">
                <RolloutTab
                  policyId={id!}
                  currentVersion={selectedVersion}
                />
              </TabsContent>

              <TabsContent value="dsl-help" className="h-full mt-0 overflow-y-auto">
                <div className="p-4 space-y-5 text-xs">
                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">Basic Structure</h5>
                    <pre className="bg-bg-elevated rounded-[var(--radius-sm)] p-3 font-mono text-[11px] text-text-secondary whitespace-pre overflow-x-auto">{`POLICY "name" {
    MATCH { ... }       # Which SIMs does this apply to?
    RULES { ... }       # What settings/actions to apply?
    CHARGING { ... }    # Billing rules (optional)
}
# Comments start with #`}</pre>
                  </div>

                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">MATCH — Target SIM Selection</h5>
                    <p className="text-text-tertiary mb-2">Determines which SIMs this policy applies to. All conditions must match (AND logic).</p>
                    <div className="text-text-secondary space-y-1.5">
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-accent">apn</code> — APN name <span className="text-text-tertiary">(=, !=, IN)</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-accent">operator</code> — Operator name <span className="text-text-tertiary">(=, !=, IN)</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-accent">rat_type</code> — Radio type <span className="text-text-tertiary">(=, IN) values: nb_iot, lte_m, lte, nr_5g</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-accent">sim_type</code> — SIM type <span className="text-text-tertiary">(=, IN) values: physical, esim</span></div>

                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-accent">metadata.*</code> — Custom fields <span className="text-text-tertiary">(=) e.g. metadata.fleet_id = "fleet-01"</span></div>
                    </div>
                    <pre className="bg-bg-elevated rounded-[var(--radius-sm)] p-2 font-mono text-[11px] text-text-tertiary mt-2 whitespace-pre overflow-x-auto">{`MATCH {
    apn IN ("iot.fleet", "m2m.demo")
    rat_type IN (nb_iot, lte_m)
    sim_type = "physical"
}`}</pre>
                  </div>

                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">RULES — Default Settings</h5>
                    <p className="text-text-tertiary mb-2">Set baseline values for matched SIMs.</p>
                    <div className="text-text-secondary space-y-1.5">
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-success">bandwidth_down</code> / <code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-success">bandwidth_up</code> — <span className="text-text-tertiary">e.g. 1mbps, 256kbps, 10gbps</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-success">session_timeout</code> / <code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-success">idle_timeout</code> — <span className="text-text-tertiary">e.g. 24h, 30min, 7d</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-success">max_sessions</code> — <span className="text-text-tertiary">Max concurrent sessions (integer)</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-success">qos_class</code> / <code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-success">priority</code> — <span className="text-text-tertiary">QoS priority level (integer)</span></div>
                    </div>
                  </div>

                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">WHEN — Conditional Rules</h5>
                    <p className="text-text-tertiary mb-2">Override defaults based on runtime conditions. Supports AND, OR, NOT, parentheses.</p>
                    <div className="text-text-secondary space-y-1.5">
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-purple">usage</code> — Data consumed <span className="text-text-tertiary">(&gt;, &lt;, &gt;=, &lt;=, BETWEEN) e.g. 500MB, 1GB</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-purple">time_of_day</code> — Time range <span className="text-text-tertiary">(IN) e.g. 00:00-06:00, 22:00-23:59</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-purple">session_count</code> — Active sessions <span className="text-text-tertiary">(&gt;, &lt;, =) e.g. 3</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-purple">session_duration</code> — Current session length <span className="text-text-tertiary">(&gt;, &lt;) e.g. 2h</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-purple">bandwidth_used</code> — Current rate <span className="text-text-tertiary">(&gt;, &lt;) e.g. 5mbps</span></div>

                    </div>
                    <pre className="bg-bg-elevated rounded-[var(--radius-sm)] p-2 font-mono text-[11px] text-text-tertiary mt-2 whitespace-pre overflow-x-auto">{`WHEN usage > 1GB AND time_of_day IN (08:00-18:00) {
    bandwidth_down = 512kbps
    ACTION notify(fup_applied, 100%)
}
WHEN usage BETWEEN 800MB 1GB {
    ACTION notify(quota_warning, 80%)
}
WHEN apn = "iot.local" {
    bandwidth_down = 5mbps
}`}</pre>
                  </div>

                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">ACTIONs</h5>
                    <div className="text-text-secondary space-y-1.5">
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-warning">ACTION notify(event_type, threshold%)</code> — <span className="text-text-tertiary">Send alert notification</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-warning">ACTION throttle(rate)</code> — <span className="text-text-tertiary">Reduce bandwidth e.g. throttle(64kbps)</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-warning">ACTION disconnect()</code> — <span className="text-text-tertiary">Terminate session immediately</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-warning">ACTION block()</code> — <span className="text-text-tertiary">Block new connections</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-warning">ACTION log(message)</code> — <span className="text-text-tertiary">Write to audit log</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-warning">ACTION tag(key, value)</code> — <span className="text-text-tertiary">Tag SIM with metadata</span></div>
                    </div>
                  </div>

                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">CHARGING — Billing Rules</h5>
                    <div className="text-text-secondary space-y-1.5">
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">model</code> — <span className="text-text-tertiary">"prepaid" or "postpaid"</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">rate_per_mb</code> — <span className="text-text-tertiary">Cost per MB (float)</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">rate_per_session</code> — <span className="text-text-tertiary">Flat fee per session (float)</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">billing_cycle</code> — <span className="text-text-tertiary">"monthly", "weekly", "daily"</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">quota</code> — <span className="text-text-tertiary">Data cap e.g. 5GB</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">overage_action</code> — <span className="text-text-tertiary">"throttle", "block", "charge"</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">overage_rate_per_mb</code> — <span className="text-text-tertiary">Cost when over quota (float)</span></div>
                      <div><code className="bg-bg-elevated px-1.5 py-0.5 rounded font-mono text-info">rat_multiplier [type] = [val]</code> — <span className="text-text-tertiary">Cost multiplier per RAT</span></div>
                    </div>
                  </div>

                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">Units Reference</h5>
                    <div className="grid grid-cols-3 gap-2 text-text-secondary">
                      <div>
                        <span className="text-text-tertiary block mb-1">Data</span>
                        <div>B, KB, MB, GB, TB</div>
                      </div>
                      <div>
                        <span className="text-text-tertiary block mb-1">Rate</span>
                        <div>bps, kbps, mbps, gbps</div>
                      </div>
                      <div>
                        <span className="text-text-tertiary block mb-1">Time</span>
                        <div>ms, s, min, h, d</div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h5 className="font-medium text-text-primary mb-2 text-sm">Complete Example</h5>
                    <pre className="bg-bg-elevated rounded-[var(--radius-sm)] p-3 font-mono text-[11px] text-text-secondary whitespace-pre overflow-x-auto">{`POLICY "iot-fleet-standard" {
    MATCH {
        apn IN ("iot.fleet", "m2m.meter")
        rat_type IN (nb_iot, lte_m)
        sim_type = "physical"
    }

    RULES {
        # Default settings for all matched SIMs
        bandwidth_down = 1mbps
        bandwidth_up = 256kbps
        session_timeout = 24h
        idle_timeout = 1h
        max_sessions = 1
        qos_class = 5

        # FUP: Throttle after 1GB usage
        WHEN usage > 1GB {
            ACTION throttle(64kbps)
            ACTION notify(quota_exceeded, 100%)
            ACTION log("FUP applied")
        }

        # Warning at 80% quota
        WHEN usage BETWEEN 800MB 1GB {
            ACTION notify(quota_warning, 80%)
        }

        # Night boost
        WHEN time_of_day IN (00:00-06:00) {
            bandwidth_down = 2mbps
            bandwidth_up = 512kbps
        }

        # Block if too many sessions
        WHEN session_count > 3 {
            ACTION disconnect()
            ACTION log("Max sessions exceeded")
        }
    }

    CHARGING {
        model = "prepaid"
        rate_per_mb = 0.02
        billing_cycle = "monthly"
        quota = 5GB
        overage_action = "throttle"
        rat_multiplier nb_iot = 0.5
        rat_multiplier lte_m = 0.8
        rat_multiplier lte = 1.0
        rat_multiplier nr_5g = 1.5
    }
}`}</pre>
                  </div>
                </div>
              </TabsContent>

              {id && (
                <TabsContent value="audit" className="h-full mt-0 overflow-y-auto p-4">
                  <RelatedAuditTab entityId={id} entityType="policy" />
                </TabsContent>
              )}

              {id && (
                <TabsContent value="violations" className="h-full mt-0 overflow-y-auto p-4">
                  <RelatedViolationsTab entityId={id} scope="policy" />
                </TabsContent>
              )}

              <TabsContent value="assigned-sims" className="h-full mt-0 overflow-y-auto">
                <AssignedSimsTab versionId={selectedVersionId} />
              </TabsContent>
            </div>
          </Tabs>
        </div>
      </div>

      {/* Activate Confirmation Dialog */}
      <Dialog open={activateDialog} onOpenChange={setActivateDialog}>
        <DialogContent onClose={() => setActivateDialog(false)}>
          <DialogHeader>
            <DialogTitle>Activate Policy Version</DialogTitle>
            <DialogDescription>
              Activate v{selectedVersion?.version} of "{policy.name}"?
              {dryRunResult && (
                <> This will affect <strong className="text-accent">{dryRunResult.total_affected.toLocaleString()}</strong> SIMs.</>
              )}
              {' '}This action replaces the current active version.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setActivateDialog(false)}>Cancel</Button>
            <Button
              onClick={handleActivate}
              disabled={activateVersionMutation.isPending}
              className="gap-2"
            >
              {activateVersionMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Activate
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
