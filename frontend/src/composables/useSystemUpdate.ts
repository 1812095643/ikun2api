import { computed, onUnmounted, shallowRef } from 'vue'
import { getUpdateStatus, checkUpdates, performUpdate, restartService, getVersion, type VersionInfo } from '@/api/admin/system'

export function useSystemUpdate() {
  const info = shallowRef<VersionInfo | null>(null)
  const phase = shallowRef<'idle' | 'checking' | 'updating' | 'restarting' | 'failed'>('idle')
  const error = shallowRef('')
  const needRestart = shallowRef(false)
  let disposed = false
  onUnmounted(() => { disposed = true })
  const busy = computed(() => ['checking', 'updating', 'restarting'].includes(phase.value))
  async function check(force = false) {
    if (busy.value) return
    phase.value = 'checking'; error.value = ''
    try { info.value = await checkUpdates(force); phase.value = 'idle' }
    catch (e) { error.value = errorMessage(e); phase.value = 'failed' }
  }
  async function restart() {
    phase.value = 'restarting'; error.value = ''
    const previous = info.value?.current_version
    try {
      await restartService()
      for (let i = 0; i < 90 && !disposed; i++) {
        await new Promise(resolve => setTimeout(resolve, 2000))
        try {
          const data = await getVersion()
          if (data.version && data.version !== previous) { window.location.reload(); return }
        } catch { /* Supervisor may still be restarting. */ }
      }
      if (!disposed) throw new Error('服务未在预计时间内恢复，请检查服务器日志后刷新。')
    } catch (e) { error.value = errorMessage(e); phase.value = 'failed' }
  }
  async function watchUpdate() {
    const previous = info.value?.current_version
    for (let i = 0; i < 450 && !disposed; i++) {
      await new Promise(resolve => setTimeout(resolve, 2000))
      try {
        const version = await getVersion()
        if (version.version && version.version !== previous) { window.location.reload(); return }
        const status = await getUpdateStatus()
        if (status.phase === 'failed') { error.value = status.error || '更新失败，现有程序仍保留。'; phase.value = 'failed'; return }
        if (status.phase === 'restarting') phase.value = 'restarting'
        if (status.phase === 'idle') { phase.value = 'idle'; await check(true); return }
      } catch (e) {
        void e
      }
    }
    if (!disposed) throw new Error('更新状态等待超时，请刷新查看版本或检查日志。')
  }
  async function update() {
    if (busy.value) return
    phase.value = 'updating'; error.value = ''
    try {
      const result = await performUpdate()
      if (result.started) { await watchUpdate(); return }
      needRestart.value = result.need_restart
      if (result.need_restart) await restart()
      else { phase.value = 'idle'; await check(true) }
    } catch (e) { error.value = errorMessage(e); phase.value = 'failed' }
  }
  return { info, phase, error, needRestart, busy, check, update, restart }
}
function errorMessage(error: unknown): string {
  const e = error as { response?: { data?: { message?: string } }; message?: string }
  return e.response?.data?.message || e.message || '操作失败，请稍后重试。'
}
