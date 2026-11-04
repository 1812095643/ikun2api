import { onMounted, ref, shallowRef, watch } from 'vue'
import { accountWarmupAPI, type WarmupState, type WarmupRun, type WarmupRule } from '@/api/accountWarmup'
import { useAppStore } from '@/stores/app'
export function useAccountWarmup() {
 const app = useAppStore()
 const state = ref<WarmupState>({ settings: { enabled: false }, rules: [], accounts: [], timezone: 'Asia/Shanghai', models: [], groups: [] })
 const runs = ref<WarmupRun[]>([]), selected = ref<Array<string | number>>([])
 const page = shallowRef(1), pageSize = shallowRef(20), total = shallowRef(0)
 const loading = shallowRef(false), busy = shallowRef(false), error = shallowRef('')
 let version = 0
 async function load() {
  loading.value = true; error.value = ''; const current = ++version
  try { const [data, records] = await Promise.all([accountWarmupAPI.get(), accountWarmupAPI.runs(page.value, pageSize.value)]); if (current !== version) return; state.value = data; runs.value = records.items; total.value = records.total }
  catch { error.value = '无法读取热身配置，请刷新重试。' } finally { if (current === version) loading.value = false }
 }
 async function action(fn: () => Promise<unknown>, text = '已保存') { if (busy.value) return false; busy.value = true; try { await fn(); app.showSuccess(text); await load(); return true } catch { app.showError('操作未完成，请检查配置或刷新重试。'); return false } finally { busy.value = false } }
 const save = (r: WarmupRule) => action(() => accountWarmupAPI.save(r))
 watch([page, pageSize], load)
 onMounted(load)
 return { state, runs, selected, page, pageSize, total, loading, busy, error, load, action, save }
}
