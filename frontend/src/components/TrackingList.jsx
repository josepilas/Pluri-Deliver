// TrackingList.jsx — searchable list of glass tracking cards.
import React, { useMemo, useState } from 'react'
import { latestEvent, relTime, trackingStatus, unseenCount } from '../lib/utils.js'
import { useI18n } from '../lib/i18n-context.jsx'
import { StatusBadge, Toggle, Chip, Button } from './ui.jsx'
import { IconSearch, IconPlus, IconRefresh, IconPackage, IconPin, IconAlert, IconShield, IconUser } from './icons.jsx'

export default function TrackingList({ trackings, carriers, onOpen, onAdd, onRefreshAll, refreshing, onToggleAuto, onVerify, onVerifyOne, onCpfOne }) {
  const t = useI18n()
  const [query, setQuery] = useState('')

  const carrierName = (id) => (carriers.find((c) => c.id === id) || { name: id }).name
  const isCaptchaCarrier = (id) => {
    const c = carriers.find((x) => x.id === id)
    return !!(c && c.captchaRequired)
  }
  const captchaCount = trackings.filter((tr) => isCaptchaCarrier(tr.carrier)).length

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return trackings
    return trackings.filter(
      (tr) => tr.name.toLowerCase().includes(q) || tr.code.toLowerCase().includes(q),
    )
  }, [trackings, query])

  return (
    <div className="view">
      <div className="view-head">
        <div className="view-head-text">
          <h1>{t('list.title')}</h1>
          <p className="view-sub">{t('list.count', { n: trackings.length })}</p>
        </div>
        <div className="view-actions">
          {captchaCount > 0 && (
            <button type="button" className="verify-chip" onClick={onVerify} title={t('captcha.list.hint')}>
              <IconShield size={13} />
              <span>{t('captcha.list.action', { n: captchaCount })}</span>
            </button>
          )}
          <button type="button" className="icon-btn big" onClick={onRefreshAll} title={t('action.refreshAll')} disabled={refreshing}>
            <IconRefresh size={16} className={refreshing ? 'spin' : ''} />
          </button>
          <Button variant="primary" onClick={onAdd}>
            <IconPlus size={15} />
            <span>{t('action.add')}</span>
          </Button>
        </div>
      </div>

      <div className="search glass">
        <IconSearch size={15} />
        <input
          className="search-input"
          placeholder={t('search.placeholder')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          spellCheck={false}
        />
      </div>

      {trackings.length === 0 ? (
        <div className="empty glass">
          <div className="empty-icon"><IconPackage size={26} /></div>
          <h2>{t('list.empty.title')}</h2>
          <p>{t('list.empty.subtitle')}</p>
          <Button variant="primary" onClick={onAdd} className="mt">
            <IconPlus size={15} /><span>{t('action.add')}</span>
          </Button>
        </div>
      ) : filtered.length === 0 ? (
        <div className="empty glass">
          <h2>{t('list.noResults')}</h2>
        </div>
      ) : (
        <div className="track-list">
          {filtered.map((tr) => {
            const ev = latestEvent(tr)
            const st = trackingStatus(tr)
            const unseen = unseenCount(tr)
            return (
              <article key={tr.id} className="track-card glass" onClick={() => onOpen(tr.id)}>
                <span className={'st-dot ' + st} />
                <div className="track-main">
                  <div className="track-title-row">
                    <span className="track-name" title={tr.name || tr.code}>{tr.name || tr.code}</span>
                    {tr.needsCaptcha && (
                      <button type="button" className="verify-pill" onClick={(e) => { e.stopPropagation(); onVerifyOne(tr.id) }}>
                        <IconShield size={11} />{t('captcha.badge')}
                      </button>
                    )}
                    {tr.needsCPF && (
                      <button type="button" className="verify-pill" onClick={(e) => { e.stopPropagation(); onCpfOne(tr.id) }}>
                        <IconUser size={11} />{t('cpf.badge')}
                      </button>
                    )}
                    {unseen > 0 && <span className="new-pill">{t('list.newBadge', { n: unseen })}</span>}
                  </div>
                  <div className="track-meta-row">
                    <span className="track-code mono" title={tr.code}>{tr.code}</span>
                    <Chip muted>{carrierName(tr.carrier)}</Chip>
                  </div>
                </div>
                <div className="track-latest">
                  {tr.lastError ? (
                    <span className="track-error" title={tr.lastError}>
                      <IconAlert size={12} />{t('detail.error')}
                    </span>
                  ) : ev ? (
                    <>
                      <span className="track-latest-desc">
                        {ev.location && <IconPin size={12} />}{ev.description}
                      </span>
                      <span className="track-latest-time">
                        {tr.eta && st === 'in_transit' ? 'ETA ' + tr.eta + ' · ' : ''}{relTime(ev.date, t)}
                      </span>
                    </>
                  ) : (
                    <span className="track-latest-desc muted">{t('detail.noEvents')}</span>
                  )}
                </div>
                <div className="track-side">
                  <StatusBadge status={st} label={t('status.' + st)} />
                  <Toggle
                    xs
                    on={tr.autoUpdate}
                    title={t(tr.autoUpdate ? 'list.autoOn' : 'list.autoOff')}
                    onChange={(v) => onToggleAuto(tr.id, v)}
                  />
                </div>
              </article>
            )
          })}
        </div>
      )}
    </div>
  )
}
