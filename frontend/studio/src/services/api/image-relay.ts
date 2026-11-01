import i18n from "@/i18n";

let importedToken = "";

export function setImageRelayToken(token: string) {
    importedToken = token;
}

export async function fetchRelayedImage(url: string, signal?: AbortSignal) {
    const token = localStorage.getItem("auth_token") || importedToken;
    if (!token) throw new Error(i18n.t("common.imageLoginRequired"));
    const endpoint = new URL("/api/v1/workspace/image", window.location.origin);
    endpoint.searchParams.set("url", url);
    // 登录凭据只发到本站，由服务端验证；不会转发给图片站。
    const response = await fetch(endpoint, { headers: { Authorization: `Bearer ${token}` }, signal, cache: "no-store" });
    if (!response.ok) throw new Error(i18n.t(response.status === 401 ? "common.imageLoginRequired" : "common.remoteImageReadFailed"));
    if (!/^(image\/|application\/octet-stream)/i.test(response.headers.get("content-type") || "")) throw new Error(i18n.t("common.remoteImageReadFailed"));
    return response;
}
