// Sidebar.jsx — macOS-style translucent navigation.
import React from 'react'
import { IconPackage, IconGear, IconPlus, IconPower, IconSparkle } from './icons.jsx'

export default function Sidebar({ i18n, view, onNavigate, onAdd, onQuit, unseen, demoMode }) {
  return (
    <nav className="sidebar">
      <div className="side-nav">
        <button
          type="button"
          className={'side-item' + (view.name === 'list' || view.name === 'detail' ? ' active' : '')}
          onClick={() => onNavigate({ name: 'list', id: null })}
        >
          <IconPackage size={17} />
          <span>{i18n('nav.trackings')}</span>
          {unseen > 0 && <span className="side-badge">{unseen > 9 ? '9+' : unseen}</span>}
        </button>
        <button
          type="button"
          className={'side-item' + (view.name === 'settings' ? ' active' : '')}
          onClick={() => onNavigate({ name: 'settings', id: null })}
        >
          <IconGear size={17} />
          <span>{i18n('nav.settings')}</span>
        </button>
      </div>

      {demoMode && (
        <div className="side-demo" title={i18n('settings.demo.hint')}>
          <IconSparkle size={14} />
          <span>{i18n('settings.demo')}</span>
        </div>
      )}

      <button type="button" className="side-add" onClick={onAdd}>
        <IconPlus size={16} />
        <span>{i18n('action.add')}</span>
      </button>
      <button type="button" className="side-quit" onClick={onQuit} title={i18n('action.quit')}>
        <IconPower size={14} />
        <span>{i18n('action.quit')}</span>
      </button>
    </nav>
  )
}
