import { onMounted, shallowRef, ref, watch } from 'vue'
import { accountVaultAPI, type VaultRecord, type VaultEntry } from '@/api/accountVault'
import { useAppStore } from '@/stores/app'
import { useStepUp, isStepUpCancelled } from './useStepUp'

export function useAccountVault() {
 const app = useAppStore(), stepUp = useStepUp()
 const items = ref<VaultRecord[]>([]), selected = ref<Array<string | number>>([])
 const status = shallowRef('')
 const query = shallowRef(''), platform = shallowRef('claude'), page = shallowRef(1), pageSize = shallowRef(20)
 const total = shallowRef(0), loading = shallowRef(false), busy = shallowRef(false), error = shallowRef('')
 let generation = 0
 async function load() {
  const request = ++generation; loading.value = true; error.value = ''
  try {
   const data = await accountVaultAPI.list({ page: page.value, page_size: pageSize.value, query: query.value, platform: platform.value, status: status.value })
   if (request !== generation) return
   items.value = data.items; total.value = data.total; selected.value = []
  } catch { if (request === generation) error.value = '无法读取资料库，请检查资料库密钥及访问权限。' }
  finally { if (request === generation) loading.value = false }
 }
 async function protect<T>(fn: () => Promise<T>): Promise<T> {
  try { return await stepUp.run(fn) } catch (e) { if (!isStepUpCancelled(e)) app.showError(stepUp.blockedReason.value ? '请先在个人资料中开启两步验证。' : '操作未完成，请检查权限或刷新后重试。'); throw e }
 }
 async function read(id: string, field: string) { return (await protect(() => accountVaultAPI.reveal(id, field))).value ?? '' }
 async function save(id: string | null, entry: VaultEntry) {
  busy.value = true
  try { if (id) await protect(() => accountVaultAPI.update(id, entry)); else await protect(() => accountVaultAPI.import(crypto.randomUUID(), [entry]))
   app.showSuccess('资料已保存'); await load(); return true
  } catch { return false } finally { busy.value = false }
 }
 async function remove(ids: string[]) { busy.value = true; try { const result = await protect(() => accountVaultAPI.delete(ids)); app.showSuccess('已删除 ' + result.deleted + ' 条资料'); await load(); return true } catch { return false } finally { busy.value = false } }
 watch([page, pageSize], load)
 onMounted(load)
 return { items, selected, query, platform, status, page, pageSize, total, loading, busy, error, stepUp, protect, read, save, remove, load }
}
