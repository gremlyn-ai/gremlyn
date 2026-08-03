import { create } from "zustand";
import type { ShieldEvent, Rule, Alert, ShieldMetricsResponse, ShieldStatusResponse } from "@/lib/api/types";

type ShieldState = {
  recentEvents: ShieldEvent[];
  rules: Rule[];
  alerts: Alert[];
  metrics: ShieldMetricsResponse | null;
  status: ShieldStatusResponse | null;
  loading: boolean;
  error: string | null;

  setEvents: (events: ShieldEvent[]) => void;
  setRules: (rules: Rule[]) => void;
  setAlerts: (alerts: Alert[]) => void;
  setMetrics: (m: ShieldMetricsResponse) => void;
  setStatus: (s: ShieldStatusResponse) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  toggleRule: (ruleId: string) => void;
};

export const useShieldStore = create<ShieldState>((set) => ({
  recentEvents: [],
  rules: [],
  alerts: [],
  metrics: null,
  status: null,
  loading: false,
  error: null,

  setEvents: (events) => set({ recentEvents: events }),
  setRules: (rules) => set({ rules }),
  setAlerts: (alerts) => set({ alerts }),
  setMetrics: (metrics) => set({ metrics }),
  setStatus: (status) => set({ status }),
  setLoading: (loading) => set({ loading }),
  setError: (error) => set({ error }),

  toggleRule: (ruleId) =>
    set((state) => ({
      rules: state.rules.map((r) =>
        r.id === ruleId ? { ...r, enabled: !r.enabled } : r
      ),
    })),
}));
