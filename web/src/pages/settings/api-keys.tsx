import { useState } from 'react'
import {
  Plus,
  AlertCircle,
  RefreshCw,
  Loader2,
  Copy,
  Check,
  RotateCw,
  Trash2,
  Key,
  Eye,
  EyeOff,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { SlidePanel } from '@/components/ui/slide-panel'
import {
  useApiKeyList,
  useCreateApiKey,
  useRotateApiKey,
  useRevokeApiKey,
} from '@/hooks/use-settings'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

const SCOPE_OPTIONS = [
  { value: 'sims:read', label: 'SIMs Read' },
  { value: 'sims:write', label: 'SIMs Write' },
  { value: 'sessions:read', label: 'Sessions Read' },
  { value: 'sessions:write', label: 'Sessions Write' },
  { value: 'policies:read', label: 'Policies Read' },
  { value: 'policies:write', label: 'Policies Write' },
  { value: 'analytics:read', label: 'Analytics Read' },
  { value: 'operators:read', label: 'Operators Read' },
]

export default function ApiKeysPage() {
  const { data: keys, isLoading, isError, refetch } = useApiKeyList()
  const createMutation = useCreateApiKey()
  const rotateMutation = useRotateApiKey()
  const revokeMutation = useRevokeApiKey()

  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [createForm, setCreateForm] = useState({
    name: '',
    scopes: [] as string[],
    rate_limit: 100,
    expires_in_days: 365,
  })
  const [createdKey, setCreatedKey] = useState<string | null>(null)
  const [copiedKey, setCopiedKey] = useState(false)
  const [showKey, setShowKey] = useState(false)
  const [confirmAction, setConfirmAction] = useState<{ id: string; action: 'rotate' | 'revoke'; name: string } | null>(null)
  const [rotatedKey, setRotatedKey] = useState<string | null>(null)

  const toggleScope = (scope: string) => {
    setCreateForm((f) => ({
      ...f,
      scopes: f.scopes.includes(scope)
        ? f.scopes.filter((s) => s !== scope)
        : [...f.scopes, scope],
    }))
  }

  const handleCreate = async () => {
    try {
      const result = await createMutation.mutateAsync(createForm)
      setCreatedKey(result.key)
      setCopiedKey(false)
      setShowKey(true)
    } catch {
      // handled by api interceptor
    }
  }

  const handleCopyKey = (key: string) => {
    navigator.clipboard.writeText(key)
    setCopiedKey(true)
    setTimeout(() => setCopiedKey(false), 2000)
  }

  const handleCloseCreate = () => {
    setShowCreateDialog(false)
    setCreatedKey(null)
    setShowKey(false)
    setCreateForm({ name: '', scopes: [], rate_limit: 100, expires_in_days: 365 })
  }

  const handleConfirmAction = async () => {
    if (!confirmAction) return
    try {
      if (confirmAction.action === 'rotate') {
        const result = await rotateMutation.mutateAsync(confirmAction.id)
        setRotatedKey(result.key)
      } else {
        await revokeMutation.mutateAsync(confirmAction.id)
        setConfirmAction(null)
      }
    } catch {
      // handled by api interceptor
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load API keys</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch API key data.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">API Keys</h1>
        <Button size="sm" className="gap-2" onClick={() => setShowCreateDialog(true)}>
          <Plus className="h-3.5 w-3.5" />
          Create Key
        </Button>
      </div>

      <Card className="overflow-hidden density-compact">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Prefix</TableHead>
                <TableHead>Scopes</TableHead>
                <TableHead>Rate Limit</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="w-24">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 4 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 7 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && (!keys || keys.length === 0) && (
                <TableRow>
                  <TableCell colSpan={7}>
                    <div className="flex flex-col items-center justify-center py-16 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <Key className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No API keys</h3>
                        <p className="text-xs text-text-secondary">Create your first API key for machine-to-machine access.</p>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {(keys ?? []).map((key) => (
                <TableRow key={key.id}>
                  <TableCell>
                    <span className="text-sm font-medium text-text-primary">{key.name}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">...{key.prefix}</span>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {key.scopes.slice(0, 3).map((scope) => (
                        <Badge key={scope} variant="outline" className="text-[10px]">
                          {scope}
                        </Badge>
                      ))}
                      {key.scopes.length > 3 && (
                        <Badge variant="secondary" className="text-[10px]">
                          +{key.scopes.length - 3}
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-text-secondary">{key.rate_limit ? `${key.rate_limit}/min` : '-'}</span>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">
                      {new Date(key.created_at).toLocaleDateString()}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className={cn(
                      'text-xs',
                      key.expires_at && new Date(key.expires_at) < new Date() ? 'text-danger' : 'text-text-secondary',
                    )}>
                      {key.expires_at ? new Date(key.expires_at).toLocaleDateString() : 'Never'}
                    </span>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        title="Rotate key"
                        onClick={() => setConfirmAction({ id: key.id, action: 'rotate', name: key.name })}
                      >
                        <RotateCw className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-danger hover:text-danger"
                        title="Revoke key"
                        onClick={() => setConfirmAction({ id: key.id, action: 'revoke', name: key.name })}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        {!isLoading && keys && keys.length > 0 && (
          <div className="px-4 py-3 border-t border-border-subtle">
            <p className="text-center text-xs text-text-tertiary">
              {keys.length} API key{keys.length !== 1 ? 's' : ''}
            </p>
          </div>
        )}
      </Card>

      {/* Create API Key SlidePanel */}
      <SlidePanel
        open={showCreateDialog}
        onOpenChange={handleCloseCreate}
        title={createdKey ? 'API Key Created' : 'Create API Key'}
        description={createdKey ? 'Copy this key now. You will not be able to see it again.' : 'Generate a new key for machine-to-machine access.'}
        width={createdKey ? 'sm' : 'md'}
      >
        {createdKey ? (
          <>
            <div className="space-y-3">
              <div className="relative">
                <div className="flex items-center gap-2 p-3 rounded-[var(--radius-sm)] border border-accent/30 bg-accent-dim font-mono text-sm break-all">
                  {showKey ? createdKey : createdKey.replace(/./g, '*')}
                </div>
                <div className="absolute right-2 top-2 flex items-center gap-1">
                  <button
                    onClick={() => setShowKey(!showKey)}
                    className="text-text-tertiary hover:text-text-primary transition-colors p-1"
                  >
                    {showKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  </button>
                  <button
                    onClick={() => handleCopyKey(createdKey)}
                    className="text-text-tertiary hover:text-text-primary transition-colors p-1"
                  >
                    {copiedKey ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
                  </button>
                </div>
              </div>
              <p className="text-xs text-warning flex items-center gap-1.5">
                <AlertCircle className="h-3.5 w-3.5 flex-shrink-0" />
                Store this key securely. It cannot be retrieved after closing this panel.
              </p>
            </div>
            <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
              <Button onClick={handleCloseCreate}>Done</Button>
            </div>
          </>
        ) : (
          <>
            <div className="space-y-4">
              <div>
                <label className="text-xs text-text-secondary block mb-1.5">Name</label>
                <Input
                  value={createForm.name}
                  onChange={(e) => setCreateForm((f) => ({ ...f, name: e.target.value }))}
                  placeholder="e.g. Production Integration"
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1.5">Scopes</label>
                <div className="grid grid-cols-2 gap-2">
                  {SCOPE_OPTIONS.map((scope) => (
                    <label
                      key={scope.value}
                      className={cn(
                        'flex items-center gap-2 px-3 py-2 rounded-[var(--radius-sm)] border text-xs cursor-pointer transition-colors',
                        createForm.scopes.includes(scope.value)
                          ? 'border-accent/30 bg-accent-dim text-accent'
                          : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary',
                      )}
                    >
                      <input
                        type="checkbox"
                        checked={createForm.scopes.includes(scope.value)}
                        onChange={() => toggleScope(scope.value)}
                        className="sr-only"
                      />
                      <div className={cn(
                        'h-3.5 w-3.5 rounded-[3px] border flex items-center justify-center flex-shrink-0',
                        createForm.scopes.includes(scope.value)
                          ? 'border-accent bg-accent'
                          : 'border-border',
                      )}>
                        {createForm.scopes.includes(scope.value) && <Check className="h-2.5 w-2.5 text-bg-primary" />}
                      </div>
                      {scope.label}
                    </label>
                  ))}
                </div>
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1.5">
                  Rate Limit (requests/minute)
                </label>
                <Input
                  type="number"
                  value={createForm.rate_limit}
                  onChange={(e) => setCreateForm((f) => ({ ...f, rate_limit: parseInt(e.target.value) || 100 }))}
                  min={1}
                  max={10000}
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1.5">
                  Expiry (days)
                </label>
                <Input
                  type="number"
                  value={createForm.expires_in_days}
                  onChange={(e) => setCreateForm((f) => ({ ...f, expires_in_days: parseInt(e.target.value) || 365 }))}
                  min={1}
                  max={3650}
                />
              </div>
            </div>
            <div className="flex items-center justify-end gap-3 pt-4 border-t border-border mt-6">
              <Button variant="outline" onClick={handleCloseCreate}>
                Cancel
              </Button>
              <Button
                onClick={handleCreate}
                disabled={!createForm.name || createForm.scopes.length === 0 || createMutation.isPending}
                className="gap-2"
              >
                {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                Create Key
              </Button>
            </div>
          </>
        )}
      </SlidePanel>

      {/* Rotate/Revoke Confirmation Dialog */}
      <Dialog open={!!confirmAction && !rotatedKey} onOpenChange={() => { setConfirmAction(null); setRotatedKey(null) }}>
        <DialogContent onClose={() => { setConfirmAction(null); setRotatedKey(null) }}>
          <DialogHeader>
            <DialogTitle>
              {confirmAction?.action === 'rotate' ? 'Rotate API Key?' : 'Revoke API Key?'}
            </DialogTitle>
            <DialogDescription>
              {confirmAction?.action === 'rotate'
                ? `This will generate a new key for "${confirmAction.name}" and invalidate the current one.`
                : `This will permanently revoke "${confirmAction?.name}". Any systems using this key will lose access.`}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmAction(null)}>
              Cancel
            </Button>
            <Button
              variant={confirmAction?.action === 'revoke' ? 'destructive' : 'default'}
              onClick={handleConfirmAction}
              disabled={rotateMutation.isPending || revokeMutation.isPending}
              className="gap-2"
            >
              {(rotateMutation.isPending || revokeMutation.isPending) && <Loader2 className="h-4 w-4 animate-spin" />}
              {confirmAction?.action === 'rotate' ? 'Rotate' : 'Revoke'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rotated Key Display Dialog */}
      <Dialog open={!!rotatedKey} onOpenChange={() => { setRotatedKey(null); setConfirmAction(null) }}>
        <DialogContent onClose={() => { setRotatedKey(null); setConfirmAction(null) }}>
          <DialogHeader>
            <DialogTitle>Key Rotated</DialogTitle>
            <DialogDescription>
              Copy the new key now. You will not be able to see it again.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="flex items-center gap-2 p-3 rounded-[var(--radius-sm)] border border-accent/30 bg-accent-dim font-mono text-sm break-all">
              {rotatedKey}
            </div>
            <button
              onClick={() => { if (rotatedKey) handleCopyKey(rotatedKey) }}
              className="flex items-center gap-1.5 text-xs text-text-secondary hover:text-accent transition-colors"
            >
              {copiedKey ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
              {copiedKey ? 'Copied!' : 'Copy to clipboard'}
            </button>
          </div>
          <DialogFooter>
            <Button onClick={() => { setRotatedKey(null); setConfirmAction(null) }}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
