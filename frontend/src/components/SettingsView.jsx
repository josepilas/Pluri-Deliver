// SettingsView.jsx — macOS-style settings sections with auto-save.
import React, { useState } from 'react'
import { api, browser } from '../api.js'
import { useI18n } from '../lib/i18n-context.jsx'
import { SectionCard, Row, Segmented, Toggle, Select, TextInput, Button } from './ui.jsx'
import { IconBell, IconExternal, IconFolder, IconSparkle } from './icons.jsx'

const INTERVALS = [5, 10, 15, 30, 60, 120, 360]

export default function SettingsView({ settings, onChange, carriers, appInfo }) {
  const t = useI18n()
  const [keyDraft, setKeyDraft] = useState(settings.aggregator.key)
  const [savingKey, setSavingKey] = useState(false)

  const set = (partial) => onChange(partial)

  const commitKey = async () => {
    setSavingKey(true)
    await set({ aggregator: { ...settings.aggregator, key: keyDraft.trim() } })
    setSavingKey(false)
  }

  const aggLink = settings.aggregator.provider === 'aftership'
    ? 'https://admin.aftership.com/'
    : 'https://www.17track.net/zh-cn/apicenter'

  return (
    <div className="view settings">
      <div className="view-head">
        <div className="view-head-text">
          <h1>{t('settings.title')}</h1>
        </div>
      </div>

      <SectionCard title={t('settings.general')}>
        <Row label={t('settings.startup')} hint={t('settings.startup.hint')}>
          <Toggle on={!!settings.startWithWindows} onChange={(v) => set({ startWithWindows: v })} />
        </Row>
        <Row label={t('settings.background')} hint={t('settings.background.hint')}>
          <Toggle on={!!settings.runInBackground} onChange={(v) => set({ runInBackground: v })} />
        </Row>
        <Row label={t('settings.tray')} hint={t('settings.tray.hint')}>
          <Toggle on={!settings.hideTrayIcon} onChange={(v) => set({ hideTrayIcon: !v })} />
        </Row>
      </SectionCard>

      <SectionCard title={t('settings.appearance')}>
        <Row label={t('settings.theme')}>
          <Segmented
            value={settings.theme}
            onChange={(v) => set({ theme: v })}
            options={[
              { value: 'dark', label: t('settings.theme.dark') },
              { value: 'light', label: t('settings.theme.light') },
            ]}
          />
        </Row>
        <Row label={t('settings.language')}>
          <Segmented
            value={settings.language}
            onChange={(v) => set({ language: v })}
            options={[
              { value: 'en', label: 'English' },
              { value: 'pt-BR', label: 'Português' },
            ]}
          />
        </Row>
      </SectionCard>

      <SectionCard title={t('settings.updates')}>
        <Row
          label={t('settings.mode')}
          hint={settings.updateMode === 'smart'
            ? t('settings.mode.smartHint', { a: settings.pollMinutes, i: settings.idlePollMinutes })
            : t('settings.mode.manualHint')}
        >
          <Segmented
            value={settings.updateMode}
            onChange={(v) => set({ updateMode: v })}
            options={[
              { value: 'smart', label: t('settings.mode.smart') },
              { value: 'manual', label: t('settings.mode.manual') },
            ]}
          />
        </Row>
        {settings.updateMode === 'smart' && (
          <>
            <Row label={t('settings.interval')}>
              <Select
                value={String(settings.pollMinutes)}
                onChange={(v) => set({ pollMinutes: parseInt(v, 10) })}
                options={INTERVALS.map((n) => ({ value: String(n), label: t('settings.minutes', { n }) }))}
              />
            </Row>
            <Row label={t('settings.idleInterval')}>
              <Select
                value={String(settings.idlePollMinutes)}
                onChange={(v) => set({ idlePollMinutes: parseInt(v, 10) })}
                options={[60, 120, 240, 480, 1440].map((n) => ({ value: String(n), label: t('settings.minutes', { n }) }))}
              />
            </Row>
          </>
        )}
      </SectionCard>

      <SectionCard title={t('settings.notifications')}>
        <Row label={t('settings.notifications')} hint={t('settings.notifications.hint')}>
          <Toggle on={settings.notifications} onChange={(v) => set({ notifications: v })} />
        </Row>
        <Row label={t('settings.notifications.test')}>
          <Button variant="default" onClick={() => api.testNotification()}>
            <IconBell size={15} />
            <span>{t('settings.notifications.test')}</span>
          </Button>
        </Row>
      </SectionCard>

      <SectionCard title={t('settings.data')}>
        <div className="storage-hint">{t('settings.data.hint')}</div>
        <Row label={t('settings.demo')} hint={t('settings.demo.hint')}>
          <Toggle on={settings.demoMode} onChange={(v) => set({ demoMode: v })} />
        </Row>
        <Row label={t('settings.aggregator')} hint={t('settings.agg.hint')}>
          <Select
            value={settings.aggregator.provider}
            onChange={(v) => set({ aggregator: { ...settings.aggregator, provider: v } })}
            options={[
              { value: 'none', label: t('settings.agg.none') },
              { value: '17track', label: '17TRACK' },
              { value: 'aftership', label: 'AfterShip' },
            ]}
          />
        </Row>
        {settings.aggregator.provider !== 'none' && (
          <Row label={t('settings.agg.key')}>
            <div className="key-row">
              <TextInput
                type="password"
                value={keyDraft}
                onChange={setKeyDraft}
                placeholder={t('settings.agg.keyPlaceholder')}
              />
              <Button variant="default" onClick={commitKey} disabled={savingKey}>
                {savingKey ? '…' : t('action.save')}
              </Button>
            </div>
          </Row>
        )}
        {settings.aggregator.provider !== 'none' && (
          <Row label="">
            <button type="button" className="link-btn" onClick={() => browser.open(aggLink)}>
              <IconExternal size={13} /><span>{t('settings.agg.get')}</span>
            </button>
          </Row>
        )}
      </SectionCard>

      <SectionCard title={t('settings.storage')}>
        <div className="storage-hint">{t('settings.storage.hint')}</div>
        <div className="folder-row">
          <Button variant="default" onClick={() => api.openDataFolder()}>
            <IconFolder size={15} /><span>{t('settings.storage.open')}</span>
          </Button>
          <Button variant="default" onClick={() => api.openCarriersFolder()}>
            <IconFolder size={15} /><span>{t('settings.carriers.open')}</span>
          </Button>
          <Button variant="default" onClick={() => api.openScriptsFolder()}>
            <IconFolder size={15} /><span>{t('settings.scripts.open')}</span>
          </Button>
        </div>
      </SectionCard>

      <SectionCard title={t('settings.about')}>
        <Row label={t('settings.version')}>
          <span className="about-val mono">{appInfo.version || '1.0.0'}</span>
        </Row>
        <Row label={t('app.name')}>
          <span className="about-val"><IconSparkle size={13} />{t('settings.builtWith')}</span>
        </Row>
      </SectionCard>
    </div>
  )
}
