export type ToastVariant = 'default' | 'destructive'

export interface Toast {
  id: string
  title?: string
  description?: string
  variant?: ToastVariant
  duration?: number
}

export interface ToastInput {
  title?: string
  description?: string
  variant?: ToastVariant
  duration?: number
}

type Listener = (toasts: Toast[]) => void

let state: Toast[] = []
const listeners = new Set<Listener>()

function notify() {
  listeners.forEach((l) => l([...state]))
}

export function toast(input: ToastInput) {
  const id = Math.random().toString(36).slice(2)
  const duration = input.duration ?? 4000
  state = [...state, { id, ...input }]
  notify()
  setTimeout(() => {
    state = state.filter((t) => t.id !== id)
    notify()
  }, duration)
}

export function dismissToast(id: string) {
  state = state.filter((t) => t.id !== id)
  notify()
}

export function subscribeToasts(listener: Listener): () => void {
  listeners.add(listener)
  listener([...state])
  return () => listeners.delete(listener)
}
