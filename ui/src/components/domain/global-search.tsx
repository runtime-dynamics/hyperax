import { useState, useRef, useEffect } from 'react'
import { Search, FileText, Code2, X } from 'lucide-react'
import { useWorkspaces } from '@/services/workspaceService'
import {
  useSearchDocs,
  useSearchCode,
  type DocSearchResult,
} from '@/services/docService'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { LoadingState } from '@/components/domain/loading-state'
import { cn } from '@/lib/utils'
import { useNavigate } from 'react-router-dom'

export function GlobalSearch() {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [submittedQuery, setSubmittedQuery] = useState('')
  const [filterWorkspace, setFilterWorkspace] = useState('__all__')
  const [searchDocs, setSearchDocs] = useState(true)
  const [searchCode, setSearchCode] = useState(true)
  const panelRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()

  const { data: workspaces } = useWorkspaces()
  const filteredWorkspaces = (workspaces ?? []).filter((w) => w.name !== '_org')
  const allWorkspaceNames = filteredWorkspaces.map((w) => w.name)
  const effectiveWorkspace = filterWorkspace === '__all__' ? '' : filterWorkspace

  const {
    data: docResults,
    isLoading: docsLoading,
  } = useSearchDocs(
    effectiveWorkspace,
    searchDocs ? submittedQuery : '',
    allWorkspaceNames,
  )

  const {
    data: codeResults,
    isLoading: codeLoading,
  } = useSearchCode(
    effectiveWorkspace,
    searchCode ? submittedQuery : '',
    allWorkspaceNames,
  )

  const isLoading = (searchDocs && docsLoading) || (searchCode && codeLoading)

  // Close on click outside
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  // Focus input when opening
  useEffect(() => {
    if (open) inputRef.current?.focus()
  }, [open])

  // Keyboard shortcut: Cmd/Ctrl+K
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setOpen((prev) => !prev)
      }
      if (e.key === 'Escape' && open) {
        setOpen(false)
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [open])

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (query.trim() && (searchDocs || searchCode)) {
      setSubmittedQuery(query.trim())
    }
  }

  const handleClear = () => {
    setQuery('')
    setSubmittedQuery('')
  }

  const handleNavigateToDoc = (workspace: string, path: string) => {
    setOpen(false)
    navigate('/docs', { state: { workspace, path } })
  }

  const bothActive = searchDocs && searchCode
  const hasDocResults = submittedQuery && searchDocs && docResults && docResults.length > 0
  const hasCodeResults = submittedQuery && searchCode && codeResults && codeResults !== ''
  const noDocsResults = submittedQuery && searchDocs && !docsLoading && (!docResults || docResults.length === 0)
  const noCodeResults = submittedQuery && searchCode && !codeLoading && (!codeResults || codeResults === '')

  return (
    <div className="relative" ref={panelRef}>
      <button
        onClick={() => setOpen(!open)}
        className={cn(
          'flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs transition-colors',
          open
            ? 'bg-accent text-foreground'
            : 'text-muted-foreground hover:text-foreground hover:bg-accent',
        )}
        title="Search (⌘K)"
      >
        <Search className="h-3.5 w-3.5" />
        <span className="hidden sm:inline">Search</span>
      </button>

      {open && (
        <div className="absolute right-0 top-full mt-2 w-[720px] max-h-[80vh] bg-card border rounded-lg shadow-xl z-50 flex flex-col overflow-hidden">
          {/* Search form */}
          <div className="p-3 border-b space-y-2 shrink-0">
            <form onSubmit={handleSearch} className="flex gap-2">
              <div className="relative flex-1">
                <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  ref={inputRef}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Search documentation and code..."
                  className="pl-9 h-9"
                />
              </div>
              <Button type="submit" size="sm" disabled={!query.trim() || (!searchDocs && !searchCode)}>
                Search
              </Button>
              {submittedQuery && (
                <Button type="button" variant="outline" size="sm" onClick={handleClear}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              )}
            </form>
            <div className="flex items-center gap-4">
              <Select value={filterWorkspace} onValueChange={setFilterWorkspace}>
                <SelectTrigger className="w-[180px] h-8 text-xs">
                  <SelectValue placeholder="All Projects" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__all__">All Projects</SelectItem>
                  {filteredWorkspaces.map((w) => (
                    <SelectItem key={w.name} value={w.name}>
                      {w.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <label className="flex items-center gap-1.5 text-xs cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={searchDocs}
                  onChange={() => setSearchDocs(!searchDocs)}
                  className="accent-primary h-3.5 w-3.5"
                />
                <FileText className="h-3 w-3 text-muted-foreground" />
                Documentation
              </label>
              <label className="flex items-center gap-1.5 text-xs cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={searchCode}
                  onChange={() => setSearchCode(!searchCode)}
                  className="accent-primary h-3.5 w-3.5"
                />
                <Code2 className="h-3 w-3 text-muted-foreground" />
                Code
              </label>
            </div>
          </div>

          {/* Results */}
          <div className="flex-1 overflow-y-auto p-3">
            {!submittedQuery && (
              <p className="text-xs text-muted-foreground text-center py-6">
                Type a query and press Search or Enter
              </p>
            )}

            {submittedQuery && isLoading && <LoadingState message="Searching..." />}

            {submittedQuery && !isLoading && (
              <div className={cn(bothActive && (hasDocResults || hasCodeResults) ? 'grid grid-cols-2 gap-3' : '')}>
                {/* Documentation results column */}
                {searchDocs && (
                  <div className={cn(bothActive ? 'border-r pr-3' : '')}>
                    <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2 flex items-center gap-1.5">
                      <FileText className="h-3 w-3" /> Documentation
                      {hasDocResults && (
                        <span className="text-[10px] bg-accent px-1.5 py-0.5 rounded-full">
                          {docResults!.length}
                        </span>
                      )}
                    </h4>
                    {noDocsResults && (
                      <p className="text-xs text-muted-foreground py-3 text-center">
                        No documentation results for &quot;{submittedQuery}&quot;
                      </p>
                    )}
                    {hasDocResults && (
                      <div className="space-y-1">
                        {docResults!.map((result: DocSearchResult, idx: number) => (
                          <button
                            key={`${result.file_path}-${idx}`}
                            onClick={() => {
                              const ws = result.workspace || effectiveWorkspace
                              if (ws) handleNavigateToDoc(ws, result.file_path)
                            }}
                            className="w-full text-left p-2 rounded-md hover:bg-accent transition-colors"
                          >
                            <div className="flex items-center gap-1.5 mb-0.5">
                              <FileText className="h-3 w-3 text-muted-foreground shrink-0" />
                              {result.workspace && !effectiveWorkspace && (
                                <span className="text-[10px] bg-accent px-1 py-0.5 rounded font-medium shrink-0">
                                  {result.workspace}
                                </span>
                              )}
                              <span className="text-xs font-medium truncate">{result.file_path}</span>
                            </div>
                            {result.section_header && (
                              <p className="text-[10px] text-muted-foreground ml-4.5 mb-0.5">
                                {result.section_header}
                              </p>
                            )}
                            <p className="text-[10px] text-muted-foreground line-clamp-2 ml-4.5">
                              {result.snippet}
                            </p>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                )}

                {/* Code results column */}
                {searchCode && (
                  <div>
                    <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2 flex items-center gap-1.5">
                      <Code2 className="h-3 w-3" /> Code
                    </h4>
                    {noCodeResults && (
                      <p className="text-xs text-muted-foreground py-3 text-center">
                        No code results for &quot;{submittedQuery}&quot;
                      </p>
                    )}
                    {hasCodeResults && (
                      <pre className="p-2 rounded-md bg-muted/50 text-[11px] overflow-x-auto whitespace-pre-wrap font-mono leading-relaxed max-h-[50vh]">
                        {codeResults}
                      </pre>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
