<script setup lang="ts">
import { onMounted, reactive, shallowRef } from "vue";
import Toggle from "@/components/common/Toggle.vue";
import Select from "@/components/common/Select.vue";
import { checkinAPI, type CheckinSettings } from "@/api/checkin";
import { workspaceError } from "@/api/workspace";

const form = reactive<CheckinSettings>({
  enabled: false,
  mode: "fixed",
  fixed_amount: "0.1",
  random_min: "0.01",
  random_max: "0.1",
});
const loading = shallowRef(true);
const saving = shallowRef(false);
const ready = shallowRef(false);
const error = shallowRef("");
const message = shallowRef("");

async function load() {
  loading.value = true;
  error.value = "";
  try {
    Object.assign(form, await checkinAPI.settings());
    ready.value = true;
  } catch (e) {
    error.value = workspaceError(e);
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (saving.value || !ready.value) return;
  saving.value = true;
  error.value = "";
  message.value = "";
  try {
    Object.assign(form, await checkinAPI.save({ ...form }));
    message.value = "签到设置已保存";
  } catch (e) {
    error.value = workspaceError(e);
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section class="checkin-settings space-y-5">
    <p
      v-if="error"
      class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>
    <p
      v-if="message"
      class="rounded-lg bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:bg-blue-950/40 dark:text-blue-300"
      role="status"
    >
      {{ message }}
    </p>
    <p
      v-if="loading"
      class="py-12 text-center text-sm text-gray-500 dark:text-gray-400"
      role="status"
    >
      正在读取设置…
    </p>
    <form v-else-if="ready" class="card p-5 sm:p-6" @submit.prevent="save">
      <fieldset class="space-y-6" :disabled="saving">
        <div
          class="flex items-center justify-between gap-4 border-b border-gray-100 pb-5 dark:border-dark-700"
        >
          <div>
            <h2
              id="checkin-enabled-label"
              class="font-semibold text-gray-900 dark:text-gray-100"
            >
              开启每日签到
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              每人每天领取一次，奖励直接计入余额。
            </p>
          </div>
          <Toggle
            v-model="form.enabled"
            aria-labelledby="checkin-enabled-label"
          />
        </div>
        <div class="max-w-md space-y-5">
          <div class="space-y-2">
            <label for="checkin-mode" class="input-label">奖励方式</label>
            <Select
              id="checkin-mode"
              v-model="form.mode"
              aria-label="奖励方式"
              :disabled="saving"
              :options="[
                { value: 'fixed', label: '固定金额' },
                { value: 'random', label: '随机金额' },
              ]"
            />
          </div>
          <div v-if="form.mode === 'fixed'" class="space-y-2">
            <label for="checkin-fixed" class="input-label">签到金额（$）</label>
            <input
              id="checkin-fixed"
              :value="form.fixed_amount"
              type="number"
              inputmode="decimal"
              class="input"
              min="0.00000001"
              max="1000000"
              step="any"
              required
              @input="
                form.fixed_amount = ($event.target as HTMLInputElement).value;
                message = '';
              "
            />
          </div>
          <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div class="space-y-2">
              <label for="checkin-min" class="input-label">金额下限（$）</label>
              <input
                id="checkin-min"
                :value="form.random_min"
                type="number"
                inputmode="decimal"
                class="input"
                min="0.00000001"
                max="1000000"
                step="any"
                required
                @input="
                  form.random_min = ($event.target as HTMLInputElement).value;
                  message = '';
                "
              />
            </div>
            <div class="space-y-2">
              <label for="checkin-max" class="input-label">金额上限（$）</label>
              <input
                id="checkin-max"
                :value="form.random_max"
                type="number"
                inputmode="decimal"
                class="input"
                min="0.00000001"
                max="1000000"
                step="any"
                required
                @input="
                  form.random_max = ($event.target as HTMLInputElement).value;
                  message = '';
                "
              />
            </div>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            北京时间每日 00:00 重置；修改仅影响后续领取。
          </p>
        </div>
        <div
          class="flex justify-end border-t border-gray-100 pt-5 dark:border-dark-700"
        >
          <button
            type="submit"
            class="checkin-save btn btn-primary"
            :aria-busy="saving"
          >
            {{ saving ? "保存中…" : "保存设置" }}
          </button>
        </div>
      </fieldset>
    </form>
    <button v-else type="button" class="btn btn-secondary" @click="load">
      重新加载
    </button>
  </section>
</template>

<style scoped>
.checkin-save {
  @apply from-blue-500 to-blue-600 shadow-blue-500/20 hover:from-blue-600 hover:to-blue-700 hover:shadow-blue-500/20 focus:ring-blue-500/50 dark:shadow-blue-500/15;
}
.checkin-settings :deep([role="switch"][aria-checked="true"]) {
  @apply bg-blue-600;
}
.checkin-settings :deep([role="switch"]) {
  @apply focus:ring-blue-500;
}
.checkin-settings .input {
  @apply focus:border-blue-500 focus:ring-blue-500/20;
}
.checkin-settings :deep(.select-trigger:focus),
.checkin-settings :deep(.select-trigger-open) {
  @apply border-blue-500 ring-blue-500/20;
}
</style>

<style>
body:has(.checkin-settings) .select-dropdown-portal .select-option-selected {
  @apply bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300;
}
</style>
