import { useState } from 'react'
import {
  Plus,
  Power,
  PowerOff,
  ArrowRightLeft,
  Trash2,
  Loader2,
  Smartphone,
  Copy,
  Check,
  Clock,
  ChevronDown,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip } from '@/components/ui/tooltip'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import {
  useESimListBySim,
  useEnableProfile,
  useDisableProfile,
  useSwitchProfile,
  useDeleteProfile,
  useEsimOTAHistory,
} from '@/hooks/use-esim'
import { AllocateFromStockPanel } from '@/components/esim/allocate-from-stock-panel'
import { formatEID } from '@/lib/format'
import type { ESimProfile, ESimProfileState, OTACommand } from '@/types/esim'

type ActionType = 'enable' | 'disable' | 'switch' | 'delete'

function stateBadgeVariant(state: ESimProfileState): 'success' | 'default' | 'warning' | 'secondary' | 'danger' {
  switch (state) {
    case 'enabled': return 'success'
    case 'available': return 'secondary'
    case 'disabled': return 'warning'
    case 'deleted': return 'danger'
    default: return 'secondary'
  }
}

function truncate(str: string, len = 16) {
  if (str.length <= len) return str
  return str.slice(0, len) + '...'
}

function CopyButton({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Button
      variant="ghost"
      size="icon"
      className="h-5 w-5 flex-shrink-0 text-text-tertiary hover:text-text-primary transition-colors"
      onClick={handleCopy}
      title={label ? `Copy ${label}` : 'Copy'}
    >
      {copied
        ? <Check className="h-3 w-3" style={{ color: 'var(--color-success)' }} />
        : <Copy className="h-3 w-3" />}
    </Button>
  )
}

function OTAStatusBadge({ status }: { status: OTACommand['status'] }) {
  const variantMap: Record<OTACommand['status'], 'success' | 'warning' | 'danger' | 'secondary'> = {
    acked: 'success',
    sent: 'warning',
    queued: 'secondary',
    failed: 'danger',
    timeout: 'danger',
  }
  return (
    <Badge variant={variantMap[status]} className="text-xs px-1.5 py-0">
      {status.toUpperCase()}
    </Badge>
  )
}

function ProfileHistoryPanel({ profileId, eid, onClose }: { profileId: string; eid: string; onClose: () => void }) {
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useEsimOTAHistory(profileId)
  const commands = data?.pages.flatMap((p) => p.data) ?? []

  return (
    <SlidePanel
      open={true}
      onOpenChange={onClose}
      title="OTA Command History"
      description={`Profile ${truncate(profileId, 16)} · EID ${formatEID(eid)}`}
      width="md"
    >
      {isLoading && (
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full rounded-[var(--radius-card)]" />
          ))}
        </div>
      )}

      {!isLoading && commands.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 text-center gap-3">
          <div className="rounded-xl border border-border bg-bg-elevated p-4">
            <Clock className="h-8 w-8 text-text-tertiary" />
          </div>
          <p className="text-sm font-medium text-text-primary">No OTA history yet</p>
          <p className="text-xs text-text-secondary">Commands will appear here once issued.</p>
        </div>
      )}

      {!isLoading && commands.length > 0 && (
        <div className="space-y-2">
          {commands.map((cmd) => (
            <div
              key={cmd.id}
              className="rounded-[var(--radius-card)] border border-border bg-bg-surface p-3 space-y-1.5"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-medium text-text-primary capitalize">{cmd.command_type}</span>
                <OTAStatusBadge status={cmd.status} />
              </div>
              <div className="grid grid-cols-2 gap-x-4 gap-y-1">
                <div>
                  <span className="text-xs uppercase tracking-wider text-text-tertiary">Created</span>
                  <p className="text-xs text-text-secondary">{new Date(cmd.created_at).toLocaleString()}</p>
                </div>
                {cmd.acked_at && (
                  <div>
                    <span className="text-xs uppercase tracking-wider text-text-tertiary">Acked</span>
                    <p className="text-xs text-text-secondary">{new Date(cmd.acked_at).toLocaleString()}</p>
                  </div>
                )}
                {cmd.retry_count > 0 && (
                  <div>
                    <span className="text-xs uppercase tracking-wider text-text-tertiary">Retries</span>
                    <p className="text-xs text-text-secondary">{cmd.retry_count}</p>
                  </div>
                )}
              </div>
              {cmd.error_message && (
                <p className="text-xs text-danger truncate" title={cmd.error_message}>
                  {cmd.error_message}
                </p>
              )}
            </div>
          ))}

          {hasNextPage && (
            <Button
              variant="outline"
              size="sm"
              className="w-full gap-1.5 text-xs"
              onClick={() => fetchNextPage()}
              disabled={isFetchingNextPage}
            >
              {isFetchingNextPage
                ? <><Loader2 className="h-3 w-3 animate-spin" /> Loading...</>
                : <><ChevronDown className="h-3 w-3" /> Load more</>}
            </Button>
          )}
        </div>
      )}

      <SlidePanelFooter>
        <Button variant="outline" onClick={onClose}>Close</Button>
      </SlidePanelFooter>
    </SlidePanel>
  )
}

