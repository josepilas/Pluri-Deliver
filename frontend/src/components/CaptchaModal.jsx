// CaptchaModal.jsx — Correios verification step.
// Correios' portal asks for a captcha on every query; one typed answer covers
// a single full timeline (verify) or a batch refresh of up to 20 codes (sync).
import React, { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api.js'
import { useI18n } from '../lib/i18n-context.jsx'
import { Modal, ModalHeader, TextInput, Button } from './ui.jsx'
import { IconShield, IconRefresh } from './icons.jsx'

export default function CaptchaModal({ open, mode, tracking, onClose, onVerified }) {
  // mode: 'verify' (one tracking, full timeline) | 'sync' (all Correios codes)
  const t = useI18n()
  const [image, setImage] = useState('')
  const [loading, setLoading] = useState(false)
  const [answer, setAnswer] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const inputRef = useRef(null)

  const loadCaptcha = useCallback(async () => {
    setLoading(true)
    setError('')
    setAnswer('')
    try {
      const res = await api.getCorreiosCaptcha()
      if (res && res.image) {
        setImage(res.image)
      } else {
        setImage('')
        setError(t('captcha.error.network'))
      }
    } catch {
      setImage('')
      setError(t('captcha.error.network'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    if (open) {
      setDone(false)
      setError('')
      setAnswer('')
      loadCaptcha()
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (open && image && inputRef.current) {
      inputRef.current.focus()
    }
  }, [open, image])

  const submit = async () => {
    const a = answer.trim()
    if (!a || busy) return
    setBusy(true)
    setError('')
    try {
      const res = mode === 'sync'
        ? await api.syncCorreios(a)
        : await api.verifyCorreiosCaptcha(tracking.id, a)
      const errCode = res && res.errorCode
      if (errCode === 'captcha_invalid') {
        setError(t('captcha.error.invalid'))
        await loadCaptcha()
        return
      }
      if (errCode) {
        setError(t('captcha.error.' + errCode) || errCode)
        await loadCaptcha()
        return
      }
      setDone(true)
      if (onVerified) onVerified(res, mode)
      setTimeout(() => onClose(), 650)
    } catch (e) {
      setError(String(e && e.message ? e.message : e))
      await loadCaptcha()
    } finally {
      setBusy(false)
    }
  }

  const title = mode === 'sync' ? t('captcha.title.sync') : t('captcha.title.verify')
  const subtitle = mode === 'sync'
    ? t('captcha.subtitle.sync')
    : (tracking
      ? t('captcha.subtitle.verify', { code: tracking.code })
      : t('captcha.subtitle.sync'))

  return (
    <Modal open={open} onClose={onClose}>
      <ModalHeader title={title} onClose={onClose} />
      <div className="modal-body captcha-body">
        <div className="captcha-lead">
          <IconShield size={16} />
          <p>{subtitle}</p>
        </div>

        <div className="captcha-frame glass">
          {loading ? (
            <div className="captcha-loading"><span className="spin-dots" /></div>
          ) : image ? (
            <img className="captcha-img" src={image} alt="captcha" draggable={false} />
          ) : (
            <div className="captcha-loading">{t('captcha.error.network')}</div>
          )}
          <button
            type="button"
            className="captcha-reload icon-btn"
            onClick={loadCaptcha}
            title={t('captcha.reload')}
            disabled={loading}
          >
            <IconRefresh size={14} />
          </button>
        </div>

        <label className="field">
          <span className="field-label">{t('captcha.input.label')}</span>
          <TextInput
            mono
            value={answer}
            onChange={(v) => setAnswer(v)}
            placeholder={t('captcha.input.placeholder')}
            onEnter={submit}
          />
        </label>

        {done && <div className="captcha-ok">{t('captcha.done')}</div>}
        {error && !done && <div className="form-error">{error}</div>}

        <div className="modal-actions">
          <Button variant="ghost" onClick={onClose}>{t('action.cancel')}</Button>
          <Button variant="primary" onClick={submit} disabled={!answer.trim() || busy || loading}>
            <IconShield size={15} />
            <span>{busy ? '…' : t('captcha.action')}</span>
          </Button>
        </div>
      </div>
    </Modal>
  )
}
