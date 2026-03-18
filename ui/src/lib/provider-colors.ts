/** Provider brand colors keyed by provider `kind`. */
export const providerBrandColors: Record<string, { bg: string; icon: string }> = {
  openai:    { bg: 'bg-emerald-50/70 dark:bg-emerald-950/25', icon: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300' },
  anthropic: { bg: 'bg-orange-50/70 dark:bg-orange-950/25',   icon: 'bg-orange-100 text-orange-700 dark:bg-orange-900/50 dark:text-orange-300' },
  google:    { bg: 'bg-blue-50/70 dark:bg-blue-950/25',       icon: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300' },
  ollama:    { bg: 'bg-slate-50/70 dark:bg-slate-900/25',     icon: 'bg-slate-100 text-slate-700 dark:bg-slate-800/50 dark:text-slate-300' },
  azure:     { bg: 'bg-sky-50/70 dark:bg-sky-950/25',         icon: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300' },
  bedrock:   { bg: 'bg-amber-50/70 dark:bg-amber-950/25',     icon: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300' },
  custom:    { bg: 'bg-teal-50/70 dark:bg-teal-950/25',       icon: 'bg-teal-100 text-teal-700 dark:bg-teal-900/50 dark:text-teal-300' },
}
