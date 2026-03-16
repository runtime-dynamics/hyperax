import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import WorkspacesPage from '@/views/workspaces/WorkspacesPage'
import { CronPage } from '@/views/cron/CronPage'
import { EventsPage } from '@/views/events/EventsPage'

export function WorkspacesSection() {
  return (
    <Tabs defaultValue="workspaces" className="flex flex-col h-full">
      <div className="px-6 pt-4 border-b">
        <TabsList>
          <TabsTrigger value="workspaces">Workspaces</TabsTrigger>
          <TabsTrigger value="cron">Cron</TabsTrigger>
          <TabsTrigger value="events">Events</TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="workspaces" className="flex-1 mt-0">
        <WorkspacesPage />
      </TabsContent>
      <TabsContent value="cron" className="flex-1 mt-0">
        <CronPage />
      </TabsContent>
      <TabsContent value="events" className="flex-1 mt-0">
        <EventsPage />
      </TabsContent>
    </Tabs>
  )
}
