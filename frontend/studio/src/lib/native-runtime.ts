export function nativeStorageName(name: string): string {
    const owner = (window as Window & { __IKUN_NATIVE_STUDIO_USER__?: string }).__IKUN_NATIVE_STUDIO_USER__;
    if (!owner) throw new Error("生图工作室需要已登录的账号。");
    return `ikun-studio:${owner}:${name}`;
}

let portalElement: HTMLElement | null = null;
export function setNativePortal(element: HTMLElement | null) { portalElement = element; }
export function nativePortal(): HTMLElement { return portalElement || document.body; }
