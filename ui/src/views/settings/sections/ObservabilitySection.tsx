import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { TelemetryPage } from '@/views/telemetry/TelemetryPage'
import { AuditPage } from '@/views/audit/AuditPage'
import { MemoryPage } from '@/views/memory/MemoryPage'
import { HintsPage } from '@/views/hints/HintsPage'
import { LifecyclePage } from '@/views/lifecycle/LifecyclePage'

export function ObservabilitySection() {
  return (
    <Tabs defaultValue="telemetry" className="flex flex-col h-full">
      <div className="px-6 pt-4 border-b">
        <TabsList>
          <TabsTrigger value="telemetry">Telemetry</TabsTrigger>
          <TabsTrigger value="audit">Audit</TabsTrigger>
          <TabsTrigger value="memory">Memory</TabsTrigger>
          <TabsTrigger value="hints">Hints</TabsTrigger>
          <TabsTrigger value="lifecycle">Lifecycle</TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="telemetry" className="flex-1 mt-0">
        <TelemetryPage />
      </TabsContent>
      <TabsContent value="audit" className="flex-1 mt-0">
        <AuditPage />
      </TabsContent>
      <TabsContent value="memory" className="flex-1 mt-0">
        <MemoryPage />
      </TabsContent>
      <TabsContent value="hints" className="flex-1 mt-0">
        <HintsPage />
      </TabsContent>
      <TabsContent value="lifecycle" className="flex-1 mt-0">
        <LifecyclePage />
      </TabsContent>
    </Tabs>
  )
}
