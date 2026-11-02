import { onUnmounted, shallowRef, watch, type Ref } from 'vue'
import { upstreamModelsAPI, type ModelMonitor, type ModelMonitorConfig, type ModelChange } from '@/api/upstreamModels'
import { useAppStore } from '@/stores/app'

export function useUpstreamModels(id: Ref<string>, onChanged: () => void) {
  const app = useAppStore()
  const data = shallowRef<ModelMonitor | null>(null)
  const loading = shallowRef(false)
  const busy = shallowRef(false)
  const error = shallowRef('')
  let controller: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  function stop() { controller?.abort(); clearTimeout(timer) }
  async function load() {
    stop()
    const target = id.value
    if (!target) return
    const request = new AbortController(); controller = request
    loading.value = !data.value
    try {
      const result = await upstreamModelsAPI.get(target, request.signal)
      if (request.signal.aborted || target !== id.value) return
      data.value = result; error.value = ''
      if (result.summary.checking || result.summary.queued) timer = setTimeout(() => void load(), 3000)
      else onChanged()
    } catch (e) {
      if (!request.signal.aborted) error.value = (e as Error).message || '模型目录加载失败'
    } finally { if (controller === request) loading.value = false }
  }
  async function run(task: () => Promise<unknown>, message: string) {
    if (busy.value) return false
    busy.value = true; error.value = ''
    try { await task(); app.showSuccess(message); await load(); onChanged(); return true }
    catch (e) { error.value = (e as Error).message || '操作失败，请重试'; app.showError(error.value); return false }
    finally { busy.value = false }
  }
  const refresh = () => run(() => upstreamModelsAPI.refresh(id.value), '已加入模型检测队列')
  const save = (config: ModelMonitorConfig) => run(() => upstreamModelsAPI.save(id.value, data.value!.revision, config), '模型监控设置已保存')
  const act = (action: 'apply' | 'ignore' | 'restore', change: ModelChange, initialize = false) =>
    run(() => upstreamModelsAPI.act(id.value, data.value!.revision, action, change, initialize), action === 'apply' ? '已更新所选账号的模型映射' : action === 'ignore' ? '已忽略所选变更' : '已恢复所选变更提醒')
  watch(id, () => { stop(); data.value = null; error.value = ''; loading.value = false; if (id.value) void load() }, { immediate: true })
  onUnmounted(stop)
  return { data, loading, busy, error, load, refresh, save, act }
}
