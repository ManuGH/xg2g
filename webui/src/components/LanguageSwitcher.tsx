import { useTranslation } from "react-i18next";

const SUPPORTED = [
  { code: "en", labelKey: "common.english" },
  { code: "de", labelKey: "common.german" }
] as const;

function normalizeLanguage(lang: string): "en" | "de" {
  const base = lang.split("-")[0];
  return base === "de" ? "de" : "en";
}

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  const current = normalizeLanguage(i18n.resolvedLanguage || i18n.language || "en");

  return (
    <label style={{ display: "inline-flex", alignItems: "center", gap: 8, cursor: "pointer" }}>
      <span>{t("common.language")}:</span>
      <select
        aria-label={t("common.language")}
        value={current}
        onChange={(e) => void i18n.changeLanguage(normalizeLanguage(e.target.value))}
        style={{
          padding: "4px 8px",
          borderRadius: "4px",
          background: "rgba(255, 255, 255, 0.1)",
          border: "1px solid rgba(255, 255, 255, 0.2)",
          color: "inherit",
          cursor: "pointer"
        }}
      >
        {SUPPORTED.map((l) => (
          <option key={l.code} value={l.code}>
            {t(l.labelKey)}
          </option>
        ))}
      </select>
    </label>
  );
}
