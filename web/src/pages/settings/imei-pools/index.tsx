import { useMemo } from 'react'
import { ShieldCheck, AlertTriangle, Ban, Upload } from 'lucide-react'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useHashTab } from '@/hooks/use-hash-tab'
import { POOL_LABEL, type IMEIPool } from '@/types/imei-pool'
import { IMEILookupTrigger } from '@/components/imei-lookup/imei-lookup-trigger'
import { PoolListTab } from './pool-list-tab'
import { BulkImportTab } from './bulk-import-tab'

const TAB_KEYS = ['whitelist', 'greylist', 'blacklist', 'bulk-import'] as const
type TabKey = (typeof TAB_KEYS)[number]

const TAB_DEFS: ReadonlyArray<{ key: TabKey; label: string; icon: typeof ShieldCheck }> = [
  { key: 'whitelist', label: POOL_LABEL.whitelist, icon: ShieldCheck },
  { key: 'greylist', label: POOL_LABEL.greylist, icon: AlertTriangle },
  { key: 'blacklist', label: POOL_LABEL.blacklist, icon: Ban },
  { key: 'bulk-import', label: 'Bulk Import', icon: Upload },
]

export default function IMEIPoolsPage() {
  const validTabs = useMemo(() => [...TAB_KEYS], [])
  const [tab, setTab] = useHashTab('whitelist', validTabs)
  const activeTab = (TAB_KEYS as readonly string[]).includes(tab) ? (tab as TabKey) : 'whitelist'

  const importInitialPool: IMEIPool = useMemo(() => {
    if (activeTab === 'greylist' || activeTab === 'blacklist' || activeTab === 'whitelist') {
      return activeTab
    }
    return 'whitelist'
  }, [activeTab])

  return (
    <div className="space-y-4">
      <header className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <p className="text-[11px] font-medium uppercase tracking-[1.5px] text-text-tertiary">
            Settings · Devices
          </p>
          <h1 className="text-[18px] font-semibold text-text-primary">IMEI Pools</h1>
          <p className="text-xs text-text-secondary max-w-2xl">
            Manage device IMEI authorization. Whitelist allows known devices, greylist
            flags suspect ones for review, blacklist denies access. Each pool supports
            full IMEIs (15 digits) and TAC ranges (8-digit prefix).
          </p>
        </div>
        <IMEILookupTrigger size="sm" className="gap-2 shrink-0" />
      </header>

      <Tabs value={activeTab} onValueChange={(v) => setTab(v)}>
        <TabsList className="flex flex-wrap">
          {TAB_DEFS.map((t) => {
            const Icon = t.icon
            return (
              <TabsTrigger key={t.key} value={t.key} className="gap-1.5">
                <Icon className="h-3.5 w-3.5" />
                {t.label}
              </TabsTrigger>
            )
          })}
        </TabsList>

        <TabsContent value="whitelist">
          <PoolListTab pool="whitelist" onSwitchToImport={() => setTab('bulk-import')} />
        </TabsContent>
        <TabsContent value="greylist">
          <PoolListTab pool="greylist" onSwitchToImport={() => setTab('bulk-import')} />
        </TabsContent>
        <TabsContent value="blacklist">
          <PoolListTab pool="blacklist" onSwitchToImport={() => setTab('bulk-import')} />
        </TabsContent>
        <TabsContent value="bulk-import">
          <BulkImportTab initialPool={importInitialPool} />
        </TabsContent>
      </Tabs>
    </div>
  )
}
