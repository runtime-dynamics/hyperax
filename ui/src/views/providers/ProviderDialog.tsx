import { useState, useEffect } from 'react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { ScrollWell } from '@/components/ui/scroll-well'
import { type Provider, type CreateProviderArgs } from '@/services/restProviderService'
import { parseModels } from '@/services/providerService'

const KIND_OPTIONS = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'google', label: 'Google Gemini' },
  { value: 'ollama', label: 'Ollama' },
  { value: 'azure', label: 'Azure OpenAI' },
  { value: 'bedrock', label: 'AWS Bedrock' },
  { value: 'custom', label: 'OpenAI Compatible API' },
]

// Providers with fixed base URLs — the user only needs to supply an API key.
const MANAGED_BASE_URLS: Record<string, string> = {
  openai: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com',
  google: 'https://generativelanguage.googleapis.com',
}

export interface ProviderDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editTarget: Provider | null
  onCreate: (data: CreateProviderArgs, cb: { onSuccess: () => void; onError: (e: Error) => void }) => void
  onUpdate: (data: Partial<Provider> & { id: string; api_key?: string }, cb: { onSuccess: () => void; onError: (e: Error) => void }) => void
  isPending: boolean
}

export function ProviderDialog({ open, onOpenChange, editTarget, onCreate, onUpdate, isPending }: ProviderDialogProps) {
  const isEdit = !!editTarget

  const [name, setName] = useState(editTarget?.name ?? '')
  const [kind, setKind] = useState(editTarget?.kind ?? '')
  const [baseUrl, setBaseUrl] = useState(editTarget?.base_url ?? '')
  const [apiKey, setApiKey] = useState('')
  const [isEnabled, setIsEnabled] = useState(editTarget?.is_enabled ?? true)
  const [isDefault, setIsDefault] = useState(editTarget?.is_default ?? false)
  const [modelsList, setModelsList] = useState<string[]>(editTarget ? parseModels(editTarget.models) : [])
  const [newModel, setNewModel] = useState('')
  const [nameError, setNameError] = useState('')
  const [kindError, setKindError] = useState('')
  const [urlError, setUrlError] = useState('')

  useEffect(() => {
    if (open) {
      setName(editTarget?.name ?? '')
      setKind(editTarget?.kind ?? '')
      setBaseUrl(editTarget?.base_url ?? '')
      setApiKey('')
      setIsEnabled(editTarget?.is_enabled ?? true)
      setIsDefault(editTarget?.is_default ?? false)
      setModelsList(editTarget ? parseModels(editTarget.models) : [])
      setNewModel('')
      setNameError('')
      setKindError('')
      setUrlError('')
    }
  }, [open, editTarget])

  function resetForm(target: Provider | null) {
    setName(target?.name ?? '')
    setKind(target?.kind ?? '')
    setBaseUrl(target?.base_url ?? '')
    setApiKey('')
    setIsEnabled(target?.is_enabled ?? true)
    setIsDefault(target?.is_default ?? false)
    setModelsList(target ? parseModels(target.models) : [])
    setNewModel('')
    setNameError('')
    setKindError('')
    setUrlError('')
  }

  function addModel() {
    const trimmed = newModel.trim()
    if (trimmed && !modelsList.includes(trimmed)) {
      setModelsList([...modelsList, trimmed])
      setNewModel('')
    }
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) resetForm(editTarget)
    onOpenChange(nextOpen)
  }

  function validate(): boolean {
    let valid = true
    if (!name.trim()) {
      setNameError('Name is required')
      valid = false
    } else {
      setNameError('')
    }
    if (!kind) {
      setKindError('Kind is required')
      valid = false
    } else {
      setKindError('')
    }
    const effectiveUrl = MANAGED_BASE_URLS[kind] ?? baseUrl
    if (!effectiveUrl.trim()) {
      setUrlError('Base URL is required')
      valid = false
    } else if (!effectiveUrl.startsWith('http://') && !effectiveUrl.startsWith('https://')) {
      setUrlError('URL must start with http:// or https://')
      valid = false
    } else {
      setUrlError('')
    }
    return valid
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!validate()) return

    const effectiveUrl = MANAGED_BASE_URLS[kind] ?? baseUrl
    const modelsPayload = modelsList.length > 0 ? JSON.stringify(modelsList) : '[]'

    if (isEdit && editTarget) {
      onUpdate(
        { id: editTarget.id, name, kind, base_url: effectiveUrl, is_enabled: isEnabled, models: modelsPayload, ...(apiKey ? { api_key: apiKey } : {}) },
        {
          onSuccess: () => handleOpenChange(false),
          onError: () => {},
        },
      )
    } else {
      onCreate(
        { name, kind, base_url: effectiveUrl, is_enabled: isEnabled, is_default: isDefault, models: modelsPayload, ...(apiKey ? { api_key: apiKey } : {}) },
        {
          onSuccess: () => handleOpenChange(false),
          onError: () => {},
        },
      )
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-lg max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'Edit Provider' : 'Add Provider'}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? 'Update the LLM provider configuration.'
              : 'Configure a new LLM provider for your agents.'}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4 overflow-y-auto flex-1 min-h-0">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="pr-name">Name *</Label>
              <Input
                id="pr-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="My OpenAI Provider"
                autoFocus
              />
              {nameError && <p className="text-xs text-destructive">{nameError}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pr-kind">Kind *</Label>
              <Select value={kind} onValueChange={(val) => {
                setKind(val)
                const managed = MANAGED_BASE_URLS[val]
                if (managed) setBaseUrl(managed)
              }}>
                <SelectTrigger id="pr-kind">
                  <SelectValue placeholder="Select kind..." />
                </SelectTrigger>
                <SelectContent>
                  {KIND_OPTIONS.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {kindError && <p className="text-xs text-destructive">{kindError}</p>}
            </div>
          </div>

          {MANAGED_BASE_URLS[kind] ? (
            <div className="space-y-1.5">
              <Label>Base URL</Label>
              <p className="text-sm text-muted-foreground font-mono bg-muted/50 px-3 py-2 rounded-md">
                {MANAGED_BASE_URLS[kind]}
              </p>
            </div>
          ) : (
            <div className="space-y-1.5">
              <Label htmlFor="pr-url">Base URL *</Label>
              <Input
                id="pr-url"
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
                placeholder={kind === 'bedrock' ? 'https://bedrock-runtime.us-east-1.amazonaws.com' : 'https://api.example.com'}
              />
              {urlError && <p className="text-xs text-destructive">{urlError}</p>}
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="pr-key">
              {kind === 'bedrock' ? 'AWS Credentials' : 'API Key'}
            </Label>
            <Input
              id="pr-key"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={kind === 'bedrock' ? 'ACCESS_KEY_ID:SECRET_ACCESS_KEY' : isEdit ? 'Leave blank to keep existing key' : 'sk-...'}
            />
            <p className="text-xs text-muted-foreground">
              {kind === 'bedrock'
                ? 'Format: ACCESS_KEY_ID:SECRET_ACCESS_KEY'
                : isEdit
                  ? 'Leave blank to keep the current key. Enter a new key to replace it.'
                  : 'Your API key will be securely stored. Leave blank for unauthenticated providers (e.g. Ollama).'}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label>Models</Label>
            <div className="flex gap-2">
              <Input
                value={newModel}
                onChange={(e) => setNewModel(e.target.value)}
                placeholder="Enter model name..."
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    addModel()
                  }
                }}
              />
              <Button type="button" variant="outline" size="sm" onClick={addModel} className="shrink-0">
                Add
              </Button>
            </div>
            <ScrollWell className="mt-2">
              {modelsList.length > 0 ? (
                <div className="flex flex-wrap gap-1 items-start w-full">
                  {modelsList.map((m) => (
                    <Badge key={m} variant="secondary" className="text-xs gap-1 pr-1">
                      {m}
                      <button
                        type="button"
                        className="ml-1 rounded-full hover:bg-destructive/20 hover:text-destructive px-0.5"
                        onClick={() => setModelsList(modelsList.filter((x) => x !== m))}
                      >
                        &times;
                      </button>
                    </Badge>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground italic pt-1">No models added yet.</p>
              )}
            </ScrollWell>
            <p className="text-xs text-muted-foreground">
              Use Test Connection to auto-discover models, or add them manually above.
            </p>
          </div>

          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <input
                id="pr-enabled"
                type="checkbox"
                checked={isEnabled}
                onChange={(e) => setIsEnabled(e.target.checked)}
                className="h-4 w-4 rounded border border-input accent-primary"
              />
              <Label htmlFor="pr-enabled" className="cursor-pointer">
                Enabled
              </Label>
            </div>
            {!isEdit && (
              <div className="flex items-center gap-2">
                <input
                  id="pr-default"
                  type="checkbox"
                  checked={isDefault}
                  onChange={(e) => setIsDefault(e.target.checked)}
                  className="h-4 w-4 rounded border border-input accent-primary"
                />
                <Label htmlFor="pr-default" className="cursor-pointer">
                  Set as default provider
                </Label>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending
                ? isEdit
                  ? 'Saving...'
                  : 'Adding...'
                : isEdit
                  ? 'Save Changes'
                  : 'Add Provider'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
