import { apiClient } from "./client";

export interface BlindboxSpec {
  price: string;
  minimum: string;
  maximum: string;
  draw_time: string;
}
export interface BlindboxSettings extends BlindboxSpec {
  enabled: boolean;
}
export interface BlindboxRound {
  id: number;
  date: string;
  spec: BlindboxSpec;
  draw_at: string;
  status: "open" | "settled";
  count: number;
  total_paid: string;
  total_awarded: string;
  result_digest: string;
  settled_at: string | null;
}
export interface BlindboxTier {
  amount: string;
  count: number;
  probability: string;
  result: "win" | "even" | "lose";
}
export interface BlindboxRank {
  name: string;
  count: number;
  paid: string;
  reward?: string;
  profit?: string;
  mine: boolean;
}
export interface BlindboxStatus {
  enabled: boolean;
  server_time: string;
  round: BlindboxRound;
  plan: {
    tiers: BlindboxTier[];
    total: string;
    even: number;
    lose: number;
    win: number;
    refund: boolean;
  };
  participants: number;
  ranking: BlindboxRank[];
  winners: BlindboxRank[];
  winners_date: string;
  quote: string;
  eligibility: {
    checked_in: boolean;
    usage_amount: string;
    limit: number;
    bought: number;
    remaining: number;
    can_buy: boolean;
    reason: string;
  };
}
export interface BlindboxEntry {
  id: number;
  round_id: number;
  user_id: number;
  date: string;
  ordinal: number;
  paid: string;
  reward: string | null;
  credit_id: number | null;
  debit_id: number;
  opened_at: string | null;
  created_at: string;
  draw_at: string;
  status: "open" | "settled";
  username?: string;
}
export interface BlindboxEntryPage {
  items: BlindboxEntry[];
  total: number;
  page: number;
  page_size: number;
}
export interface BlindboxRoundPage {
  items: BlindboxRound[];
  total: number;
  page: number;
  page_size: number;
}

export const blindboxAPI = {
  async status(day?: string) {
    return (
      await apiClient.get<BlindboxStatus>("/workspace/blindbox", {
        params: { day },
      })
    ).data;
  },
  async entries(page = 1, admin = false, roundID?: number) {
    return (
      await apiClient.get<BlindboxEntryPage>(
        `${admin ? "/admin" : ""}/workspace/blindbox/entries`,
        { params: { page, page_size: 20, round_id: roundID } },
      )
    ).data;
  },
  async buy(requestKey: string, quote: string) {
    return (
      await apiClient.post<{
        entry: BlindboxEntry;
        already_purchased: boolean;
      }>("/workspace/blindbox/buy", { request_key: requestKey, quote })
    ).data;
  },
  async open(id: number) {
    return (
      await apiClient.post<{ entry: BlindboxEntry; profit: string }>(
        `/workspace/blindbox/entries/${id}/open`,
        {},
      )
    ).data;
  },
  async settings() {
    return (
      await apiClient.get<BlindboxSettings>(
        "/admin/workspace/blindbox/settings",
      )
    ).data;
  },
  async save(settings: BlindboxSettings) {
    return (
      await apiClient.put<BlindboxSettings>(
        "/admin/workspace/blindbox/settings",
        settings,
      )
    ).data;
  },
  async rounds(page = 1) {
    return (
      await apiClient.get<BlindboxRoundPage>(
        "/admin/workspace/blindbox/rounds",
        { params: { page, page_size: 20 } },
      )
    ).data;
  },
};
