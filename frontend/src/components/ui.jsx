// ui.jsx — Liquid Glass design primitives.
import React, { useEffect } from 'react'
import { IconX } from './icons.jsx'

export function Toggle({ on, onChange, small, xs, title }) {
  return (
    <button
      type="button"
      className={'toggle' + (on ? ' on' : '') + (small ? ' small' : '') + (xs ? ' xs' : '')}
      title={title}
      onClick={() => onChange(!on)}
      role="switch"
      aria-checked={on}
      aria-label={title}
    >
      <span className="toggle-knob" />
    </button>
  )
}

export function Segmented({ options, value, onChange }) {
  return (
    <div className="segmented" role="tablist">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="tab"
          aria-selected={value === o.value}
          className={'seg' + (value === o.value ? ' active' : '')}
          onClick={() => onChange(o.value)}
        >
          {o.icon}
          <span>{o.label}</span>
        </button>
      ))}
    </div>
  )
}

export function StatusBadge({ status, label }) {
  return <span className={'badge st-' + status}>{label}</span>
}

export function Chip({ children, muted, onClick, title }) {
  const Tag = onClick ? 'button' : 'span'
  return (
    <Tag className={'chip' + (muted ? ' muted' : '') + (onClick ? ' clickable' : '')} onClick={onClick} title={title} type={onClick ? 'button' : undefined}>
      {children}
    </Tag>
  )
}

export function Button({ variant = 'default', onClick, children, disabled, title, className = '' }) {
  return (
    <button
      type="button"
      title={title}
      disabled={disabled}
      className={`btn btn-${variant} ${className}`}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

export function TextInput({ value, onChange, placeholder, mono, type = 'text', onEnter, autoFocus, inputRef }) {
  return (
    <input
      ref={inputRef}
      className={'input' + (mono ? ' mono' : '')}
      type={type}
      value={value}
      placeholder={placeholder}
      autoFocus={autoFocus}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => { if (onEnter && e.key === 'Enter') onEnter() }}
      spellCheck={false}
    />
  )
}

export function Select({ value, onChange, options }) {
  return (
    <div className="select-wrap">
      <select className="input select" value={value} onChange={(e) => onChange(e.target.value)}>
        {options.map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  )
}

export function Modal({ open, onClose, children, wide }) {
  useEffect(() => {
    if (!open) return
    const onKey = (e) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
  if (!open) return null
  return (
    <div className="modal-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}>
      <div className={'modal glass' + (wide ? ' wide' : '')} onMouseDown={(e) => e.stopPropagation()}>
        {children}
      </div>
    </div>
  )
}

export function ModalHeader({ title, onClose }) {
  return (
    <div className="modal-header">
      <h2>{title}</h2>
      <button type="button" className="icon-btn" onClick={onClose} title="Close">
        <IconX />
      </button>
    </div>
  )
}

export function SectionCard({ title, children }) {
  return (
    <section className="card section">
      {title && <h3 className="section-title">{title}</h3>}
      {children}
    </section>
  )
}

export function Row({ label, hint, children }) {
  return (
    <div className="row">
      <div className="row-text">
        <div className="row-label">{label}</div>
        {hint && <div className="row-hint">{hint}</div>}
      </div>
      <div className="row-control">{children}</div>
    </div>
  )
}

export function Spinner({ size = 16 }) {
  return <span className="spinner" style={{ width: size, height: size }} />
}
