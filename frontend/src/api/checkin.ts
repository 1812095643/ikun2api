import { apiClient } from "./client";

export interface CheckinSettings {
  enabled: boolean;
  mode: "fixed" | "random";
  fixed_amount: string;
  random_min: string;
  random_max: string;
}

export interface CheckinRecord {
  id: number;
  day: string;
  amount: string;
  consecutive_days: number;
  mode: "fixed" | "random";
  created_at: string;
}

export interface CheckinStatus {
  settings: CheckinSettings;
  today: string;
  server_time: string;
  next_reset: string;
  checked_in: boolean;
  today_amount: string;
  summary: {
    total_days: number;
    consecutive_days: number;
    total_amount: string;
  };
  items: CheckinRecord[];
  total: number;
  page: number;
  page_size: number;
  month: string;
  month_items: CheckinRecord[];
}

export const checkinAPI = {
  async status(page = 1, month?: string) {
    return (
      await apiClient.get<CheckinStatus>("/workspace/checkin", {
        params: { page, page_size: 20, month },
      })
    ).data;
  },
  async claim() {
    return (
      await apiClient.post<{
        record: CheckinRecord;
        already_checked_in: boolean;
      }>("/workspace/checkin", {})
    ).data;
  },
  async settings() {
    return (
      await apiClient.get<CheckinSettings>("/admin/workspace/checkin/settings")
    ).data;
  },
  async save(settings: CheckinSettings) {
    return (
      await apiClient.put<CheckinSettings>(
        "/admin/workspace/checkin/settings",
        settings,
      )
    ).data;
  },
};

export function checkinMoney(value: string) {
  const [whole, fraction = ""] = value.split(".");
  return `$${whole}.${fraction.padEnd(2, "0")}`;
}
