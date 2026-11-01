export interface SiteSettings { site_name?: string; site_logo?: string; backend_mode_enabled?: boolean; menu_visibility?: Record<string, boolean> }
declare global {
    interface Window {
        __APP_CONFIG__?: SiteSettings;
        __IKUN_NATIVE_STUDIO_USER__?: string;
    }
}
export const siteName = () => window.__APP_CONFIG__?.site_name?.trim() || "IKUN2api";
export const studioName = () => `${siteName()} · 生图工作室`;
export function siteLogo() {
    const raw = window.__APP_CONFIG__?.site_logo?.trim() || "/logo.svg";
    try {
        const url = new URL(raw, window.location.origin);
        if (url.protocol === "https:" || url.protocol === "http:" || /^data:image\//i.test(raw)) return raw;
    } catch { /* Use the bundled logo for invalid settings. */ }
    return "/logo.svg";
}
