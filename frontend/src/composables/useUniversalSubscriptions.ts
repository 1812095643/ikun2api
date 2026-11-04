import { ref, shallowRef } from 'vue'
import { useAppStore } from '@/stores/app'
import { universalSubscriptionsAPI, universalError, type UniversalSubscription } from '@/api/universalSubscriptions'
export function useUniversalSubscriptions(admin = false) {
 const items = ref<UniversalSubscription[]>([])
 const loading = shallowRef(false)
 const total = shallowRef(0)
 const page = shallowRef(1)
 const userId = shallowRef<number | undefined>()
 const error = shallowRef('')
 let generation = 0
 async function refresh() {
  const token = ++generation; loading.value = true; error.value = ''
  try { const data = await universalSubscriptionsAPI.list(admin, { page: page.value, page_size: 30, user_id: userId.value }); if (token === generation) { items.value = data.items; total.value = data.total } }
  catch (e) { if (token === generation) { error.value = universalError(e, '订阅暂时无法读取'); useAppStore().showError(error.value) } }
  finally { if (token === generation) loading.value = false }
 }
 return { items, loading, total, page, userId, error, refresh }
}
