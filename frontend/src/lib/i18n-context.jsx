// lib/i18n-context.jsx — shared translation context.
import { createContext, useContext } from 'react'
import { translate } from '../i18n.js'

export const I18nContext = createContext((key, params) => translate('en', key, params))

export const useI18n = () => useContext(I18nContext)
