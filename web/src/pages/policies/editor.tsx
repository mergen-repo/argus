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

export default function PolicyEditorPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const { data: policy, isLoading, isError, refetch } = usePolicy(id!)

  const [activeTab, setActiveTab] = useState('preview')
  const [dslContent, setDslContent] = useState('')
  const [selectedVersionId, setSelectedVersionId] = useState<string | undefined>()
  const [isDirty, setIsDirty] = useState(false)
  const [activateDialog, setActivateDialog] = useState(false)
  const [dryRunResult, setDryRunResult] = useState<DryRunResult | null>(null)
  const [dividerPosition, setDividerPosition] = useState(55)
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved'>('idle')

  const dryRunTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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

  useEffect(() => {
    const handleKeydown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault()
        handleSave()
      }
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
        e.preventDefault()
        handleDryRun()
      }
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [handleSave, handleDryRun])

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
            onClick={() => navigate('/policies')}
            className="h-8 w-8"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <Shield className="h-4 w-4 text-accent" />
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-sm font-semibold text-text-primary">{policy.name}</h1>
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
          <Tooltip content="Ctrl+S: Save | Ctrl+Enter: Dry Run" side="bottom">
            <Button
              variant="ghost"
              size="icon"
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

          <Button
            variant="outline"
            size="sm"
            className="gap-1.5 text-xs"
            onClick={handleSave}
            disabled={!isDirty || !isDraft || updateVersionMutation.isPending}
          >
            {updateVersionMutation.isPending ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Save className="h-3 w-3" />
            )}
            Save Draft
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
          <div className="flex-1 min-h-0 overflow-hidden">
            <DSLEditor
              value={dslContent}
              onChange={handleDslChange}
              onSave={handleSave}
              onDryRun={handleDryRun}
              readOnly={!isDraft}
              className="h-full"
            />
          </div>
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
