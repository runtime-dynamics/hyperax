import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { TokensPage } from '@/views/security/TokensPage'
import { SecretsPage } from '@/views/security/SecretsPage'
import { DelegationsPage } from '@/views/security/DelegationsPage'
import { SecretProviderPage } from '@/views/security/SecretProviderPage'

export function SecuritySection() {
  return (
    <Tabs defaultValue="tokens" className="flex flex-col h-full">
      <div className="px-6 pt-4 border-b">
        <TabsList>
          <TabsTrigger value="tokens">API Tokens</TabsTrigger>
          <TabsTrigger value="secrets">Secrets</TabsTrigger>
          <TabsTrigger value="delegations">Delegations</TabsTrigger>
          <TabsTrigger value="secret-provider">Secret Provider</TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="tokens" className="flex-1 mt-0">
        <TokensPage />
      </TabsContent>
      <TabsContent value="secrets" className="flex-1 mt-0">
        <SecretsPage />
      </TabsContent>
      <TabsContent value="delegations" className="flex-1 mt-0">
        <DelegationsPage />
      </TabsContent>
      <TabsContent value="secret-provider" className="flex-1 mt-0">
        <SecretProviderPage />
      </TabsContent>
    </Tabs>
  )
}
