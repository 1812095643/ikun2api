import { onBeforeUnmount, onMounted, shallowRef } from "vue";
import { checkinAPI, checkinMoney, type CheckinStatus } from "@/api/checkin";
import { workspaceError } from "@/api/workspace";
import { useAuthStore } from "@/stores/auth";
import { refreshNavigation } from '@/utils/navigationEvents';

export function useCheckin() {
  const status = shallowRef<CheckinStatus | null>(null);
  const page = shallowRef(1);
  const loading = shallowRef(true);
  const busy = shallowRef(false);
  const error = shallowRef("");
  const feedback = shallowRef("");
  const month = shallowRef<string>();
  const auth = useAuthStore();
  let active = true;
  let revision = 0;
  let resetTimer: ReturnType<typeof setTimeout> | undefined;

  async function load(nextPage = page.value) {
    const request = ++revision;
    loading.value = true;
    try {
      const result = await checkinAPI.status(nextPage, month.value);
      if (!active || request !== revision) return;
      status.value = result;
      month.value = result.month;
      page.value = result.page;
      error.value = "";
      clearTimeout(resetTimer);
      const remaining =
        Date.parse(result.next_reset) - Date.parse(result.server_time);
      resetTimer = setTimeout(
        () => {
          void load(1);
        },
        Math.max(1000, remaining + 500),
      );
    } catch (e) {
      if (active && request === revision) error.value = workspaceError(e);
    } finally {
      if (active && request === revision) loading.value = false;
    }
  }

  async function claim() {
    if (
      busy.value ||
      !status.value?.settings.enabled ||
      status.value.checked_in
    )
      return;
    busy.value = true;
    error.value = "";
    feedback.value = "";
    ++revision;
    try {
      const result = await checkinAPI.claim();
      refreshNavigation();
      if (!active) return;
      feedback.value = result.already_checked_in
        ? "今日已签到"
        : `签到成功，${checkinMoney(result.record.amount)} 已到账`;
      await Promise.all([load(1), auth.refreshUser().catch(() => undefined)]);
    } catch (e) {
      if (active) {
        await load();
        error.value = workspaceError(e);
      }
    } finally {
      if (active) busy.value = false;
    }
  }

  function onVisible() {
    if (document.visibilityState === "visible" && !busy.value) void load();
  }

  onMounted(() => {
    void load();
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", onVisible);
  });
  onBeforeUnmount(() => {
    active = false;
    ++revision;
    clearTimeout(resetTimer);
    document.removeEventListener("visibilitychange", onVisible);
    window.removeEventListener("focus", onVisible);
  });

  async function changeMonth(value: string) {
    month.value = value;
    await load();
  }
  return {
    status,
    page,
    loading,
    busy,
    error,
    feedback,
    load,
    claim,
    changeMonth,
  };
}
