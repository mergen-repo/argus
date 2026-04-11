import { useState } from 'react'
import {
  Plus,
  Power,
  PowerOff,
  ArrowRightLeft,
  Trash2,
  Loader2,
  Smartphone,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
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
import {
  useESimListBySim,
  useEnableProfile,
  useDisableProfile,
  useSwitchProfile,
  useCreateProfile,
  useDeleteProfile,
} from '@/hooks/use-esim'
import { useOperatorList } from '@/hooks/use-operators'
import type { ESimProfile, ESimProfileState, ESimCreateRequest } from '@/types/esim'

type ActionType = 'enable' | 'disable' | 'switch' | 'delete' | 'load'

function stateBadgeVariant(state: ESimProfileState): 'success' | 'default' | 'warning' | 'secondary' {
  switch (state) {
    case 'enabled': return 'success'
    case 'available': return 'default'
    case 'disabled': return 'warning'
    default: return 'secondary'
  }
}

function truncate(str: string, len = 16) {
  if (str.length <= len) return str
  return str.slice(0, len) + '...'
}

interface LoadProfileForm {
  eid: string
  operator_id: string
  iccid_on_profile: string
  profile_id: string
}

const EMPTY_LOAD_FORM: LoadProfileForm = {
  eid: '',
  operator_id: '',
  iccid_on_profile: '',
  profile_id: '',
}

interface Props {
  simId: string
}

export function ESimTab({ simId }: Props) {
  const [actionDialog, setActionDialog] = useState<{
    profile: ESimProfile
    action: ActionType
  } | null>(null)
  const [loadDialogOpen, setLoadDialogOpen] = useState(false)
  const [loadForm, setLoadForm] = useState<LoadProfileForm>(EMPTY_LOAD_FORM)
  const [switchTargetId, setSwitchTargetId] = useState('')

  const { data: profiles, isLoading } = useESimListBySim(simId)
  const { data: operators = [] } = useOperatorList()

  const enableMutation = useEnableProfile()
  const disableMutation = useDisableProfile()
  const switchMutation = useSwitchProfile()
  const createMutation = useCreateProfile()
  const deleteMutation = useDeleteProfile()

  const isPending =
    enableMutation.isPending ||
    disableMutation.isPending ||
    switchMutation.isPending ||
    createMutation.isPending ||
    deleteMutation.isPending

  const operatorName = (id: string) =>
    operators.find((o) => o.id === id)?.name ?? truncate(id, 8)

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

  const handleLoad = async () => {
    try {
      const body: ESimCreateRequest = {
        sim_id: simId,
        eid: loadForm.eid,
        operator_id: loadForm.operator_id,
        iccid_on_profile: loadForm.iccid_on_profile,
        profile_id: loadForm.profile_id,
      }
      await createMutation.mutateAsync(body)
      setLoadDialogOpen(false)
      setLoadForm(EMPTY_LOAD_FORM)
    } catch {
      // handled by api interceptor
    }
  }

  const visibleProfiles = (profiles ?? []).filter((p) => p.profile_state !== 'deleted')

  return (
    <div className="space-y-4 py-2">
      <div className="flex items-center justify-between">
        <h2 className="text-[16px] font-semibold text-text-primary">
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
          onClick={() => setLoadDialogOpen(true)}
        >
          <Plus className="h-3.5 w-3.5" />
          Load Profile
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
              <p className="text-xs text-text-secondary">Load a profile to get started with this eSIM.</p>
            </div>
            <Button size="sm" className="gap-1.5" onClick={() => setLoadDialogOpen(true)}>
              <Plus className="h-3.5 w-3.5" />
              Load Profile
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
                  {operatorName(profile.operator_id)}
                </span>
              </div>

              <div className="mt-3 grid grid-cols-2 gap-x-6 gap-y-1.5">
                <div>
                  <span className="text-xs uppercase tracking-wider text-text-tertiary">EID</span>
                  <p className="font-mono text-xs text-text-secondary mt-0.5">{truncate(profile.eid)}</p>
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
              </div>

              <div className="mt-3 flex gap-2 justify-end">
                {profile.profile_state === 'enabled' && (
                  <>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-7 px-2.5 text-[11px] gap-1 border-warning/30 text-warning hover:bg-warning-dim"
                      onClick={() => setActionDialog({ profile, action: 'disable' })}
                    >
                      <PowerOff className="h-3 w-3" />
                      Disable
                    </Button>
                    {switchTargets(profile.id).length > 0 && (
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          className="inline-flex items-center gap-1 h-7 px-2.5 text-[11px] rounded-[var(--radius-sm)] border border-border bg-transparent text-text-secondary hover:text-text-primary hover:bg-bg-elevated transition-colors"
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
                      className="h-7 px-2.5 text-[11px] gap-1"
                      onClick={() => setActionDialog({ profile, action: 'enable' })}
                    >
                      <Power className="h-3 w-3" />
                      Enable
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-7 px-2.5 text-[11px] gap-1 border-danger/30 text-danger hover:bg-danger-dim"
                      onClick={() => setActionDialog({ profile, action: 'delete' })}
                    >
                      <Trash2 className="h-3 w-3" />
                      Delete
                    </Button>
                  </>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Action Confirmation Dialog */}
      <Dialog
        open={!!actionDialog && actionDialog.action !== 'load'}
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

      {/* Load Profile Dialog */}
      <Dialog open={loadDialogOpen} onOpenChange={setLoadDialogOpen}>
        <DialogContent onClose={() => { setLoadDialogOpen(false); setLoadForm(EMPTY_LOAD_FORM) }}>
          <DialogHeader>
            <DialogTitle>Load eSIM Profile</DialogTitle>
            <DialogDescription>
              Download and attach a new eSIM profile to this SIM. Max 8 profiles per SIM.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                EID <span className="text-danger">*</span>
              </label>
              <Input
                value={loadForm.eid}
                onChange={(e) => setLoadForm((f) => ({ ...f, eid: e.target.value }))}
                placeholder="32-character EID..."
                className="font-mono text-sm"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                Operator <span className="text-danger">*</span>
              </label>
              <Select
                value={loadForm.operator_id}
                onChange={(e) => setLoadForm((f) => ({ ...f, operator_id: e.target.value }))}
                placeholder="Select operator..."
                options={operators.map((o) => ({ value: o.id, label: o.name }))}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                ICCID on Profile <span className="text-danger">*</span>
              </label>
              <Input
                value={loadForm.iccid_on_profile}
                onChange={(e) => setLoadForm((f) => ({ ...f, iccid_on_profile: e.target.value }))}
                placeholder="Up to 22 digits..."
                className="font-mono text-sm"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-secondary block mb-1.5">
                Profile ID <span className="text-text-tertiary text-[10px] ml-1">(optional)</span>
              </label>
              <Input
                value={loadForm.profile_id}
                onChange={(e) => setLoadForm((f) => ({ ...f, profile_id: e.target.value }))}
                placeholder="SM-DP+ profile identifier..."
                className="font-mono text-sm"
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => { setLoadDialogOpen(false); setLoadForm(EMPTY_LOAD_FORM) }}
            >
              Cancel
            </Button>
            <Button
              onClick={handleLoad}
              disabled={isPending || !loadForm.eid || !loadForm.operator_id || !loadForm.iccid_on_profile}
              className="gap-2"
            >
              {createMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              Load Profile
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
