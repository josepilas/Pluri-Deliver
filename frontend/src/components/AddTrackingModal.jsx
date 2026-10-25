// AddTrackingModal.jsx — add a tracking with live carrier detection.
import React, { useEffect, useRef, useState } from 'react'
import { api } from '../api.js'
import { useI18n } from '../lib/i18n-context.jsx'
import { Modal, ModalHeader, TextInput, Toggle, Button, Chip } from './ui.jsx'
import { IconSparkle, IconTruck, IconUser } from './icons.jsx'

function maskCPF(v) {
  const d = (v || '').replace(/\D/g, '').slice(0, 11)
  if (d.length <= 3) return d
  if (d.length <= 6) return d.replace(/(\d{3})(\d+)/, '$1.$2')
  if (d.length <= 9) return d.replace(/(\d{3})(\d{3})(\d+)/, '$1.$2.$3')
  return d.replace(/(\d{3})(\d{3})(\d{3})(\d+)/, '$1.$2.$3-$4')
}

function validCPF(v) {
  const c = (v || '').replace(/\D/g, '')
  if (c.length !== 11 || /^(\d)\1{10}$/.test(c)) return false
  const check = (n) => { const r = n % 11; return r < 2 ? 0 : 11 - r }
  let s1 = 0, s2 = 0
  for (let i = 0; i < 9; i++) {
    s1 += Number(c[i]) * (10 - i)
    s2 += Number(c[i]) * (11 - i)
  }
  s2 += Number(c[9]) * 2
  return check(s1) === Number(c[9]) && check(s2) === Number(c[10])
}

export default function AddTrackingModal({ open, onClose, carriers, onAdded }) {
  const t = useI18n()
  const [code, setCode] = useState('')
  const [name, setName] = useState('')
  const [carrier, setCarrier] = useState('')
  const [detected, setDetected] = useState(null)
  const [detecting, setDetecting] = useState(false)
  const [autoUpdate, setAutoUpdate] = useState(true)
  const [cpf, setCpf] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const debounce = useRef(null)

  useEffect(() => {
    if (!open) {
      setCode(''); setName(''); setCarrier(''); setDetected(null); setError(''); setSubmitting(false); setAutoUpdate(true); setCpf('')
    }
  }, [open])

  // live detection (debounced)
  useEffect(() => {
    if (debounce.current) clearTimeout(debounce.current)
    const c = code.trim()
    if (!c) { setDetected(null); setDetecting(false); return }
    setDetecting(true)
    debounce.current = setTimeout(async () => {
      try {
        const found = await api.detectCarriers(c)
        setDetected(found)
        if (found && found.length > 0) {
          setCarrier((prev) => (found.some((f) => f.id === prev) ? prev : found[0].id))
        }
      } catch { setDetected([]) } finally { setDetecting(false) }
    }, 260)
    return () => debounce.current && clearTimeout(debounce.current)
  }, [code])

  const submit = async () => {
    const c = code.trim()
    if (!c || !carrier || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const res = await api.addTracking(c, name.trim(), carrier, autoUpdate, cpf)
      if (res && res.errorCode) {
        setError(t('add.error.' + res.errorCode) || res.errorCode)
        setSubmitting(false)
        return
      }
      onClose()
      if (res && res.tracking) onAdded(res.tracking)
    } catch (e) {
      setError(String(e && e.message ? e.message : e))
      setSubmitting(false)
    }
  }

  const selected = carriers.find((c) => c.id === carrier)
  const isDemoCode = code.trim().toUpperCase().startsWith('DEMO')
  const needsCPF = !!(selected && selected.requiresCpf)
  const cpfDigits = cpf.replace(/\D/g, '')

  return (
    <Modal open={open} onClose={onClose}>
      <ModalHeader title={t('add.title')} onClose={onClose} />
      <div className="modal-body">
        <label className="field">
          <span className="field-label">{t('add.code.label')}</span>
          <div className="field-row">
            <TextInput
              mono
              autoFocus
              value={code}
              onChange={(v) => setCode(v)}
              placeholder={t('add.code.placeholder')}
              onEnter={submit}
            />
            {isDemoCode && <Chip><IconSparkle size={13} />{t('kind.demo')}</Chip>}
          </div>
          <div className="field-hint-row">
            {detecting ? (
              <span className="field-hint detecting">{t('add.detecting')}</span>
            ) : detected && detected.length > 0 ? (
              <div className="detect-row">
                <span className="field-hint">{t('add.detected')}:</span>
                {detected.map((d) => (
                  <button
                    key={d.id}
                    type="button"
                    className={'detect-chip' + (d.id === carrier ? ' sel' : '')}
                    onClick={() => setCarrier(d.id)}
                  >
                    {d.id === 'demo' && <IconSparkle size={12} />}
                    {d.name}
                  </button>
                ))}
              </div>
            ) : (
              <span className="field-hint">{t('add.code.hint')}</span>
            )}
          </div>
        </label>

        <label className="field">
          <span className="field-label">{t('add.name.label')}</span>
          <TextInput value={name} onChange={setName} placeholder={t('add.name.placeholder')} onEnter={submit} />
        </label>

        <label className="field">
          <span className="field-label">{t('add.carrier.label')}</span>
          <div className="select-wrap">
            <select className="input select" value={carrier} onChange={(e) => setCarrier(e.target.value)}>
              <option value="">{t('add.carrier.auto')}</option>
              {carriers.map((c) => (
                <option key={c.id} value={c.id}>{c.name}{c.kind === 'aggregator' ? ' (aggregator)' : ''}</option>
              ))}
            </select>
          </div>
          {selected && selected.requiresKey && (
            <span className="field-hint warn">{t('add.carrier.hint')}</span>
          )}
          {selected && selected.captchaRequired && (
            <span className="field-hint">{t('add.carrier.captchaHint')}</span>
          )}
        </label>

        {needsCPF && (
          <label className="field">
            <span className="field-label">
              <IconUser size={12} /> {t('add.cpf.label')}
            </span>
            <TextInput
              mono
              value={cpf}
              onChange={(v) => { setCpf(maskCPF(v)); setError('') }}
              placeholder={t('add.cpf.placeholder')}
              onEnter={submit}
            />
            <span className="field-hint">{t('add.cpf.hint')}</span>
            {cpfDigits.length === 11 && !validCPF(cpf) && (
              <span className="field-hint warn">{t('cpf.error.invalid')}</span>
            )}
          </label>
        )}

        <div className="field switch-field">
          <div className="switch-text">
            <span className="field-label">{t('add.autoUpdate')}</span>
            <span className="field-hint">{t('add.autoUpdate.hint')}</span>
          </div>
          <Toggle on={autoUpdate} onChange={setAutoUpdate} />
        </div>

        {error && <div className="form-error">{error}</div>}

        <div className="modal-actions">
          <Button variant="ghost" onClick={onClose}>{t('action.cancel')}</Button>
          <Button variant="primary" onClick={submit} disabled={!code.trim() || !carrier || submitting || (needsCPF && cpfDigits.length > 0 && !validCPF(cpf))}>
            <IconTruck size={15} />
            <span>{submitting ? '…' : t('action.add')}</span>
          </Button>
        </div>
      </div>
    </Modal>
  )
}
