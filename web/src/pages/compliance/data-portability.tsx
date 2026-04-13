import { useState } from 'react'
import { toast } from 'sonner'
import { Download, ShieldCheck, Loader2 } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { useRequestDataPortability } from '@/hooks/use-data-portability'

export default function DataPortabilityPage() {
  const [userID, setUserID] = useState('')
  const [lastJobID, setLastJobID] = useState<string | null>(null)
  const request = useRequestDataPortability()

  const handleRequest = async () => {
    if (!userID) {
      toast.error('User ID required')
      return
    }
    try {
      const res = await request.mutateAsync(userID)
      setLastJobID(res.job_id)
      toast.success('Export request queued. You will be notified when ready.')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to request export'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <Breadcrumb items={[{ label: 'Dashboard', href: '/' }, { label: 'Compliance' }, { label: 'Data Portability' }]} />
      <div>
        <h1 className="text-[16px] font-semibold text-text-primary">Data Portability (GDPR Article 20 / KVKK Md.11)</h1>
        <p className="text-xs text-text-tertiary mt-0.5">Request a machine-readable export of a user's personal data</p>
      </div>

      <Card className="p-5">
        <div className="flex items-start gap-3 mb-4">
          <div className="h-9 w-9 rounded-lg bg-bg-hover border border-border flex items-center justify-center">
            <ShieldCheck className="h-4 w-4 text-accent" />
          </div>
          <div>
            <h2 className="text-sm font-semibold text-text-primary">Request export</h2>
            <p className="text-xs text-text-tertiary mt-0.5">
              The export contains the user profile, owned SIMs, last 90 days of CDRs, and audit log entries.
              A signed download link valid for 7 days is sent to the user via the configured notification channels.
            </p>
          </div>
        </div>
        <div className="space-y-4">
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">Target User ID *</label>
            <Input
              type="text"
              placeholder="00000000-0000-0000-0000-000000000000"
              value={userID}
              onChange={(e) => setUserID(e.target.value)}
              className="font-mono text-sm"
            />
            <p className="text-[11px] text-text-tertiary mt-1">
              You may request your own export. Requesting another user's export requires <code className="text-[10px]">tenant_admin</code> role.
            </p>
          </div>
          <Button onClick={handleRequest} disabled={request.isPending} className="gap-1.5">
            {request.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
            {request.isPending ? 'Submitting...' : 'Request Export'}
          </Button>
        </div>
      </Card>

      {lastJobID && (
        <Card className="p-5 border-success">
          <div className="flex items-start gap-3">
            <ShieldCheck className="h-5 w-5 text-success flex-shrink-0 mt-0.5" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium text-text-primary">Export queued</p>
              <p className="text-xs text-text-secondary mt-1">
                Job ID: <code className="font-mono">{lastJobID}</code>
              </p>
              <p className="text-xs text-text-tertiary mt-1">
                Track progress on the Jobs page. The user will receive a download link when the export completes.
              </p>
            </div>
          </div>
        </Card>
      )}
    </div>
  )
}
