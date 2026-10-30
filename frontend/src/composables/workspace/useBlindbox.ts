import { onBeforeUnmount, onMounted, shallowRef } from "vue";
import {
  blindboxAPI,
  type BlindboxEntry,
  type BlindboxEntryPage,
  type BlindboxStatus,
} from "@/api/blindbox";
import { workspaceError } from "@/api/workspace";
import { useAuthStore } from "@/stores/auth";
import { refreshNavigation } from '@/utils/navigationEvents';

export function useBlindbox() {
  const status = shallowRef<BlindboxStatus | null>(null);
  const history = shallowRef<BlindboxEntryPage | null>(null);
  const page = shallowRef(1);
  const loading = shallowRef(false);
  const busy = shallowRef(false);
  const error = shallowRef("");
  const message = shallowRef("");
  const confirming = shallowRef(false);
  const opening = shallowRef<number | null>(null);
  const revealed = shallowRef<BlindboxEntry | null>(null);
  const auth = useAuthStore();
  let active = true;
  let revision = 0;
  let timer: ReturnType<typeof setInterval> | undefined;
  let revealTimer: ReturnType<typeof setTimeout> | undefined;
  let pending: { requestKey: string; quote: string; price: string } | null =
    null;
  const confirmationPrice = shallowRef("0");
  const storageKey = () => `ikun-blindbox-pending:${auth.user?.id}`;

  async function load(nextPage = page.value, silent = false) {
    if (!active) return;
    const current = ++revision;
    if (!silent) loading.value = true;
    try {
      const [result, entries] = await Promise.all([
        blindboxAPI.status(),
        blindboxAPI.entries(nextPage),
      ]);
      if (!active || current !== revision) return;
      const newlySettled = history.value?.items.some(
        (old) =>
          !old.reward &&
          entries.items.some((item) => item.id === old.id && item.reward),
      );
      status.value = result;
      history.value = entries;
      page.value = nextPage;
      if (newlySettled) {
        message.value = "本场已开奖，奖励已自动到账";
        void auth.refreshUser().catch(() => undefined);
      }
      if (!silent) error.value = "";
    } catch (e) {
      if (active && current === revision && !silent)
        error.value = workspaceError(e);
    } finally {
      if (active && current === revision) loading.value = false;
    }
  }

  function confirmBuy() {
    if (busy.value || !status.value?.eligibility.can_buy) return;
    if (!pending) {
      try {
        const saved = sessionStorage.getItem(storageKey());
        if (saved) pending = JSON.parse(saved);
      } catch {
        pending = null;
      }
    }
    if (!pending) {
      pending = {
        requestKey: crypto.randomUUID(),
        quote: status.value.quote,
        price: status.value.round.spec.price,
      };
      sessionStorage.setItem(storageKey(), JSON.stringify(pending));
    }
    confirmationPrice.value = pending.price;
    confirming.value = true;
  }

  async function buy() {
    if (busy.value || !pending) return;
    busy.value = true;
    error.value = "";
    message.value = "";
    try {
      const result = await blindboxAPI.buy(pending.requestKey, pending.quote);
      pending = null;
      sessionStorage.removeItem(storageKey());
      if (!active) return;
      confirming.value = false;
      message.value = result.already_purchased
        ? "这份盲盒已参与，无需重复支付"
        : "参与成功，等待今晚开奖";
      await Promise.all([load(1), auth.refreshUser().catch(() => undefined)]);
    } catch (e) {
      if (active) {
        error.value = workspaceError(e);
        const statusCode =
          (e as { response?: { status?: number }; status?: number })?.response
            ?.status ?? (e as { status?: number })?.status;
        if (
          statusCode &&
          statusCode >= 400 &&
          statusCode < 500 &&
          statusCode !== 408 &&
          statusCode !== 429
        ) {
          pending = null;
          sessionStorage.removeItem(storageKey());
          confirming.value = false;
        }
        await load(page.value, true);
      }
    } finally {
      if (active) busy.value = false;
    }
  }

  async function open(entry: BlindboxEntry) {
    if (opening.value !== null || entry.reward === null) return;
    opening.value = entry.id;
    error.value = "";
    try {
      const result = await blindboxAPI.open(entry.id);
      refreshNavigation();
      if (!active) return;
      revealTimer = setTimeout(
        () => {
          revealed.value = result.entry;
          opening.value = null;
          void load(page.value, true);
        },
        window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 380,
      );
    } catch (e) {
      if (active) {
        error.value = workspaceError(e);
        opening.value = null;
      }
    }
  }

  function onVisible() {
    if (document.visibilityState === "visible" && !busy.value)
      void load(page.value, true);
  }
  onMounted(() => {
    void load();
    timer = setInterval(onVisible, 15000);
    window.addEventListener("focus", onVisible);
  });
  onBeforeUnmount(() => {
    active = false;
    ++revision;
    clearInterval(timer);
    clearTimeout(revealTimer);
    window.removeEventListener("focus", onVisible);
  });
  return {
    status,
    history,
    page,
    loading,
    busy,
    error,
    message,
    confirming,
    confirmationPrice,
    opening,
    revealed,
    load,
    confirmBuy,
    buy,
    open,
  };
}
