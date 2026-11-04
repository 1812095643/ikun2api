import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { resolveMenuLabel } from '@/config/menuPages'

export function useMenuLabels() {
 const app = useAppStore(), auth = useAuthStore(), admin = useAdminSettingsStore()
 const labels = computed(() => ({ ...(app.cachedPublicSettings?.menu_labels ?? {}), ...(auth.isAdmin ? admin.menuLabels : {}) }))
 const menuLabel = (path: string, fallback: string) => resolveMenuLabel(path, fallback, labels.value)
 return { labels, menuLabel }
}
