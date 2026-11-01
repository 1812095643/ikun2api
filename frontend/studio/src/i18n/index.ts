import { siteName } from "@/site-runtime";
import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import enUS from "@/i18n/locales/en-US";
import zhCN from "@/i18n/locales/zh-CN";

export type AppLocale = "zh-CN" | "en-US";

const LOCALE_STORAGE_KEY = "infinite-canvas:locale";

function brand<T>(value: T): T {
    if (typeof value === "string") return value.replace(/IKUN生图工作室/g, `${siteName()} · 生图工作室`).replace(/IKUN Image Studio/g, `${siteName()} · Image Studio`).replace(/IKUN Creative Studio/g, `${siteName()} · Creative Studio`) as T;
    if (Array.isArray(value)) return value.map(brand) as T;
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, brand(item)])) as T;
    return value;
}

i18n.use(initReactI18next).init({
    resources: {
        "zh-CN": { translation: brand(zhCN) },
        "en-US": { translation: brand(enUS) },
    },
    lng: (localStorage.getItem(LOCALE_STORAGE_KEY) as AppLocale) || "zh-CN",
    fallbackLng: "zh-CN",
    supportedLngs: ["zh-CN", "en-US"],
    initAsync: false,
    interpolation: { escapeValue: false },
    react: { useSuspense: false },
});

export function changeAppLocale(locale: AppLocale) {
    localStorage.setItem(LOCALE_STORAGE_KEY, locale);
    return i18n.changeLanguage(locale);
}

export default i18n;
