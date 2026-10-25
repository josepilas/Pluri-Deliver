// CpfModal.jsx — J&T Express Brasil verification step.
// J&T's official API asks for the recipient's CPF on every query (their own
// site does too). The CPF is stored per tracking and reused for every refresh.
import React, { useEffect, useRef, useState } from 'react'
import { api } from '../api.js'
import { useI18n } from '../lib/i18n-context.jsx'
import { Modal, ModalHeader, TextInput, Button } from './ui.jsx'
import { IconUser, IconCheck } from './icons.jsx'

// CPF mask: 123.456.789-01 while typing.
function maskCPF(v) {
  const d = (v || '').replace(/\D/g, '').slice(0, 11)
  if (d.length <= 3) return d
  if (d.length <= 6) return d.replace(/(\d{3})(\d+)/, '$1.$2')
  if (d.length <= 9) return d.replace(/(\d{3})(\d{3})(\d+)/, '$1.$2.$3')
  return d.replace(/(\d{3})(\d{3})(\d{3})(\d+)/, '$1.$2.$3-$4')
}

// Brazilian CPF check digits (same rules the backend enforces).
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

export default function CpfModal({ open, tracking, onClose, onSaved }) {
  const t = useI18n()
  const [cpf, setCpf] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const inputRef = useRef(null)

  useEffect(() => {
    if (open) {
      setDone(false)
      setError('')
      setCpf(tracking && tracking.recipientCPF ? maskCPF(tracking.recipientCPF) : '')
    }
  }, [open, tracking])

  useEffect(() => {
    if (open && inputRef.current) inputRef.current.focus()
  }, [open])

  const submit = async () => {
    if (busy || !tracking) return
    if (!validCPF(cpf)) {
      setError(t('cpf.error.invalid'))
      return
    }
    setBusy(true)
    setError('')
    try {
      const res = await api.setTrackingCPF(tracking.id, cpf)
      const errCode = res && res.errorCode
      if (errCode) {
        setError(t('cpf.error.' + errCode) || errCode)
        return
      }
      setDone(true)
      if (onSaved) onSaved(res && res.tracking)
      setTimeout(() => onClose(), 650)
    } catch (e) {
      setError(String(e && e.message ? e.message : e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose}>
      <ModalHeader title={t('cpf.title')} onClose={onClose} />
      <div className="modal-body cpf-body">
        <div className="captcha-lead">
          <IconUser size={16} />
          <p>{tracking ? t('cpf.subtitle', { code: tracking.code }) : ''}</p>
        </div>

        <label className="field">
          <span className="field-label">{t('cpf.input.label')}</span>
          <TextInput
            mono
            value={cpf}
            onChange={(v) => { setCpf(maskCPF(v)); setError('') }}
            placeholder={t('cpf.input.placeholder')}
            onEnter={submit}
            inputRef={inputRef}
          />
          <span className="field-hint">{t('cpf.input.hint')}</span>
        </label>

        {done && <div className="captcha-ok">{t('cpf.done')}</div>}
        {error && !done && <div className="form-error">{error}</div>}

        <div className="modal-actions">
          <Button variant="ghost" onClick={onClose}>{t('action.cancel')}</Button>
          <Button variant="primary" onClick={submit} disabled={busy || cpf.replace(/\D/g, '').length !== 11}>
            <IconCheck size={15} />
            <span>{busy ? '…' : t('cpf.action')}</span>
          </Button>
        </div>
      </div>
    </Modal>
  )
}
