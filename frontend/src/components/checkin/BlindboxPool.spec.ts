import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import type { BlindboxStatus } from "@/api/blindbox";
import BlindboxPool from "./BlindboxPool.vue";

function status(threshold: string): BlindboxStatus {
  return {
    enabled: true,
    server_time: "2099-01-01T12:00:00+08:00",
    round: {
      id: 1, date: "2099-01-01", spec: { price: "2.5", minimum: "1", maximum: "10", draw_time: "21:00" },
      draw_at: "2099-01-01T21:00:00+08:00", status: "open", count: 0,
      total_paid: "0", total_awarded: "0", result_digest: "", settled_at: null,
    },
    plan: { tiers: [], total: "0", even: 0, lose: 0, win: 0, refund: false },
    participants: 0, ranking: [], winners: [], winners_date: "", quote: "quote",
    eligibility: { checked_in: true, usage_amount: "1", usage_per_chance: threshold, limit: 1, bought: 0, remaining: 1, can_buy: true, reason: "" },
  };
}

describe("盲盒参与规则", () => {
  it("显示和更新服务端的解锁门槛，不残留固定 100 的说明", async () => {
    const wrapper = mount(BlindboxPool, { props: { status: status("10"), busy: false } });
    expect(wrapper.get(".blindbox-rules").text()).toContain("每 10 额度消费可购 1 份");
    await wrapper.setProps({ status: status("0.00000001") });
    expect(wrapper.get(".blindbox-rules").text()).toContain("每 0.00000001 额度消费可购 1 份");
    expect(wrapper.get(".blindbox-rules").text()).not.toContain("100 可购");
    wrapper.unmount();
  });
});
