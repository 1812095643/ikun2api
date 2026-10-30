<script setup lang="ts">
import { onMounted, reactive, shallowRef } from "vue";
import Toggle from "@/components/common/Toggle.vue";
import { blindboxAPI, type BlindboxSettings } from "@/api/blindbox";
import { workspaceError } from "@/api/workspace";

const form = reactive<BlindboxSettings>({
  enabled: false,
  price: "2.5",
  minimum: "1",
  maximum: "10",
  draw_time: "21:00",
});
const loading = shallowRef(true);
const saving = shallowRef(false);
const ready = shallowRef(false);
const error = shallowRef("");
const message = shallowRef("");
async function load() {
  loading.value = true;
  try {
    Object.assign(form, await blindboxAPI.settings());
    ready.value = true;
    error.value = "";
  } catch (e) {
    error.value = workspaceError(e);
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (!ready.value || saving.value) return;
  saving.value = true;
  error.value = "";
  message.value = "";
  try {
    Object.assign(form, await blindboxAPI.save({ ...form }));
    message.value = "盲盒设置已保存";
  } catch (e) {
    error.value = workspaceError(e);
  } finally {
    saving.value = false;
  }
}
onMounted(load);
</script>

<template>
  <section class="blindbox-settings space-y-4">
    <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">
      每日盲盒设置
    </h2>
    <p
      v-if="error"
      class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300"
      role="alert"
    >
      {{ error }}
    </p>
    <p
      v-if="message"
      class="rounded-lg bg-blue-50 p-3 text-sm text-blue-700 dark:bg-blue-950/40 dark:text-blue-300"
      role="status"
    >
      {{ message }}
    </p>
    <p v-if="loading" class="py-8 text-sm text-gray-500 dark:text-gray-400">
      正在读取设置…
    </p>
    <form v-else-if="ready" class="card p-5 sm:p-6" @submit.prevent="save">
      <fieldset :disabled="saving" class="space-y-5">
        <div
          class="flex items-center justify-between gap-4 border-b border-gray-100 pb-5 dark:border-dark-700"
        >
          <div>
            <h3
              id="blindbox-enabled-label"
              class="font-semibold text-gray-900 dark:text-gray-100"
            >
              开启每日盲盒
            </h3>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              奖池全部返还，目标 30% 保本，其余亏损或盈利。
            </p>
          </div>
          <Toggle
            v-model="form.enabled"
            aria-labelledby="blindbox-enabled-label"
          />
        </div>
        <div class="grid max-w-2xl gap-5 sm:grid-cols-2">
          <div class="space-y-2">
            <label for="blindbox-price" class="input-label">每份售价（$）</label
            ><input
              id="blindbox-price"
              :value="form.price"
              type="number"
              class="input"
              inputmode="decimal"
              min="0.02"
              max="999.99"
              step="0.01"
              required
              @input="
                form.price = ($event.target as HTMLInputElement).value;
                message = '';
              "
            />
          </div>
          <div class="space-y-2">
            <label for="blindbox-time" class="input-label"
              >每日开奖时间（北京时间）</label
            ><input
              id="blindbox-time"
              v-model="form.draw_time"
              type="time"
              class="input"
              required
            />
          </div>
          <div class="space-y-2">
            <label for="blindbox-min" class="input-label">最低奖励（$）</label
            ><input
              id="blindbox-min"
              :value="form.minimum"
              type="number"
              class="input"
              inputmode="decimal"
              min="0.01"
              max="999.98"
              step="0.01"
              required
              @input="
                form.minimum = ($event.target as HTMLInputElement).value;
                message = '';
              "
            />
          </div>
          <div class="space-y-2">
            <label for="blindbox-max" class="input-label">最高奖励（$）</label
            ><input
              id="blindbox-max"
              :value="form.maximum"
              type="number"
              class="input"
              inputmode="decimal"
              min="0.03"
              max="1000"
              step="0.01"
              required
              @input="
                form.maximum = ($event.target as HTMLInputElement).value;
                message = '';
              "
            />
          </div>
        </div>
        <p class="max-w-2xl text-xs leading-6 text-gray-500 dark:text-gray-400">
          今日消费大于 0 至 100 解锁 1 份，超过 100 至 200 解锁 2
          份，依此类推。首份购买后锁定本场价格、奖励范围及开奖时间；修改从下一场生效。关闭开关只停止新购，已购场次照常结算。
        </p>
        <div
          class="flex justify-end border-t border-gray-100 pt-5 dark:border-dark-700"
        >
          <button type="submit" class="blindbox-save btn btn-primary">
            {{ saving ? "保存中…" : "保存设置" }}
          </button>
        </div>
      </fieldset>
    </form>
    <button v-else class="btn btn-secondary" @click="load">重新加载</button>
  </section>
</template>

<style scoped>
.blindbox-save {
  @apply from-blue-500 to-blue-600 shadow-blue-500/20 hover:from-blue-600 hover:to-blue-700 hover:shadow-blue-500/20;
}
.blindbox-settings :deep([role="switch"][aria-checked="true"]) {
  @apply bg-blue-600;
}
.blindbox-settings :deep([role="switch"]) {
  @apply focus:ring-blue-500;
}
.blindbox-settings .input {
  @apply focus:border-blue-500 focus:ring-blue-500/20;
}
</style>
