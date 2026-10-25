// RenameModal.jsx — inline rename dialog.
import React, { useEffect, useState } from 'react'
import { useI18n } from '../lib/i18n-context.jsx'
import { Modal, ModalHeader, TextInput, Button } from './ui.jsx'
import { IconPencil } from './icons.jsx'

export default function RenameModal({ open, tracking, onClose, onConfirm }) {
  const t = useI18n()
  const [name, setName] = useState('')
  useEffect(() => { if (tracking) setName(tracking.name || '') }, [tracking, open])
  const submit = () => { if (name.trim() || tracking) onConfirm(name.trim()) }
  return (
    <Modal open={open} onClose={onClose}>
      <ModalHeader title={t('action.rename')} onClose={onClose} />
      <div className="modal-body">
        <label className="field">
          <span className="field-label">{t('add.name.label')}</span>
          <TextInput autoFocus value={name} onChange={setName} onEnter={submit} placeholder={t('add.name.placeholder')} />
        </label>
        <div className="modal-actions">
          <Button variant="ghost" onClick={onClose}>{t('action.cancel')}</Button>
          <Button variant="primary" onClick={submit}>
            <IconPencil size={14} /><span>{t('action.save')}</span>
          </Button>
        </div>
      </div>
    </Modal>
  )
}
