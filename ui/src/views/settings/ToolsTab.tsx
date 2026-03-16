import { useState, useRef, useId } from 'react'
import { Send, Wrench, ChevronDown, ChevronRight, Clock, AlertCircle, CheckCircle2 } from 'lucide-react'
import { mcpCall, McpError } from '@/lib/mcp-client'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

const COMMON_TOOLS = [
  'workspace',
  'config',
  'observability',
  'agent',
  'comm',
  'pipeline',
  'plugin',
  'project',
  'secret',
  'doc',
  'event',
  'governance',
  'memory',
  'audit',
  'code',
  'refactor',
]

interface ToolCallRecord {
  id: number
  tool: string
  args: Record<string, unknown>
  result: unknown
  error: string | null
  durationMs: number
  timestamp: Date
}

let nextId = 1

function formatJson(value: unknown): string {
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
}

interface HistoryItemProps {
  record: ToolCallRecord
}

function HistoryItem({ record }: HistoryItemProps) {
  const [expanded, setExpanded] = useState(true)

  return (
    <div className="border rounded-lg overflow-hidden text-sm">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-muted/40 hover:bg-muted/70 transition-colors text-left"
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        {record.error ? (
          <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />
        ) : (
          <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-green-500" />
        )}
        <span className="font-mono font-medium flex-1 truncate">{record.tool}</span>
        <span className="text-xs text-muted-foreground shrink-0">{record.durationMs}ms</span>
        <span className="text-xs text-muted-foreground shrink-0">
          {record.timestamp.toLocaleTimeString()}
        </span>
      </button>

      {expanded && (
        <div className="divide-y">
          <div className="px-3 py-2 bg-background">
            <p className="text-xs font-medium text-muted-foreground mb-1">Args</p>
            <pre className="font-mono text-xs whitespace-pre-wrap break-all text-foreground">
              {formatJson(record.args)}
            </pre>
          </div>
          <div className="px-3 py-2 bg-background">
            <p className="text-xs font-medium text-muted-foreground mb-1">
              {record.error ? 'Error' : 'Result'}
            </p>
            <pre
              className={cn(
                'font-mono text-xs whitespace-pre-wrap break-all',
                record.error ? 'text-destructive' : 'text-foreground',
              )}
            >
              {record.error ?? formatJson(record.result)}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}

export function ToolsTab() {
  const toolInputId = useId()
  const argsInputId = useId()

  const [toolName, setToolName] = useState('')
  const [argsText, setArgsText] = useState('{}')
  const [isLoading, setIsLoading] = useState(false)
  const [argsError, setArgsError] = useState<string | null>(null)
  const [showSuggestions, setShowSuggestions] = useState(false)
  const [history, setHistory] = useState<ToolCallRecord[]>([])

  const inputRef = useRef<HTMLInputElement>(null)

  const suggestions = toolName.trim()
    ? COMMON_TOOLS.filter((t) => t.toLowerCase().includes(toolName.toLowerCase()))
    : COMMON_TOOLS

  function selectSuggestion(name: string) {
    setToolName(name)
    setShowSuggestions(false)
    inputRef.current?.focus()
  }

  function validateArgs(): Record<string, unknown> | null {
    const trimmed = argsText.trim()
    if (trimmed === '' || trimmed === '{}') return {}
    try {
      const parsed = JSON.parse(trimmed) as unknown
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        setArgsError('Arguments must be a JSON object, e.g. {"key": "value"}')
        return null
      }
      setArgsError(null)
      return parsed as Record<string, unknown>
    } catch {
      setArgsError('Invalid JSON. Check your syntax.')
      return null
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const tool = toolName.trim()
    if (!tool) return

    const args = validateArgs()
    if (args === null) return

    setIsLoading(true)
    const start = performance.now()

    try {
      const result = await mcpCall(tool, args)
      const durationMs = Math.round(performance.now() - start)
      setHistory((prev) => [
        {
          id: nextId++,
          tool,
          args,
          result,
          error: null,
          durationMs,
          timestamp: new Date(),
        },
        ...prev,
      ])
    } catch (err) {
      const durationMs = Math.round(performance.now() - start)
      const message = err instanceof McpError ? err.message : String(err)
      setHistory((prev) => [
        {
          id: nextId++,
          tool,
          args,
          result: null,
          error: message,
          durationMs,
          timestamp: new Date(),
        },
        ...prev,
      ])
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="border rounded-lg p-4 space-y-4">
        <div className="flex items-center gap-2 mb-1">
          <Wrench className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-sm font-medium">Call an MCP Tool</h3>
        </div>

        <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4">
          <div className="space-y-1.5 relative">
            <Label htmlFor={toolInputId} className="text-xs">
              Tool name
            </Label>
            <Input
              id={toolInputId}
              ref={inputRef}
              value={toolName}
              onChange={(e) => {
                setToolName(e.target.value)
                setShowSuggestions(true)
              }}
              onFocus={() => setShowSuggestions(true)}
              onBlur={() => setTimeout(() => setShowSuggestions(false), 150)}
              placeholder="e.g. list_workspaces"
              className="font-mono text-sm"
              autoComplete="off"
              spellCheck={false}
            />
            {showSuggestions && suggestions.length > 0 && (
              <div className="absolute z-10 top-full mt-1 w-full bg-popover border rounded-md shadow-md overflow-hidden">
                {suggestions.map((s) => (
                  <button
                    key={s}
                    type="button"
                    onMouseDown={() => selectSuggestion(s)}
                    className={cn(
                      'w-full text-left px-3 py-1.5 text-sm font-mono hover:bg-accent hover:text-accent-foreground transition-colors',
                      toolName === s && 'bg-accent text-accent-foreground',
                    )}
                  >
                    {s}
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor={argsInputId} className="text-xs">
              Arguments (JSON)
            </Label>
            <Textarea
              id={argsInputId}
              value={argsText}
              onChange={(e) => {
                setArgsText(e.target.value)
                setArgsError(null)
              }}
              placeholder="{}"
              className="font-mono text-sm min-h-[80px] resize-y"
              spellCheck={false}
            />
            {argsError && (
              <p className="text-xs text-destructive flex items-center gap-1">
                <AlertCircle className="h-3 w-3 shrink-0" />
                {argsError}
              </p>
            )}
          </div>

          <div className="flex items-center justify-between">
            <Button
              type="submit"
              size="sm"
              disabled={isLoading || !toolName.trim()}
            >
              {isLoading ? (
                <>
                  <span className="h-3.5 w-3.5 mr-2 rounded-full border-2 border-current border-t-transparent animate-spin" />
                  Calling...
                </>
              ) : (
                <>
                  <Send className="h-3.5 w-3.5 mr-2" />
                  Call Tool
                </>
              )}
            </Button>
            {history.length > 0 && (
              <button
                type="button"
                onClick={() => setHistory([])}
                className="text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                Clear history
              </button>
            )}
          </div>
        </form>
      </div>

      {history.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Clock className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              History
            </span>
            <Badge variant="secondary" className="text-xs h-5">
              {history.length}
            </Badge>
          </div>
          <div className="space-y-2">
            {history.map((record) => (
              <HistoryItem key={record.id} record={record} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
