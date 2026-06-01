// I18n configuration v2
import i18n, { type ResourceLanguage } from 'i18next';
import { initReactI18next } from 'react-i18next';
// Locales are bundled STATICALLY (not dynamic import()). On iOS Safari the
// dynamic import() of a locale chunk can stall indefinitely; when the initial
// language bundle was therefore absent at first render, react-i18next treated
// the namespace as not-ready and the tree never committed (black screen).
// Bundling the (small) JSON eagerly removes that failure mode entirely.
import deTranslation from './locales/de.json';
import enTranslation from './locales/en.json';

const SUPPORTED_LANGUAGES = ['de', 'en'] as const;
type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

const DEFAULT_LANGUAGE: SupportedLanguage = 'en';
const STORAGE_KEY = 'xg2g_lang';

function normalizeLanguage(value?: string | null): SupportedLanguage {
  const normalized = (value ?? '').trim().toLowerCase();
  if (!normalized) {
    return DEFAULT_LANGUAGE;
  }

  const base = normalized.split(/[-_]/, 1)[0] as SupportedLanguage;
  return SUPPORTED_LANGUAGES.includes(base) ? base : DEFAULT_LANGUAGE;
}

function loadTranslation(language: SupportedLanguage): ResourceLanguage {
  switch (language) {
    case 'de':
      return deTranslation as ResourceLanguage;
    case 'en':
    default:
      return enTranslation as ResourceLanguage;
  }
}

function detectInitialLanguage(): SupportedLanguage {
  if (typeof window === 'undefined') {
    return DEFAULT_LANGUAGE;
  }

  try {
    const storedLanguage = window.localStorage.getItem(STORAGE_KEY);
    if (storedLanguage) {
      return normalizeLanguage(storedLanguage);
    }
  } catch {
    // Ignore storage access issues and fall back to browser detection.
  }

  return normalizeLanguage(window.navigator.languages?.[0] ?? window.navigator.language);
}

async function ensureLanguageLoaded(language: SupportedLanguage): Promise<void> {
  if (i18n.hasResourceBundle(language, 'translation')) {
    return;
  }

  const translation = loadTranslation(language);
  i18n.addResourceBundle(language, 'translation', translation, true, true);
}

function syncDocumentLanguage(language: string): void {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = normalizeLanguage(language);
  }
}

const initialLanguage = detectInitialLanguage();

// Initialise i18next SYNCHRONOUSLY at module load with ALL language bundles
// present (they are imported statically above). The tree can mount immediately,
// useTranslation is always "ready" (no missing-namespace suspend), and there is
// no dynamic import that could stall on iOS Safari.
void i18n.use(initReactI18next).init({
  resources: {
    de: { translation: deTranslation as ResourceLanguage },
    en: { translation: enTranslation as ResourceLanguage },
  },
  lng: initialLanguage,
  fallbackLng: false,
  supportedLngs: SUPPORTED_LANGUAGES,
  nonExplicitSupportedLngs: true,
  load: 'languageOnly',
  interpolation: {
    escapeValue: false,
  },
  react: {
    useSuspense: false,
  },
});
syncDocumentLanguage(i18n.language);
i18n.on('languageChanged', syncDocumentLanguage);

// Locales are bundled statically, so i18n is fully ready synchronously.
export const i18nReady = Promise.resolve(i18n);

export async function setLanguage(language: string): Promise<void> {
  const normalized = normalizeLanguage(language);
  await i18nReady;
  await ensureLanguageLoaded(normalized);
  await i18n.changeLanguage(normalized);

  if (typeof window !== 'undefined') {
    try {
      window.localStorage.setItem(STORAGE_KEY, normalized);
    } catch {
      // Ignore storage write failures and keep the in-memory language change.
    }
  }
}

export default i18n;
