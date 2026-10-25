// TrackingDetail.jsx — parcel detail: header, progress, event timeline.
import React, { useState } from 'react'
import { fmtDateTime, relTime, unseenCount } from '../lib/utils.js'
import { useI18n } from '../lib/i18n-context.jsx'
import { StatusBadge, Button, Chip, Toggle } from './ui.jsx'
import {
  IconBack, IconRefresh, IconTrash, IconPencil, IconCopy, IconCheck,
  IconPin, IconClock, IconAlert, IconShield, IconUser,
} from './icons.jsx'

const STAGES = ['pending', 'in_transit', 'out_for_delivery', 'delivered']

export default function TrackingDetail({ tracking, carriers, onBack, onRefresh, onRename, onDelete, onToggleAuto, onSetCPF }) {
  const t = useI18n()
  const [copied, setCopied] = useState(false)
  const [busy, setBusy] = useState(false)

  if (!tracking) {
    return (
      <div className="view">
        <button type="button" className="btn btn-ghost" onClick={onBack}><IconBack />{t('action.back')}</button>
      </div>
    )
  }

  const carrier = carriers.find((c) => c.id === tracking.carrier)
  const isCaptchaCarrier = !!(carrier && carrier.captchaRequired)
  const isCPFCarrier = !!(carrier && carrier.requiresCpf)
  const events = [...(tracking.events || [])].reverse() // newest first
  const unseen = unseenCount(tracking)
  const st = tracking.events && tracking.events.length
    ? tracking.events[tracking.events.length - 1].status
    : 'pending'
  const stageIdx = STAGES.indexOf(st) === -1 && st !== 'info' ? 0 : STAGES.indexOf(st)
  const lastSeenAt = tracking.lastSeen ? new Date(tracking.lastSeen).getTime() : 0

  const copyCode = async () => {
    try { await navigator.clipboard.writeText(tracking.code) } catch { }
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  const refresh = async () => {
    setBusy(true)
    try { await onRefresh(tracking.id) } finally { setBusy(false) }
  }

  return (
    <div className="view">
      <button type="button" className="btn btn-ghost back-btn" onClick={onBack}>
        <IconBack size={15} /><span>{t('action.back')}</span>
      </button>

      <div className="detail-head glass">
        <div className="detail-title-row">
          <h1 className="detail-name">{tracking.name || t('detail.unnamed')}</h1>
          <div className="detail-actions">
            <button type="button" className="icon-btn" onClick={onRename} title={t('action.rename')}><IconPencil /></button>
            <button type="button" className="icon-btn danger" onClick={onDelete} title={t('action.delete')}><IconTrash /></button>
          </div>
        </div>
        <div className="detail-meta-row">
          <button type="button" className="code-chip mono" onClick={copyCode} title={t('action.copy')}>
            {copied ? <IconCheck size={13} /> : <IconCopy />}
            {tracking.code}
          </button>
          <Chip muted>{carrier ? carrier.name : tracking.carrier}</Chip>
          <StatusBadge status={st} label={t('status.' + st)} />
        </div>

        <div className="detail-progress">
          <div className="progress-track">
            {STAGES.map((s, i) => (
              <div key={s} className={'progress-seg' + (st === 'exception' ? ' exc' : i <= stageIdx ? ' done' : '')} />
            ))}
          </div>
          <div className="progress-labels">
            {STAGES.map((s, i) => (
              <span key={s} className={'progress-label' + (i <= stageIdx ? ' done' : '')}>{t('status.' + s)}</span>
            ))}
          </div>
        </div>

        <div className="detail-meta-row subtle">
          <span className="meta-item">
            <IconClock size={13} />
            {tracking.lastCheck ? t('detail.lastCheck', { t: relTime(tracking.lastCheck, t) }) : t('detail.never')}
          </span>
          <span className="meta-item">
            <Toggle small on={tracking.autoUpdate} onChange={(v) => onToggleAuto(tracking.id, v)} />
            {t(tracking.autoUpdate ? 'list.autoOn' : 'list.autoOff')}
          </span>
          <span className="meta-item events-count">{t('detail.events', { n: events.length })}</span>
          {tracking.eta && (
            <span className="meta-item eta-item">
              <IconPin size={13} />
              {t('detail.eta')} {tracking.eta}
            </span>
          )}
          {tracking.category && <span className="meta-item">{tracking.category}</span>}
        </div>

        {!tracking.lastError && tracking.needsCaptcha && (
          <div className="detail-hint glass">
            <IconShield size={15} />
            <div>
              <strong>{t('captcha.badge')}</strong>
              <span>{t('captcha.detail.hint')}</span>
            </div>
          </div>
        )}

        {!tracking.lastError && tracking.needsCPF && (
          <div className="detail-hint glass">
            <IconUser size={15} />
            <div>
              <strong>{t('cpf.badge')}</strong>
              <span>{t('cpf.detail.hint')}</span>
            </div>
          </div>
        )}

        {tracking.lastError && (
          <div className="detail-error glass">
            <IconAlert size={15} />
            <div>
              <strong>{t('detail.error')}</strong>
              <span>{tracking.lastError}</span>
            </div>
          </div>
        )}

        <div className="detail-refresh-row">
          <Button variant="default" onClick={refresh} disabled={busy}>
            <IconRefresh size={14} className={busy ? 'spin' : ''} />
            <span>{t('action.refresh')}</span>
          </Button>
          {isCaptchaCarrier && (
            <span className="refresh-hint" title={t('captcha.detail.hint')}>
              <IconShield size={12} />{t('captcha.refresh.hint')}
            </span>
          )}
          {isCPFCarrier && (
            <button type="button" className="cpf-link" onClick={onSetCPF} title={t('cpf.change.tooltip')}>
              <IconUser size={12} />
              {tracking.recipientCPF
                ? t('cpf.change.saved')
                : t('cpf.change.set')}
            </button>
          )}
        </div>
      </div>

      <h2 className="timeline-title">{t('detail.timeline')}</h2>

      {events.length === 0 ? (
        <div className="empty glass"><p>{t('detail.noEvents')}</p></div>
      ) : (
        <ol className="timeline">
          {events.map((ev, i) => {
            const isNew = lastSeenAt && new Date(ev.date).getTime() > lastSeenAt
            return (
              <li key={ev.id || i} className={'tl-item ' + ev.status + (i === 0 ? ' latest' : '')}>
                <span className="tl-dot" />
                <div className="tl-body">
                  <div className="tl-top">
                    <span className="tl-desc">{ev.description}</span>
                    {isNew && <span className="new-pill">NEW</span>}
                  </div>
                  <div className="tl-meta">
                    <span className="tl-date">{fmtDateTime(ev.date)}</span>
                    {ev.location && (
                      <span className="tl-loc"><IconPin size={12} />{ev.location}</span>
                    )}
                  </div>
                </div>
              </li>
            )
          })}
        </ol>
      )}
    </div>
  )
}
