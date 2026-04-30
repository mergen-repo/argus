import { useState } from 'react'
import { Server, RefreshCw } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useInfraHealth } from '@/hooks/use-ops'
import { NATSPanel } from './_partials/nats-panel'
import { DBPanel } from './_partials/db-panel'
import { RedisPanel } from './_partials/redis-panel'

export default function InfraHealth() {
  const [tab, setTab] = useState('nats')
  const { data, isLoading, refetch } = useInfraHealth(10_000)

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-64" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-accent" />
          <h1 className="text-[15px] font-semibold text-text-primary">Infrastructure Health</h1>
          <span className="text-[11px] text-text-tertiary">(10s refresh)</span>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()} className="text-text-secondary hover:text-text-primary">
          <RefreshCw className="h-4 w-4 mr-1" />
          Refresh
        </Button>
      </div>

      <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
        <CardContent className="p-6">
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList className="mb-6 bg-bg-elevated">
              <TabsTrigger value="nats" className="text-[13px]">NATS Bus</TabsTrigger>
              <TabsTrigger value="db" className="text-[13px]">Database</TabsTrigger>
              <TabsTrigger value="redis" className="text-[13px]">Redis</TabsTrigger>
            </TabsList>

            <TabsContent value="nats">
              <NATSPanel data={data?.nats} />
            </TabsContent>

            <TabsContent value="db">
              <DBPanel data={data?.db} />
            </TabsContent>

            <TabsContent value="redis">
              <RedisPanel data={data?.redis} />
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </div>
  )
}
