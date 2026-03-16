import { useCallback, useEffect, useState } from 'react'
import { useUpdateAgent } from '@/services/agentService'
import { toast } from '@/components/ui/use-toast'

const FAVORITES_STORAGE_KEY = 'favorited_agents'

export interface FavoriteState {
  favoriteIds: Set<string>
  toggleFavorite: (agentId: string, isFavorite: boolean, isChiefOfStaff?: boolean) => Promise<void>
  isFavorited: (agentId: string) => boolean
  isLoading: boolean
}

/**
 * Hook to manage agent favorites
 * Persists to localStorage and syncs with backend via updateAgent
 */
export function useFavorites(): FavoriteState {
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const [isLoading, setIsLoading] = useState(false)
  const updateAgent = useUpdateAgent()

  // Load favorites from localStorage on mount
  useEffect(() => {
    const stored = localStorage.getItem(FAVORITES_STORAGE_KEY)
    if (stored) {
      try {
        const ids = JSON.parse(stored) as string[]
        setFavoriteIds(new Set(ids))
      } catch (e) {
        console.error('Failed to parse favorites from localStorage:', e)
      }
    }
  }, [])

  // Persist to localStorage whenever favorites change
  useEffect(() => {
    localStorage.setItem(FAVORITES_STORAGE_KEY, JSON.stringify(Array.from(favoriteIds)))
  }, [favoriteIds])

  const toggleFavorite = useCallback(
    async (agentId: string, newState: boolean, isChiefOfStaff?: boolean) => {
      // Prevent unfavoriting Chief of Staff
      if (isChiefOfStaff && !newState) {
        toast({
          title: 'Cannot unfavorite',
          description: 'Chief of Staff must remain in your favorites.',
          variant: 'destructive',
        })
        return
      }

      setIsLoading(true)
      try {
        await updateAgent.mutateAsync({
          agent_id: agentId,
          is_favorite: newState,
        })

        // Update local state
        setFavoriteIds((prev) => {
          const next = new Set(prev)
          if (newState) {
            next.add(agentId)
          } else {
            next.delete(agentId)
          }
          return next
        })

        toast({
          title: newState ? 'Added to favorites' : 'Removed from favorites',
          description: '',
        })
      } catch (error) {
        const msg = error instanceof Error ? error.message : 'Failed to update favorite'
        toast({
          title: 'Error',
          description: msg,
          variant: 'destructive',
        })
      } finally {
        setIsLoading(false)
      }
    },
    [updateAgent],
  )

  const isFavorited = useCallback((agentId: string) => {
    return favoriteIds.has(agentId)
  }, [favoriteIds])

  return {
    favoriteIds,
    toggleFavorite,
    isFavorited,
    isLoading,
  }
}
