// Titlebar.jsx — slim glass window header with drag region + window controls.
import React from 'react'
import { isDesktop, windowControls } from '../api.js'
import { IconLogo, IconMinus, IconSquare, IconX } from './icons.jsx'

export default function Titlebar({ i18n }) {
  const onDoubleClick = (e) => {
    if (!isDesktop) return
    if (e.target.closest('.win-btn')) return
    windowControls.toggleMaximize()
  }
  return (
    <header className="titlebar" style={{ '--wails-draggable': 'drag' }} onDoubleClick={onDoubleClick}>
      <div className="titlebar-brand">
        <span className="brand-mark"><IconLogo size={16} /></span>
        <span className="brand-name">Pluri-Deliver</span>
      </div>
      <div className="titlebar-spacer" />
      <div className="titlebar-actions">
        {isDesktop && (
          <>
            <button type="button" className="win-btn" style={{ '--wails-draggable': 'no-drag' }}
              onClick={() => windowControls.minimize()} title={i18n('win.min')} aria-label={i18n('win.min')}>
              <IconMinus size={13} />
            </button>
            <button type="button" className="win-btn" style={{ '--wails-draggable': 'no-drag' }}
              onClick={() => windowControls.toggleMaximize()} title={i18n('win.max')} aria-label={i18n('win.max')}>
              <IconSquare size={11} />
            </button>
            <button type="button" className="win-btn close" style={{ '--wails-draggable': 'no-drag' }}
              onClick={() => windowControls.close()} title={i18n('win.close')} aria-label={i18n('win.close')}>
              <IconX size={14} />
            </button>
          </>
        )}
      </div>
    </header>
  )
}
