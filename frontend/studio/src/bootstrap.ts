import { studioName, type SiteSettings } from "./site-runtime";

// Resolve the current installation and authenticated owner before importing stores.
// Credentials stay on this origin and never appear in the navigation URL.
async function bootstrap() {
    if (!window.__APP_CONFIG__) {
        const response = await fetch("/api/v1/settings/public", { cache: "no-store" });
        if (!response.ok) throw new Error("暂时无法读取站点设置，请刷新重试。");
        const value = await response.json();
        window.__APP_CONFIG__ = (value.data ?? value) as SiteSettings;
    }
    document.title = studioName();
    const token = localStorage.getItem("auth_token");
    if (!token) return signIn();
    const response = await fetch("/api/v1/auth/me", { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
    if (response.status === 401 || response.status === 403) return signIn();
    if (!response.ok) throw new Error("暂时无法确认登录状态，请刷新重试。");
    const value = await response.json();
    const id = value.data?.id;
    if (!Number.isSafeInteger(id) || id <= 0) throw new Error("登录状态无效，请重新登录。");
    if (window.__APP_CONFIG__?.backend_mode_enabled && value.data?.role !== "admin") return signIn();
    window.__IKUN_NATIVE_STUDIO_USER__ = String(id);
    const theme = localStorage.getItem("theme");
    if (theme === "dark" || theme === "light") document.documentElement.classList.toggle("dark", theme === "dark");
    await import("./main");
    window.addEventListener("storage", event => {
        if (event.key === "auth_token" && event.newValue !== token) window.location.reload();
    });
}
function signIn() {
    const next = window.location.pathname + window.location.search;
    window.location.replace(`/login?redirect=${encodeURIComponent(next)}`);
}
void bootstrap().catch(error => {
    const root = document.getElementById("root");
    if (root) root.textContent = error instanceof Error ? error.message : "工作室暂时无法加载，请刷新重试。";
});
