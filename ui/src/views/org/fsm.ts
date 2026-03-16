export type FsmState =
  | 'active'
  | 'onboarding'
  | 'suspended'
  | 'error'
  | 'halted'
  | 'draining'
  | 'decommissioned'
  | 'idle'
  | 'unknown'

export function getFsmState(status: string): FsmState {
  const s = status.toLowerCase()
  if (s === 'active' || s === 'running') return 'active'
  if (s === 'onboarding') return 'onboarding'
  if (s === 'suspended' || s === 'waiting') return 'suspended'
  if (s === 'error' || s === 'failed' || s === 'crashed') return 'error'
  if (s === 'halted') return 'halted'
  if (s === 'draining') return 'draining'
  if (s === 'decommissioned') return 'decommissioned'
  if (s === 'idle') return 'idle'
  return 'unknown'
}

export const fsmStyles: Record<
  FsmState,
  { border: string; bg: string; badge: string; dot: string; ring: string; pulse: boolean }
> = {
  active: {
    border: 'border-l-green-500',
    bg: 'bg-green-50 dark:bg-green-950/30',
    badge: 'bg-green-100 text-green-800 dark:bg-green-900/50 dark:text-green-300',
    dot: 'bg-green-500',
    ring: 'ring-green-400',
    pulse: false,
  },
  onboarding: {
    border: 'border-l-blue-500',
    bg: 'bg-blue-50 dark:bg-blue-950/30',
    badge: 'bg-blue-100 text-blue-800 dark:bg-blue-900/50 dark:text-blue-300',
    dot: 'bg-blue-500',
    ring: 'ring-blue-400',
    pulse: true,
  },
  suspended: {
    border: 'border-l-amber-500',
    bg: 'bg-amber-50 dark:bg-amber-950/30',
    badge: 'bg-amber-100 text-amber-800 dark:bg-amber-900/50 dark:text-amber-300',
    dot: 'bg-amber-500',
    ring: 'ring-amber-400',
    pulse: false,
  },
  error: {
    border: 'border-l-red-500',
    bg: 'bg-red-50 dark:bg-red-950/30',
    badge: 'bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-300',
    dot: 'bg-red-500',
    ring: 'ring-red-500',
    pulse: false,
  },
  halted: {
    border: 'border-l-red-700',
    bg: 'bg-red-100 dark:bg-red-950/40',
    badge: 'bg-red-200 text-red-900 dark:bg-red-900 dark:text-red-200',
    dot: 'bg-red-700',
    ring: 'ring-red-700',
    pulse: false,
  },
  draining: {
    border: 'border-l-gray-400',
    bg: 'bg-gray-50 dark:bg-gray-900/30',
    badge: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
    dot: 'bg-gray-400',
    ring: 'ring-gray-400',
    pulse: false,
  },
  decommissioned: {
    border: 'border-l-gray-300',
    bg: 'bg-gray-50/50 dark:bg-gray-900/20',
    badge: 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-500',
    dot: 'bg-gray-300',
    ring: 'ring-gray-300',
    pulse: false,
  },
  idle: {
    border: 'border-l-yellow-400',
    bg: 'bg-yellow-50 dark:bg-yellow-950/30',
    badge: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/50 dark:text-yellow-300',
    dot: 'bg-yellow-400',
    ring: 'ring-yellow-400',
    pulse: false,
  },
  unknown: {
    border: 'border-l-gray-400',
    bg: 'bg-gray-50 dark:bg-gray-900/30',
    badge: 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300',
    dot: 'bg-gray-400',
    ring: 'ring-gray-400',
    pulse: false,
  },
}
