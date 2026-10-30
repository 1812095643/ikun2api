import { onBeforeUnmount, ref, watch } from 'vue'
import { lotteryAPI, type LotteryActivity, type LotteryPreview, type LotterySpec } from '@/api/lottery'
import { workspaceError } from '@/api/workspace'

function inputTime(value: string) {
  return new Date(new Date(value).getTime() + 8 * 3600000).toISOString().slice(0, 16)
}

function initialSpec(activity: LotteryActivity | null): LotterySpec {
  if (activity) {
    const spec = JSON.parse(JSON.stringify(activity.spec)) as LotterySpec
    return { ...spec, start_at: inputTime(spec.start_at), draw_at: spec.draw_at ? inputTime(spec.draw_at) : null }
  }
  const start = new Date()
  return {
    title: '', description: '', business_date: inputTime(start.toISOString()).slice(0, 10),
    match: 'all', conditions: [],
    prizes: [{ key: 'first', amount: '10', count: 1 }, { key: 'consolation', amount: '1', count: 5 }],
    draw_mode: 'time', capacity: 0, start_at: inputTime(start.toISOString()),
    draw_at: inputTime(new Date(start.getTime() + 24 * 3600000).toISOString())
  }
}

export function useLotteryEditor(activity: LotteryActivity | null, isBusy: () => boolean) {
  const form = ref(initialSpec(activity))
  const formElement = ref<HTMLFormElement | null>(null)
  const step = ref(1)
  const preview = ref<LotteryPreview | null>(null)
  const previewBusy = ref(false)
  const previewError = ref('')
  const confirmPublish = ref(false)
  let alive = true

  watch(form, () => {
    preview.value = null
    confirmPublish.value = false
    previewError.value = ''
  }, { deep: true, flush: 'sync' })

  function validate() {
    if (!formElement.value?.reportValidity()) return false
    if (step.value === 1 && !form.value.title.trim()) {
      previewError.value = '请填写活动名称。'
      return false
    }
    if (step.value === 3 && !form.value.prizes.length) {
      previewError.value = '请至少启用一个奖项。'
      return false
    }
    return true
  }

  function next() {
    if (isBusy() || previewBusy.value || !validate()) return
    previewError.value = ''
    step.value = Math.min(3, step.value + 1)
  }

  function previous() {
    if (isBusy() || previewBusy.value) return
    previewError.value = ''
    step.value = Math.max(1, step.value - 1)
  }

  function payload(): LotterySpec {
    return {
      ...form.value,
      title: form.value.title.trim(),
      start_at: new Date(`${form.value.start_at}:00+08:00`).toISOString(),
      draw_at: form.value.draw_mode === 'full' || !form.value.draw_at
        ? null : new Date(`${form.value.draw_at}:00+08:00`).toISOString(),
      capacity: Number(form.value.capacity)
    }
  }

  async function check() {
    if (isBusy() || previewBusy.value || step.value !== 3 || !validate()) return
    previewBusy.value = true
    preview.value = null
    confirmPublish.value = false
    previewError.value = ''
    try {
      const snapshot = JSON.stringify(payload())
      const result = await lotteryAPI.preview(JSON.parse(snapshot))
      // 表单改动后丢弃旧预览，发布只能使用当前规则的服务端核验结果。
      if (alive && snapshot === JSON.stringify(payload())) preview.value = result
    } catch (e) {
      if (alive) previewError.value = workspaceError(e)
    } finally {
      if (alive) previewBusy.value = false
    }
  }

  function submission(publish: boolean) {
    if (isBusy() || previewBusy.value || step.value !== 3 || !validate()) return null
    if (publish && (!preview.value || !confirmPublish.value)) return null
    return payload()
  }

  onBeforeUnmount(() => { alive = false })
  return { form, formElement, step, preview, previewBusy, previewError, confirmPublish, next, previous, check, submission }
}
