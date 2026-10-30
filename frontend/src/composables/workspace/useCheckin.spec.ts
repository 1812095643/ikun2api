import { defineComponent } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCheckin } from "./useCheckin";
import type { CheckinStatus } from "@/api/checkin";

const api = vi.hoisted(() => ({ status: vi.fn(), claim: vi.fn() }));
const refreshUser = vi.hoisted(() => vi.fn());
vi.mock("@/api/checkin", async () => ({
  ...(await vi.importActual<typeof import("@/api/checkin")>("@/api/checkin")),
  checkinAPI: api,
}));
vi.mock("@/stores/auth", () => ({ useAuthStore: () => ({ refreshUser }) }));
vi.mock("@/api/workspace", () => ({ workspaceError: () => "暂时无法读取" }));

function status(checked = false): CheckinStatus {
  return {
    settings: {
      enabled: true,
      mode: "fixed",
      fixed_amount: "0.1",
      random_min: "0.01",
      random_max: "0.1",
    },
    today: "2026-10-26",
    server_time: "2026-10-26T02:00:00Z",
    next_reset: "2026-10-26T16:00:00Z",
    checked_in: checked,
    today_amount: checked ? "0.1" : "0",
    summary: {
      total_days: checked ? 1 : 0,
      consecutive_days: checked ? 1 : 0,
      total_amount: checked ? "0.1" : "0",
    },
    items: [],
    total: checked ? 1 : 0,
    page: 1,
    page_size: 20,
    month: "2026-10",
    month_items: [],
  };
}

function setup() {
  let state!: ReturnType<typeof useCheckin>;
  const wrapper = mount(
    defineComponent({
      setup() {
        state = useCheckin();
        return () => null;
      },
    }),
  );
  return { state, wrapper };
}

describe("每日签到领取", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    api.status.mockResolvedValue(status());
    api.claim.mockResolvedValue({
      record: { amount: "0.1" },
      already_checked_in: false,
    });
    refreshUser.mockResolvedValue({ balance: 1 });
  });

  it("连续点击仅发送一次领取，成功后刷新统计与余额", async () => {
    const { state, wrapper } = setup();
    await flushPromises();
    api.status.mockResolvedValue(status(true));
    await Promise.all([state.claim(), state.claim()]);
    expect(api.claim).toHaveBeenCalledOnce();
    expect(api.claim).toHaveBeenCalledWith();
    expect(state.status.value?.summary.total_days).toBe(1);
    expect(state.feedback.value).toContain("$0.10 已到账");
    expect(refreshUser).toHaveBeenCalledOnce();
    await state.claim();
    expect(api.claim).toHaveBeenCalledOnce();
    wrapper.unmount();
  });

  it("功能关闭时不发放，重复领取响应不会显示新奖励", async () => {
    api.status.mockResolvedValue({
      ...status(),
      settings: { ...status().settings, enabled: false },
    });
    const { state, wrapper } = setup();
    await flushPromises();
    await state.claim();
    expect(api.claim).not.toHaveBeenCalled();
    api.status.mockResolvedValue(status());
    await state.load();
    api.claim.mockResolvedValue({
      record: { amount: "0.1" },
      already_checked_in: true,
    });
    await state.claim();
    expect(state.feedback.value).toBe("今日已签到");
    wrapper.unmount();
  });

  it("入账后余额刷新中断不重复发送领取，迟到的旧页面响应不覆盖新统计", async () => {
    const { state, wrapper } = setup();
    await flushPromises();
    let resolveOld!: (value: CheckinStatus) => void;
    api.status.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    );
    const oldLoad = state.load();
    api.status.mockResolvedValue(status(true));
    refreshUser.mockRejectedValue(new Error("connection interrupted"));
    await state.claim();
    resolveOld(status());
    await oldLoad;
    expect(state.status.value?.checked_in).toBe(true);
    expect(state.error.value).toBe("");
    expect(api.claim).toHaveBeenCalledOnce();
    wrapper.unmount();
  });
});
