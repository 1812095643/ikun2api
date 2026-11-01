import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import { isMenuVisible } from '@/config/menuPages'

export function useMenuVisibility() {
  const app = useAppStore()
  const visibility = computed(() => app.cachedPublicSettings?.menu_visibility ?? {})
  const menuVisible = (path: string) => isMenuVisible(path, visibility.value)
  return { visibility, menuVisible }
}
