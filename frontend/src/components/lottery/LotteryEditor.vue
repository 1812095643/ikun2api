<script setup lang="ts">
import BaseDialog from '@/components/common/BaseDialog.vue'
import FormSteps from '@/components/common/FormSteps.vue'
import Select from '@/components/common/Select.vue'
import LotteryAudienceEditor from './LotteryAudienceEditor.vue'
import LotteryPrizeEditor from './LotteryPrizeEditor.vue'
import { lotteryMoney, type LotteryActivity, type LotterySpec } from '@/api/lottery'
import { useLotteryEditor } from '@/composables/workspace/useLotteryEditor'

const props = defineProps<{ activity: LotteryActivity | null; busy: boolean; error: string }>()
const emit = defineEmits<{ save: [spec: LotterySpec, publish: boolean]; close: [] }>()
const steps = ['基本信息', '参与条件', '奖项与开奖']
const drawModes = [
  { value: 'time', label: '定时开奖' },
  { value: 'full', label: '人满开奖' },
  { value: 'either', label: '人满或到时开奖' }
]
const { form, formElement, step, preview, previewBusy, previewError, confirmPublish,
  next, previous, check, submission } = useLotteryEditor(props.activity, () => props.busy)

function close() {
  if (!props.busy) emit('close')
}
function submit(publish: boolean) {
  const spec = submission(publish)
  if (spec) emit('save', spec, publish)
}
</script>

<template>
  <BaseDialog
    :show="true"
    :title="activity ? '编辑抽奖' : '发起抽奖'"
    width="wide"
    content-class="lottery-editor-dialog"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    @close="close"
  >
    <FormSteps :steps="steps" :current="step" accent="blue" label="抽奖设置步骤" />
    <p v-if="error || previewError" class="lottery-error mb-4" role="alert">{{ error || previewError }}</p>
    <form ref="formElement" class="lottery-editor" aria-label="抽奖设置" @submit.prevent="step < 3 ? next() : check()">
      <fieldset :disabled="busy" class="min-w-0 space-y-5">
        <section v-if="step === 1" class="space-y-5">
          <div>
            <label for="lottery-title" class="input-label">活动名称</label>
            <input id="lottery-title" v-model="form.title" class="input" maxlength="80" required placeholder="请输入活动名称" />
          </div>
          <div>
            <label for="lottery-description" class="input-label">活动说明</label>
            <textarea id="lottery-description" v-model="form.description" class="input" maxlength="1500" rows="4" placeholder="选填" />
          </div>
        </section>
        <LotteryAudienceEditor v-else-if="step === 2" v-model:conditions="form.conditions" v-model:match="form.match" v-model:date="form.business_date" />
        <template v-else>
          <LotteryPrizeEditor v-model="form.prizes" />
          <section class="lottery-form-section lottery-draw-settings">
            <header><h3>开奖设置</h3></header>
            <div class="lottery-form-inline">
              <label>开奖方式<Select v-model="form.draw_mode" :options="drawModes" aria-label="开奖方式" /></label>
              <label>{{ form.draw_mode === 'time' ? '报名上限（0 为不限）' : '开奖人数' }}<input v-model.number="form.capacity" class="input" type="number" :min="form.draw_mode === 'time' ? 0 : 1" max="100000" step="1" required /></label>
              <label>报名开始（北京时间）<input v-model="form.start_at" class="input" type="datetime-local" required /></label>
              <label v-if="form.draw_mode !== 'full'">开奖时间（北京时间）<input v-model="form.draw_at" class="input" type="datetime-local" :min="form.start_at" required /></label>
            </div>
          </section>
          <section class="lottery-form-section lottery-publish-section">
            <div class="flex flex-wrap items-center gap-4">
              <button class="btn btn-secondary" type="button" :disabled="previewBusy || busy" @click="check">{{ previewBusy ? '查询中…' : '查看符合人数' }}</button>
              <div v-if="preview" class="lottery-preview-result" role="status">
                <p>符合条件 <strong>{{ preview.eligible_count }}</strong> 人 · {{ preview.prize_count }} 个名额 · 总奖额 <strong>${{ lotteryMoney(preview.prize_total) }}</strong></p>
                <small v-if="preview.eligible_count < preview.prize_count" class="lottery-muted">当前符合人数少于奖项名额。</small>
              </div>
            </div>
            <label v-if="preview" class="lottery-confirm-label"><input v-model="confirmPublish" type="checkbox" />确认奖额与规则，发布后锁定</label>
          </section>
        </template>
      </fieldset>
    </form>
    <template #footer>
      <div class="lottery-editor-footer">
        <button class="btn btn-secondary" type="button" :disabled="busy || previewBusy" @click="step === 1 ? close() : previous()">{{ step === 1 ? '取消' : '上一步' }}</button>
        <div class="flex gap-3">
          <button v-if="step === 3" class="btn btn-secondary" type="button" :disabled="busy || previewBusy" @click="submit(false)">{{ busy ? '保存中…' : '保存草稿' }}</button>
          <button v-if="step < 3" class="btn btn-primary" type="button" :disabled="busy" @click="next">下一步</button>
          <button v-else class="btn btn-primary" type="button" :disabled="busy || previewBusy || !preview || !confirmPublish" @click="submit(true)">{{ busy ? '发布中…' : '发布抽奖' }}</button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
