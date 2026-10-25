// ConfirmModal.jsx — destructive action confirmation.
import React from 'react'
import { useI18n } from '../lib/i18n-context.jsx'
import { Modal, ModalHeader, Button } from './ui.jsx'
import { IconTrash } from './icons.jsx'

export default function ConfirmModal({ open, tracking, onClose, onConfirm }) {
  const t = useI18n()
  return (
    <Modal open={open} onClose={onClose}>
      <ModalHeader title={t('action.confirmDelete')} onClose={onClose} />
      <div className="modal-body">
        <p className="confirm-text">
          <strong>{tracking ? (tracking.name || tracking.code) : ''}</strong>
          <br />
          {t('action.confirmDeleteHint')}
        </p>
        <div className="modal-actions">
          <Button variant="ghost" onClick={onClose}>{t('action.cancel')}</Button>
          <Button variant="danger" onClick={onConfirm}>
            <IconTrash size={14} />
            <span>{t('action.delete')}</span>
          </Button>
        </div>
      </div>
    </Modal>
  )
}
