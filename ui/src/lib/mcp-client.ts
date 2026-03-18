let requestId = 0

export class McpError extends Error {
  constructor(
    message: string,
    public code?: number,
  ) {
    super(message)
    this.name = 'McpError'
  }
}

export async function mcpCall<T = unknown>(
  tool: string,
  args: Record<string, unknown> = {},
): Promise<T> {
  const id = ++requestId
  const res = await fetch('/mcp', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      jsonrpc: '2.0',
      id,
      method: 'tools/call',
      params: { name: tool, arguments: args },
    }),
  })

  if (!res.ok) {
    throw new McpError(`HTTP ${res.status}: ${res.statusText}`)
  }

  const rpc = await res.json()

  if (rpc.error) {
    throw new McpError(rpc.error.message, rpc.error.code)
  }

  const result = rpc.result
  if (result?.isError) {
    const text = result.content?.[0]?.text ?? 'Unknown error'
    throw new McpError(text)
  }

  const text = result?.content?.[0]?.text ?? ''
  try {
    return JSON.parse(text) as T
  } catch {
    return text as T
  }
}
