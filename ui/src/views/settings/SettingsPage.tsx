import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { PageHeader } from '@/components/domain/page-header'
import { ConfigTab } from './ConfigTab'
import { ServerTab } from './ServerTab'
import { ToolsTab } from './ToolsTab'

export default function SettingsPage() {
  return (
    <div className="p-6">
      <div className="max-w-5xl mx-auto space-y-6">
        <PageHeader title="System" description="Configuration keys, server info, and registered tools." />
        <Tabs defaultValue="config">
          <TabsList>
            <TabsTrigger value="config">Configuration</TabsTrigger>
            <TabsTrigger value="server">Server</TabsTrigger>
            <TabsTrigger value="tools">Tools</TabsTrigger>
          </TabsList>
          <TabsContent value="config" className="mt-4">
            <ConfigTab />
          </TabsContent>
          <TabsContent value="server" className="mt-4">
            <ServerTab />
          </TabsContent>
          <TabsContent value="tools" className="mt-4">
            <ToolsTab />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}
