import { computed, onUnmounted, shallowRef } from 'vue'
import { getUpdateStatus, checkUpdates, performUpdate, rollback as rollbackAPI, restartService, getVersion, type VersionInfo } from '@/api/admin/system'

type UpdatePhase = 'idle' | 'checking' | 'interrupting' | 'updating' | 'restarting' | 'failed'

export function useSystemUpdate(options: { autoRestart?: boolean } = { autoRestart: false }) {
  const info = shallowRef<VersionInfo | null>(null)
  const phase = shallowRef<UpdatePhase>('idle')
  const error = shallowRef('')
  const needRestart = shallowRef(false)
  let disposed = false
  onUnmounted(() => { disposed = true })
  const busy = computed(() => ['checking', 'interrupting', 'updating', 'restarting'].includes(phase.value))
  const taskPhases = ['draining', 'interrupting', 'downloading', 'validating', 'backing_up', 'restarting']
  function taskPhase(value: string): UpdatePhase {
    if (value === 'restarting') return 'restarting'
    if (value === 'interrupting' || value === 'draining') return 'interrupting'
    return 'updating'
  }
  function fail(e: unknown) { error.value = errorMessage(e); phase.value = 'failed' }
  async function check(force = false) {
    if (busy.value || disposed) return
    phase.value = 'checking'; error.value = ''
    try {
      const result = await checkUpdates(force)
      if (disposed) return
      info.value = result
      const status = await getUpdateStatus()
      if (disposed) return
      if (status.phase === 'failed') { error.value = status.error || '上次更新未完成，请查看服务器日志。'; phase.value = 'failed' }
      else if (taskPhases.includes(status.phase)) {
        phase.value = taskPhase(status.phase)
        void watchUpdate().catch(fail)
      } else if (status.phase === 'ready') { needRestart.value = true; phase.value = 'idle' }
      else phase.value = 'idle'
    } catch (e) { if (!disposed) fail(e) }
  }
  async function restart() {
    if (disposed) return
    phase.value = 'restarting'; error.value = ''
    try {
      try { await restartService() } catch (e) {
        if ((e as { response?: unknown }).response) throw e
        // The restart can close this connection before the success body arrives.
      }
      for (let i = 0; i < 90 && !disposed; i++) {
        await new Promise(resolve => setTimeout(resolve, 2000))
        try {
          const data = await getVersion()
          const status = await getUpdateStatus()
          if (data.version && !status.active && status.phase === 'idle') { window.location.reload(); return }
          if (status.phase === 'failed') throw new Error(status.error || '重启失败，已保留恢复快照。')
        } catch (e) {
          if ((e as Error).message?.includes('恢复快照')) throw e
        }
      }
      if (!disposed) throw new Error('服务未在预计时间内恢复，请检查服务器日志后刷新。')
    } catch (e) { if (!disposed) fail(e) }
  }
  async function watchUpdate() {
    const previous = info.value?.current_version
    for (let i = 0; i < 630 && !disposed; i++) {
      await new Promise(resolve => setTimeout(resolve, 2000))
      if (disposed) return
      try {
        const version = await getVersion()
        const status = await getUpdateStatus()
        if (disposed) return
        if (version.version && version.version !== previous && !status.active && status.phase === 'idle') { window.location.reload(); return }
        if (status.phase === 'failed') { error.value = status.error || '更新失败，现有程序仍保留。'; phase.value = 'failed'; return }
        if (status.phase === 'ready') { needRestart.value = true; phase.value = 'idle'; return }
        if (taskPhases.includes(status.phase)) phase.value = taskPhase(status.phase)
        if (status.phase === 'idle' && !status.active) { phase.value = 'idle'; await check(true); return }
      } catch { /* The supervisor can temporarily stop the listener while backing up. */ }
    }
    if (!disposed) throw new Error('更新状态等待超时，请刷新查看版本或检查日志。')
  }
  async function install(target?: string) {
    if (busy.value || disposed) return
    phase.value = 'updating'; error.value = ''; needRestart.value = false
    try {
      const result = target ? await rollbackAPI(target) : await performUpdate()
      if (disposed) return
      if (result.started) { await watchUpdate(); return }
      needRestart.value = result.need_restart
      if (result.need_restart && options.autoRestart !== false) await restart()
      else if (result.need_restart) phase.value = 'idle'
      else { phase.value = 'idle'; await check(true) }
    } catch (e) {
      if (disposed) return
      // A proxy/client timeout does not mean a detached server job was canceled.
      if (!(e as { response?: unknown }).response) {
        try {
          const status = await getUpdateStatus()
          if (taskPhases.includes(status.phase)) { phase.value = taskPhase(status.phase); await watchUpdate(); return }
        } catch { /* Preserve the original operation error. */ }
      }
      fail(e)
    }
  }
  return { info, phase, error, needRestart, busy, check, update: () => install(), rollback: (version: string) => install(version), restart }
}
function errorMessage(error: unknown): string {
  const e = error as { response?: { data?: { message?: string } }; message?: string }
  return e.response?.data?.message || e.message || '操作失败，请稍后重试。'
}
