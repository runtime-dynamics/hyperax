import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mcpCall } from '@/lib/mcp-client'
import { useProviders, type Provider } from '@/services/providerService'

// ─── Interfaces ───────────────────────────────────────────────────────────────

// Matches backend get_budget_status / get_all_budget_statuses item shape
export interface BudgetStatus {
  scope: string
  cost: number
  threshold: number
  remaining: number
  percent_used: number
}

export interface SetBudgetThresholdArgs {
  scope: string
  threshold: number
}

export interface ProviderBudget {
  provider: Provider
  budget: BudgetStatus
}

// Response shape for list_budget_scopes: { scopes: string[], count: number }
interface ListBudgetScopesResponse {
  scopes: string[]
  count: number
}

// Response shape for get_all_budget_statuses: { statuses: BudgetStatus[], count: number }
interface AllBudgetStatusesResponse {
  statuses: BudgetStatus[]
  count: number
}

// ─── Query Keys ──────────────────────────────────────────────────────────────

export const budgetKeys = {
  status: (scope: string) => ['budget-status', scope] as const,
  statusList: (scopes: string[]) => ['budget-status-list', scopes] as const,
  allStatuses: () => ['budget-all-statuses'] as const,
  providerBudgets: () => ['budget-provider-budgets'] as const,
  scopes: () => ['budget-scopes'] as const,
}

// ─── Hooks ────────────────────────────────────────────────────────────────────

export function useBudgetStatus(scope: string) {
  return useQuery({
    queryKey: budgetKeys.status(scope),
    queryFn: () => mcpCall<BudgetStatus>('observability', { action: 'get_budget_status', scope }),
    enabled: !!scope,
    retry: false,
  })
}

export function useBudgetStatusList(scopes: string[]) {
  return useQuery({
    queryKey: budgetKeys.statusList(scopes),
    queryFn: async () => {
      const results = await Promise.all(
        scopes.map((scope) => mcpCall<BudgetStatus>('observability', { action: 'get_budget_status', scope })),
      )
      return results
    },
    enabled: scopes.length > 0,
    retry: false,
  })
}

/**
 * Fetches all budget statuses in a single batch call via get_all_budget_statuses.
 * Returns a flat array of BudgetStatus items.
 */
export function useAllBudgetStatuses() {
  return useQuery({
    queryKey: budgetKeys.allStatuses(),
    queryFn: async (): Promise<BudgetStatus[]> => {
      const result = await mcpCall<AllBudgetStatusesResponse>('observability', { action: 'get_all_budget_statuses' })
      return Array.isArray(result?.statuses) ? result.statuses : []
    },
    retry: false,
  })
}

export function useSetBudgetThreshold() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (args: SetBudgetThresholdArgs) =>
      mcpCall<{ scope: string; threshold: number; status: string }>(
        'observability',
        { action: 'set_budget_threshold', ...(args as unknown as Record<string, unknown>) },
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['budget-status'] })
      void qc.invalidateQueries({ queryKey: ['budget-status-list'] })
      void qc.invalidateQueries({ queryKey: budgetKeys.allStatuses() })
      void qc.invalidateQueries({ queryKey: budgetKeys.providerBudgets() })
      void qc.invalidateQueries({ queryKey: budgetKeys.scopes() })
    },
  })
}

// Re-export useProviders so BudgetPage can import from a single service file
export { useProviders }
export type { Provider }

/**
 * Joins enabled providers with budget statuses from the batch endpoint.
 * Uses get_all_budget_statuses (1 call) instead of N parallel get_budget_status calls.
 */
export function useProviderBudgets() {
  const { data: providers, isLoading: providersLoading, error: providersError } = useProviders()
  const { data: allStatuses, isLoading: statusesLoading, error: statusesError } = useAllBudgetStatuses()

  const enabledProviders = Array.isArray(providers)
    ? (providers as Provider[]).filter((p) => p.is_enabled)
    : []

  return useQuery({
    queryKey: [...budgetKeys.providerBudgets(), enabledProviders.map((p) => p.id)],
    queryFn: (): ProviderBudget[] => {
      // Build a lookup from scope → BudgetStatus using the already-fetched batch result
      const statusMap = new Map<string, BudgetStatus>()
      for (const s of allStatuses ?? []) {
        statusMap.set(s.scope, s)
      }

      return enabledProviders.map((provider) => {
        const scope = `provider:${provider.id}`
        const existing = statusMap.get(scope)
        const budget: BudgetStatus = existing ?? {
          scope,
          cost: 0,
          threshold: 0,
          remaining: 0,
          percent_used: 0,
        }
        return { provider, budget }
      })
    },
    enabled:
      !providersLoading &&
      !providersError &&
      !statusesLoading &&
      !statusesError &&
      enabledProviders.length > 0,
    retry: false,
  })
}

/**
 * Returns all known budget scopes via list_budget_scopes.
 * Response shape: { scopes: string[], count: number }
 */
export function useBudgetScopes() {
  return useQuery({
    queryKey: budgetKeys.scopes(),
    queryFn: async (): Promise<string[]> => {
      const result = await mcpCall<ListBudgetScopesResponse>('observability', { action: 'list_budget_scopes' })
      return Array.isArray(result?.scopes) ? result.scopes : []
    },
    retry: false,
  })
}
