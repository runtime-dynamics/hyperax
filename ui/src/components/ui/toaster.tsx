import { useEffect, useState } from 'react'
import {
  ToastProvider,
  ToastViewport,
  Toast,
  ToastTitle,
  ToastDescription,
  ToastClose,
} from './toast'
import { type Toast as ToastType, subscribeToasts, dismissToast } from './use-toast'

export function Toaster() {
  const [toasts, setToasts] = useState<ToastType[]>([])

  useEffect(() => {
    return subscribeToasts(setToasts)
  }, [])

  return (
    <ToastProvider>
      {toasts.map((t) => (
        <Toast key={t.id} variant={t.variant} onOpenChange={(open) => { if (!open) dismissToast(t.id) }}>
          <div className="grid gap-1">
            {t.title && <ToastTitle>{t.title}</ToastTitle>}
            {t.description && <ToastDescription>{t.description}</ToastDescription>}
          </div>
          <ToastClose />
        </Toast>
      ))}
      <ToastViewport />
    </ToastProvider>
  )
}
