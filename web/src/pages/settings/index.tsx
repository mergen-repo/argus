import React, { Suspense } from 'react'
import { Settings } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Select } from '@/components/ui/select'
import { EmptyState } from '@/components/shared/empty-state'
import { useAuthStore } from '@/stores/auth'
import { hasMinRole } from '@/lib/rbac'
import { useHashTab } from '@/hooks/use-hash-tab'

const SecurityTab = React.lazy(() => import('./tabs/security-tab'))
const SessionsTab = React.lazy(() => import('./tabs/sessions-tab'))
const ReliabilityTab = React.lazy(() => import('./tabs/reliability-tab'))
const NotificationsTab = React.lazy(() => import('./tabs/notifications-tab'))
const PreferencesTab = React.lazy(() => import('./tabs/preferences-tab'))

interface TabDef {
  key: string
  label: string
  minRole?: string
  Component: React.LazyExoticComponent<() => React.ReactElement | null>
}

const TAB_DEFS: TabDef[] = [
  { key: 'security', label: 'Security', Component: SecurityTab },
  { key: 'sessions', label: 'Sessions', Component: SessionsTab },
  { key: 'reliability', label: 'Reliability', minRole: 'super_admin', Component: ReliabilityTab },
  { key: 'notifications', label: 'Notifications', Component: NotificationsTab },
  { key: 'preferences', label: 'Preferences', Component: PreferencesTab },
]

export default function SettingsPage() {
  const userRole = useAuthStore((s) => s.user?.role)

  const visibleTabs = TAB_DEFS.filter(
    (t) => !t.minRole || hasMinRole(userRole, t.minRole),
  )

  const [tab, setTab] = useHashTab(
    visibleTabs[0]?.key ?? 'security',
    visibleTabs.map((t) => t.key),
  )

  if (visibleTabs.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center p-8">
        <EmptyState
          icon={Settings}
          title="No accessible settings"
          description="You do not have permission to view any settings."
        />
      </div>
    )
  }

  const selectOptions = visibleTabs.map((t) => ({ value: t.key, label: t.label }))

  return (
    <div>
      <div className="mb-6">
        <h1 className="text-lg font-semibold text-text-primary">Settings</h1>
        <p className="text-sm text-text-secondary">Manage your account and system preferences</p>
      </div>

      <Tabs value={tab} onValueChange={setTab}>
        <div className="mb-4">
          <TabsList className="hidden md:inline-flex">
            {visibleTabs.map((t) => (
              <TabsTrigger key={t.key} value={t.key}>
                {t.label}
              </TabsTrigger>
            ))}
          </TabsList>

          <div className="md:hidden">
            <Select
              aria-label="Select settings tab"
              options={selectOptions}
              value={tab}
              onChange={(e) => setTab(e.target.value)}
            />
          </div>
        </div>

        <Suspense fallback={null}>
          {visibleTabs.map((t) => (
            <TabsContent key={t.key} value={t.key}>
              <t.Component />
            </TabsContent>
          ))}
        </Suspense>
      </Tabs>
    </div>
  )
}
