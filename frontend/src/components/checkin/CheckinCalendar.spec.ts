import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import CheckinCalendar from "./CheckinCalendar.vue";

describe("签到月历", () => {
  it("完整展示当月日期和已签到状态，未来月份不可前进", async () => {
    const wrapper = mount(CheckinCalendar, {
      props: {
        month: "2026-10",
        today: "2026-10-26",
        loading: false,
        items: [
          {
            id: 1,
            day: "2026-10-25",
            amount: "0.1",
            consecutive_days: 1,
            mode: "fixed",
            created_at: "",
          },
        ],
      },
    });
    expect(wrapper.findAll(".calendar-day")).toHaveLength(31);
    expect(wrapper.findAll(".signed")).toHaveLength(1);
    expect(wrapper.get('[aria-current="date"]').text()).toBe("26");
    expect(
      wrapper.get('[aria-label="下个月"]').attributes("disabled"),
    ).toBeDefined();
    await wrapper.get(".signed").trigger("click");
    expect(wrapper.text()).toContain("领取 $0.10");
    await wrapper.get('[aria-label="上个月"]').trigger("click");
    expect(wrapper.emitted("month")?.[0]).toEqual(["2026-09"]);
    wrapper.unmount();
  });
});