interface Props {
  simId: string
}

export function ESimTab({ simId }: Props) {
  const [actionDialog, setActionDialog] = useState<{
    profile: ESimProfile
    action: ActionType
  } | null>(null)
  const [allocatePanelOpen, setAllocatePanelOpen] = useState(false)
  const [historyProfile, setHistoryProfile] = useState<ESimProfile | null>(null)
  const [switchTargetId, setSwitchTargetId] = useState('')

  const { data: profiles, isLoading } = useESimListBySim(simId)

  const enableMutation = useEnableProfile()
  const disableMutation = useDisableProfile()
  const switchMutation = useSwitchProfile()
  const deleteMutation = useDeleteProfile()

  const isPending =
    enableMutation.isPending ||
    disableMutation.isPending ||
    switchMutation.isPending ||
    deleteMutation.isPending

  const switchTargets = (currentId: string) =>
    (profiles ?? []).filter(
      (p) =>
        p.id !== currentId &&
        (p.profile_state === 'available' || p.profile_state === 'disabled'),
    )

  const handleAction = async () => {
    if (!actionDialog) return
    try {
      const { profile, action } = actionDialog
      if (action === 'enable') {
        await enableMutation.mutateAsync(profile.id)
      } else if (action === 'disable') {
        await disableMutation.mutateAsync(profile.id)
      } else if (action === 'switch' && switchTargetId) {
        await switchMutation.mutateAsync({
          profileId: profile.id,
          targetProfileId: switchTargetId,
        })
      } else if (action === 'delete') {
        await deleteMutation.mutateAsync(profile.id)
      }
      setActionDialog(null)
      setSwitchTargetId('')
    } catch {
      // handled by api interceptor
    }
  }

  const visibleProfiles = (profiles ?? []).filter((p) => p.profile_state !== 'deleted')

  return (
    <div className="space-y-4 py-2">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold text-text-primary">
          eSIM Profiles
          {visibleProfiles.length > 0 && (
            <span className="ml-2 text-xs font-normal text-text-tertiary">
              ({visibleProfiles.length})
            </span>
          )}
        </h2>
        <Button
          size="sm"
          className="gap-1.5"
          onClick={() => setAllocatePanelOpen(true)}
        >
          <Plus className="h-3.5 w-3.5" />
          Allocate from Stock
        </Button>
      </div>

      {isLoading && (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i} className="p-4">
              <div className="flex items-center justify-between mb-3">
                <Skeleton className="h-5 w-20" />
                <Skeleton className="h-5 w-32" />
              </div>
              <div className="space-y-2">
                <Skeleton className="h-4 w-48" />
                <Skeleton className="h-4 w-36" />
                <Skeleton className="h-4 w-40" />
              </div>
            </Card>
          ))}
        </div>
      )}

      {!isLoading && visibleProfiles.length === 0 && (
        <Card className="p-10">
          <div className="flex flex-col items-center justify-center text-center gap-4">
            <div className="rounded-xl border border-border bg-bg-elevated p-4">
              <Smartphone className="h-8 w-8 text-text-tertiary" />
            </div>
            <div>
              <p className="text-sm font-medium text-text-primary mb-1">No eSIM profiles loaded</p>
              <p className="text-xs text-text-secondary">Allocate a profile from operator stock to get started.</p>
            </div>
            <Button size="sm" className="gap-1.5" onClick={() => setAllocatePanelOpen(true)}>
              <Plus className="h-3.5 w-3.5" />
              Allocate from Stock
            </Button>
          </div>
        </Card>
      )}

      {!isLoading && visibleProfiles.length > 0 && (
        <div className="space-y-3">
          {visibleProfiles.map((profile) => (
            <Card key={profile.id} className="p-4 shadow-[var(--shadow-card)]">
              <div className="flex items-start justify-between gap-4">
                <div className="flex items-center gap-2 min-w-0">
                  <Badge variant={stateBadgeVariant(profile.profile_state)} className="gap-1 shrink-0">
                    {profile.profile_state === 'enabled' && (
                      <span className="h-1.5 w-1.5 rounded-full bg-current animate-pulse" />
                    )}
                    {profile.profile_state.toUpperCase()}
                  </Badge>
                  {profile.profile_id && (
                    <span className="font-mono text-xs text-text-secondary truncate">
                      {profile.profile_id}
                    </span>
                  )}
                </div>
                <span className="text-xs text-text-secondary shrink-0">
                  {profile.operator_name ?? truncate(profile.operator_id, 8)}
                </span>
              </div>

              <div className="mt-3 grid grid-cols-2 gap-x-6 gap-y-1.5">
                <div>
                  <span className="text-xs uppercase tracking-wider text-text-tertiary">EID</span>
                  <div className="flex items-center gap-1 mt-0.5">
                    <Tooltip content={profile.eid} side="top">
                      <span className="font-mono text-xs text-text-secondary">{formatEID(profile.eid)}</span>
                    </Tooltip>
                    <CopyButton text={profile.eid} label="EID" />
                  </div>
                </div>
                <div>
                  <span className="text-xs uppercase tracking-wider text-text-tertiary">ICCID</span>
                  <p className="font-mono text-xs text-text-secondary mt-0.5">
                    {profile.iccid_on_profile ? truncate(profile.iccid_on_profile) : '-'}
                  </p>
                </div>
                {profile.last_provisioned_at && (
                  <div className="col-span-2">
                    <span className="text-xs uppercase tracking-wider text-text-tertiary">Provisioned</span>
                    <p className="text-xs text-text-secondary mt-0.5">
                      {new Date(profile.last_provisioned_at).toLocaleString()}
                    </p>
                  </div>
                )}
                {profile.last_error && (
                  <div className="col-span-2">
                    <span className="text-xs uppercase tracking-wider text-text-tertiary">Last Error</span>
                    <p className="text-xs text-danger mt-0.5 truncate" title={profile.last_error}>
                      {profile.last_error}
                    </p>
                  </div>
                )}
              </div>

              <div className="mt-3 flex gap-2 items-center justify-between">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2.5 text-xs gap-1 text-text-secondary hover:text-text-primary"
                  onClick={() => setHistoryProfile(profile)}
                >
                  <Clock className="h-3 w-3" />
                  View History
                </Button>

                <div className="flex gap-2">
                  {profile.profile_state === 'enabled' && (
                    <>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 px-2.5 text-xs gap-1 border-warning/30 text-warning hover:bg-warning-dim"
                        onClick={() => setActionDialog({ profile, action: 'disable' })}
                      >
                        <PowerOff className="h-3 w-3" />
                        Disable
                      </Button>
                      {switchTargets(profile.id).length > 0 && (
                        <DropdownMenu>
                          <DropdownMenuTrigger
                            className="inline-flex items-center gap-1 h-7 px-2.5 text-xs rounded-[var(--radius-sm)] border border-border bg-transparent text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors"
                          >
                            <ArrowRightLeft className="h-3 w-3" />
                            Switch
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            {switchTargets(profile.id).map((target) => (
                              <DropdownMenuItem
                                key={target.id}
                                onClick={() => {
                                  setSwitchTargetId(target.id)
                                  setActionDialog({ profile, action: 'switch' })
                                }}
                              >
                                <span className="text-xs">
                                  {target.profile_id ? truncate(target.profile_id, 12) : truncate(target.id, 12)}
                                  <span className="ml-2 text-text-tertiary">{target.profile_state}</span>
                                </span>
                              </DropdownMenuItem>
                            ))}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      )}
                    </>
                  )}
                  {(profile.profile_state === 'available' || profile.profile_state === 'disabled') && (
                    <>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 px-2.5 text-xs gap-1"
                        onClick={() => setActionDialog({ profile, action: 'enable' })}
                      >
                        <Power className="h-3 w-3" />
                        Enable
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 px-2.5 text-xs gap-1 border-danger/30 text-danger hover:bg-danger-dim"
                        onClick={() => setActionDialog({ profile, action: 'delete' })}
                      >
                        <Trash2 className="h-3 w-3" />
                        Delete
                      </Button>
                    </>
                  )}
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Allocate from Stock Panel */}
      <AllocateFromStockPanel
        open={allocatePanelOpen}
        onOpenChange={setAllocatePanelOpen}
        simId={simId}
      />

      {/* OTA History Panel */}
      {historyProfile && (
        <ProfileHistoryPanel
          profileId={historyProfile.id}
          eid={historyProfile.eid}
          onClose={() => setHistoryProfile(null)}
        />
      )}

      {/* Action Confirmation Dialog */}
      <Dialog
        open={!!actionDialog}
        onOpenChange={() => { setActionDialog(null); setSwitchTargetId('') }}
      >
        <DialogContent onClose={() => { setActionDialog(null); setSwitchTargetId('') }}>
          <DialogHeader>
            <DialogTitle>
              {actionDialog?.action === 'enable' && 'Enable Profile?'}
              {actionDialog?.action === 'disable' && 'Disable Profile?'}
              {actionDialog?.action === 'switch' && 'Switch Profile?'}
              {actionDialog?.action === 'delete' && 'Delete Profile?'}
            </DialogTitle>
            <DialogDescription>
              {actionDialog?.action === 'enable' && (
                <>Activate this eSIM profile. The device will connect using this profile.</>
              )}
              {actionDialog?.action === 'disable' && (
                <>Disable the active profile. The device will lose connectivity until another profile is enabled.</>
              )}
              {actionDialog?.action === 'switch' && switchTargetId && (
                <>
                  Switch to profile{' '}
                  <span className="font-mono text-accent">
                    {(profiles ?? []).find((p) => p.id === switchTargetId)?.profile_id ?? truncate(switchTargetId)}
                  </span>
                  . The current profile will revert to available state.
                </>
              )}
              {actionDialog?.action === 'delete' && (
                <>Permanently remove this eSIM profile from the SIM. This cannot be undone.</>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setActionDialog(null); setSwitchTargetId('') }}>
              Cancel
            </Button>
            <Button
              variant={actionDialog?.action === 'delete' || actionDialog?.action === 'disable' ? 'destructive' : 'default'}
              onClick={handleAction}
              disabled={isPending}
              className="gap-2"
            >
              {isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              {actionDialog?.action === 'enable' && 'Enable'}
              {actionDialog?.action === 'disable' && 'Disable'}
              {actionDialog?.action === 'switch' && 'Switch'}
              {actionDialog?.action === 'delete' && 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
