import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import CheckinAdminSettings from "./CheckinAdminSettings.vue";

const api = vi.hoisted(() => ({ settings: vi.fn(), save: vi.fn() }));
vi.mock("@/api/checkin", () => ({ checkinAPI: api }));
vi.mock("@/api/workspace", () => ({ workspaceError: () => "暂时无法读取" }));
vi.mock("vue-i18n", async () => ({
  ...(await vi.importActual<typeof import("vue-i18n")>("vue-i18n")),
  useI18n: () => ({ t: (key: string) => key }),
}));

describe("签到管理表单", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    api.settings.mockResolvedValue({
      enabled: false,
      mode: "fixed",
      fixed_amount: "0.1",
      random_min: "0.01",
      random_max: "0.1",
    });
    api.save.mockImplementation(async (value) => value);
  });

  it("金额保持十进制字符串，不被数字输入框转换为浮点数", async () => {
    const wrapper = mount(CheckinAdminSettings);
    await flushPromises();
    await wrapper.get("#checkin-fixed").setValue("0.00000001");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(api.save).toHaveBeenCalledWith(
      expect.objectContaining({ fixed_amount: "0.00000001" }),
    );
    expect(wrapper.text()).toContain("签到设置已保存");
    wrapper.unmount();
  });

  it("读取配置未完成时不提供保存入口，防止默认值覆盖线上配置", async () => {
    api.settings.mockRejectedValue(new Error("network"));
    const wrapper = mount(CheckinAdminSettings);
    await flushPromises();
    expect(wrapper.find("form").exists()).toBe(false);
    expect(api.save).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
