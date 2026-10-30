export const NAVIGATION_CHANGED = 'ikun-navigation-changed'

export function refreshNavigation() {
  window.dispatchEvent(new Event(NAVIGATION_CHANGED))
}
