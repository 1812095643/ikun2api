import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import BlindboxAdminSettings from "./BlindboxAdminSettings.vue";

const api = vi.hoisted(() => ({ settings: vi.fn(), save: vi.fn() }));
vi.mock("@/api/blindbox", () => ({ blindboxAPI: api }));
vi.mock("@/api/workspace", () => ({ workspaceError: () => "暂时无法保存" }));

const settings = {
  enabled: true,
  price: "2.5",
  minimum: "1",
  maximum: "10",
  draw_time: "21:00",
  usage_per_chance: "10",
};

describe("盲盒解锁门槛设置", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    api.settings.mockResolvedValue({ ...settings });
    api.save.mockImplementation(async (value) => ({ ...value }));
  });

  it("显示服务端门槛，并以十进制字符串保存小数及重载后的配置", async () => {
    let wrapper = mount(BlindboxAdminSettings);
    await flushPromises();
    expect((wrapper.get("#blindbox-usage-per-chance").element as HTMLInputElement).value).toBe("10");
    await wrapper.get("#blindbox-usage-per-chance").setValue("0.00000001");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(api.save).toHaveBeenCalledWith({ ...settings, usage_per_chance: "0.00000001" });
    expect(wrapper.text()).toContain("盲盒设置已保存");
    wrapper.unmount();

    api.settings.mockResolvedValue({ ...settings, usage_per_chance: "0.00000001" });
    wrapper = mount(BlindboxAdminSettings);
    await flushPromises();
    expect((wrapper.get("#blindbox-usage-per-chance").element as HTMLInputElement).value).toBe("0.00000001");
    wrapper.unmount();
  });

  it("读取失败时不提供保存入口，避免默认值覆盖已有门槛", async () => {
    api.settings.mockRejectedValue(new Error("network"));
    const wrapper = mount(BlindboxAdminSettings);
    await flushPromises();
    expect(wrapper.find("form").exists()).toBe(false);
    expect(api.save).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("保存失败保留输入，并且不显示成功", async () => {
    api.save.mockRejectedValue(new Error("network"));
    const wrapper = mount(BlindboxAdminSettings);
    await flushPromises();
    await wrapper.get("#blindbox-usage-per-chance").setValue("250");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect((wrapper.get("#blindbox-usage-per-chance").element as HTMLInputElement).value).toBe("250");
    expect(wrapper.get('[role="alert"]').text()).toBe("暂时无法保存");
    expect(wrapper.find('[role="status"]').exists()).toBe(false);
    wrapper.unmount();
  });
});
