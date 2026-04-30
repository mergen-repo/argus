import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

import enCommon from '@/locales/en/common.json'
import enForms from '@/locales/en/forms.json'
import enErrors from '@/locales/en/errors.json'
import enEmptyStates from '@/locales/en/emptyStates.json'

import trCommon from '@/locales/tr/common.json'
import trForms from '@/locales/tr/forms.json'
import trErrors from '@/locales/tr/errors.json'
import trEmptyStates from '@/locales/tr/emptyStates.json'

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: {
        common: enCommon,
        forms: enForms,
        errors: enErrors,
        emptyStates: enEmptyStates,
      },
      tr: {
        common: trCommon,
        forms: trForms,
        errors: trErrors,
        emptyStates: trEmptyStates,
      },
    },
    fallbackLng: 'en',
    defaultNS: 'common',
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'argus:locale',
    },
    interpolation: {
      escapeValue: false,
    },
  })

export default i18n
