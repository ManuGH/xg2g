// I18n configuration v2
import i18n, { type ResourceLanguage } from 'i18next';
import { initReactI18next } from 'react-i18next';

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

async function loadTranslation(language: SupportedLanguage): Promise<ResourceLanguage> {
  switch (language) {
    case 'de':
      return (await import('./locales/de.json')).default;
    case 'en':
    default:
      return (await import('./locales/en.json')).default;
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

  const translation = await loadTranslation(language);
  i18n.addResourceBundle(language, 'translation', translation, true, true);
}

function syncDocumentLanguage(language: string): void {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = normalizeLanguage(language);
  }
}

const initialLanguage = detectInitialLanguage();

export const i18nReady = (async () => {
  const translation = await loadTranslation(initialLanguage);

  await i18n
    .use(initReactI18next)
    .init({
      resources: {
        [initialLanguage]: { translation },
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
  return i18n;
})();

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
