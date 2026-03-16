import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import ChatPage from './views/chat/ChatPage'
import OrgPage from './views/org/OrgPage'
import { PipelinesPage } from './views/pipelines/PipelinesPage'
import { TasksPage } from './views/tasks/TasksPage'
import { SettingsLayout } from './views/settings/SettingsLayout'
import { ProvidersSection } from './views/settings/sections/ProvidersSection'
import { WorkspacesSection } from './views/settings/sections/WorkspacesSection'
import { ObservabilitySection } from './views/settings/sections/ObservabilitySection'
import { BudgetSection } from './views/settings/sections/BudgetSection'
import { PluginsSection } from './views/settings/sections/PluginsSection'
import { SystemSection } from './views/settings/sections/SystemSection'
import { RoleTemplatesPage } from './views/settings/RoleTemplatesPage'
import { SecuritySection } from './views/settings/sections/SecuritySection'
import { ActionsPage } from './views/actions/ActionsPage'
import { DocsPage } from './views/docs/DocsPage'
import { AuditPage } from './views/audit/AuditPage'

function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<ChatPage />} />
        <Route path="/pipelines" element={<PipelinesPage />} />
        <Route path="/tasks" element={<TasksPage />} />
        <Route path="/org" element={<OrgPage />} />
        <Route path="/actions" element={<ActionsPage />} />
        <Route path="/docs" element={<DocsPage />} />
        <Route path="/audit" element={<AuditPage />} />
        <Route path="/settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to="/settings/providers" replace />} />
          <Route path="providers" element={<ProvidersSection />} />
          <Route path="role-templates" element={<RoleTemplatesPage />} />
          <Route path="workspaces" element={<WorkspacesSection />} />
          <Route path="observability" element={<ObservabilitySection />} />
          <Route path="budget" element={<BudgetSection />} />
          <Route path="plugins" element={<PluginsSection />} />
          <Route path="security" element={<SecuritySection />} />
          <Route path="system" element={<SystemSection />} />
        </Route>
      </Routes>
    </Layout>
  )
}

export default App
