import { useState } from 'react'
import { Check, Pencil, X } from 'lucide-react'
import { useConfigKeys, useSetConfig } from '@/services/configService'
import { LoadingState } from '@/components/domain/loading-state'
import { ErrorState } from '@/components/domain/error-state'
import { EmptyState } from '@/components/domain/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { toast } from '@/components/ui/use-toast'
import { Settings } from 'lucide-react'

export function ConfigTab() {
  const { data: keys, isLoading, error, refetch } = useConfigKeys()
  const { mutate: setConfig } = useSetConfig()
  const [editing, setEditing] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')

  function startEdit(key: string, currentValue: string) {
    setEditing(key)
    setEditValue(currentValue)
  }

  function cancelEdit() {
    setEditing(null)
    setEditValue('')
  }

  function saveEdit(key: string) {
    setConfig(
      { key, value: editValue },
      {
        onSuccess: () => {
          toast({ title: 'Config updated', description: `"${key}" has been saved.` })
          cancelEdit()
        },
        onError: (err) => {
          toast({ title: 'Save failed', description: (err as Error).message, variant: 'destructive' })
        },
      },
    )
  }

  if (isLoading) return <LoadingState message="Loading configuration..." />
  if (error) return <ErrorState error={error as Error} onRetry={() => void refetch()} />

  if (!keys || keys.length === 0) {
    return (
      <EmptyState
        icon={Settings}
        title="No configuration keys"
        description="The configuration store has no keys registered."
      />
    )
  }

  return (
    <div className="border rounded-lg overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="text-left px-4 py-3 font-medium text-muted-foreground">Key</th>
            <th className="text-left px-4 py-3 font-medium text-muted-foreground">Type</th>
            <th className="text-left px-4 py-3 font-medium text-muted-foreground">Value</th>
            <th className="text-left px-4 py-3 font-medium text-muted-foreground">Description</th>
            <th className="w-20 px-4 py-3" />
          </tr>
        </thead>
        <tbody>
          {keys.map((ck, i) => {
            const displayValue = ck.current_value ?? ck.default_val
            return (
              <tr key={ck.key} className={i < keys.length - 1 ? 'border-b' : ''}>
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs">{ck.key}</span>
                    {ck.critical && (
                      <Badge variant="destructive" className="text-xs">critical</Badge>
                    )}
                  </div>
                </td>
                <td className="px-4 py-3 text-muted-foreground text-xs">{ck.value_type}</td>
                <td className="px-4 py-3">
                  {editing === ck.key ? (
                    <Input
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      className="h-7 text-xs font-mono w-40"
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') saveEdit(ck.key)
                        if (e.key === 'Escape') cancelEdit()
                      }}
                      autoFocus
                    />
                  ) : (
                    <span className="font-mono text-xs">{displayValue || '—'}</span>
                  )}
                </td>
                <td className="px-4 py-3 text-xs text-muted-foreground max-w-xs">{ck.description}</td>
                <td className="px-4 py-3">
                  {editing === ck.key ? (
                    <div className="flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-green-600"
                        onClick={() => saveEdit(ck.key)}
                      >
                        <Check className="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="icon" className="h-7 w-7" onClick={cancelEdit}>
                        <X className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  ) : (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-muted-foreground hover:text-foreground"
                      onClick={() => startEdit(ck.key, displayValue)}
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
