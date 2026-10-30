import { defineComponent } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useBlindbox } from "./useBlindbox";

const api = vi.hoisted(() => ({
  status: vi.fn(),
  entries: vi.fn(),
  buy: vi.fn(),
  open: vi.fn(),
}));
const refreshUser = vi.hoisted(() => vi.fn());
vi.mock("@/api/blindbox", () => ({ blindboxAPI: api }));
vi.mock("@/api/workspace", () => ({ workspaceError: () => "请求暂未完成" }));
vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({ user: { id: 8 }, refreshUser }),
}));

function setup() {
  let state!: ReturnType<typeof useBlindbox>;
  const wrapper = mount(
    defineComponent({
      setup() {
        state = useBlindbox();
        return () => null;
      },
    }),
  );
  return { state, wrapper };
}

describe("盲盒购买防重复", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    sessionStorage.clear();
    api.status.mockResolvedValue({
      round: { spec: { price: "2.5" } },
      quote: "signed-quote",
      eligibility: { can_buy: true },
    });
    api.entries.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
    });
    api.buy.mockResolvedValue({ entry: { id: 1 }, already_purchased: false });
    refreshUser.mockResolvedValue({ balance: 10 });
  });

  it("并发点击只提交一次，服务端成功后才更换购买凭据", async () => {
    const { state, wrapper } = setup();
    await flushPromises();
    state.confirmBuy();
    await Promise.all([state.buy(), state.buy()]);
    expect(api.buy).toHaveBeenCalledOnce();
    expect(api.buy).toHaveBeenCalledWith(expect.any(String), "signed-quote");
    expect(state.message.value).toContain("参与成功");
    expect(sessionStorage.getItem("ikun-blindbox-pending:8")).toBeNull();
    wrapper.unmount();
  });

  it("超时或服务器异常保留同一购买编号，重载后重试仍不会生成新扣款", async () => {
    let instance = setup();
    await flushPromises();
    instance.state.confirmBuy();
    api.buy.mockRejectedValue({ status: 0 });
    await instance.state.buy();
    const key = api.buy.mock.calls[0][0];
    instance.wrapper.unmount();
    instance = setup();
    await flushPromises();
    instance.state.confirmBuy();
    api.buy.mockResolvedValue({ entry: { id: 1 }, already_purchased: true });
    await instance.state.buy();
    expect(api.buy.mock.calls[1][0]).toBe(key);
    expect(instance.state.message.value).toContain("无需重复支付");
    instance.wrapper.unmount();
  });

  it("规则被拒绝后清理旧报价，下一次购买获取新报价", async () => {
    const { state, wrapper } = setup();
    await flushPromises();
    state.confirmBuy();
    api.buy.mockRejectedValue({ status: 409 });
    await state.buy();
    expect(sessionStorage.getItem("ikun-blindbox-pending:8")).toBeNull();
    expect(state.confirming.value).toBe(false);
    wrapper.unmount();
  });
});
