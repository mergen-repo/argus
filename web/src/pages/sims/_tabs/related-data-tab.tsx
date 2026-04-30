import * as React from 'react'
import { Shield, Bell, Radio, FileWarning } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  RelatedAuditTab,
  RelatedNotificationsPanel,
  RelatedAlertsPanel,
  RelatedViolationsTab,
} from '@/components/shared'

interface RelatedDataTabProps {
  simId: string
}

export function RelatedDataTab({ simId }: RelatedDataTabProps) {
  const [tab, setTab] = React.useState('audit')

  return (
    <Tabs value={tab} onValueChange={setTab} className="mt-4">
      <TabsList>
        <TabsTrigger value="audit" className="gap-1.5">
          <Shield className="h-3.5 w-3.5" />
          Audit
        </TabsTrigger>
        <TabsTrigger value="notifications" className="gap-1.5">
          <Bell className="h-3.5 w-3.5" />
          Notifications
        </TabsTrigger>
        <TabsTrigger value="violations" className="gap-1.5">
          <FileWarning className="h-3.5 w-3.5" />
          Violations
        </TabsTrigger>
        <TabsTrigger value="alerts" className="gap-1.5">
          <Radio className="h-3.5 w-3.5" />
          Alerts
        </TabsTrigger>
      </TabsList>

      <TabsContent value="audit" className="mt-4">
        <RelatedAuditTab entityId={simId} entityType="sim" />
      </TabsContent>

      <TabsContent value="notifications" className="mt-4">
        <RelatedNotificationsPanel entityId={simId} />
      </TabsContent>

      <TabsContent value="violations" className="mt-4">
        <RelatedViolationsTab entityId={simId} scope="sim" />
      </TabsContent>

      <TabsContent value="alerts" className="mt-4">
        <RelatedAlertsPanel entityId={simId} entityType="sim" />
      </TabsContent>
    </Tabs>
  )
}
