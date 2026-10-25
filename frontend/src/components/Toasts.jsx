// Toasts.jsx — in-app glass toasts (mirror of system notifications).
import React from 'react'
import { useI18n } from '../lib/i18n-context.jsx'
import { IconPackage, IconBell, IconSparkle, IconX } from './icons.jsx'

const ICONS = {
  update: IconPackage,
  delivered: IconPackage,
  test: IconBell,
  demo: IconSparkle,
}

export default function Toasts({ toasts, onDismiss }) {
  const t = useI18n()
  return (
    <div className="toasts">
      {toasts.map((toast) => {
        const Icon = ICONS[toast.kind] || IconBell
        return (
          <div key={toast.id} className={'toast glass ' + (toast.kind || 'update')} onClick={() => onDismiss(toast.id)}>
            <span className="toast-icon"><Icon size={16} /></span>
            <div className="toast-text">
              <span className="toast-title">{toast.title}</span>
              {toast.body && <span className="toast-body">{toast.body}</span>}
            </div>
            <button type="button" className="toast-x" onClick={(e) => { e.stopPropagation(); onDismiss(toast.id) }}>
              <IconX size={13} />
            </button>
          </div>
        )
      })}
    </div>
  )
}
