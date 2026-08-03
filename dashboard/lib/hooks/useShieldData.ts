import useSWR, { mutate } from "swr";
import { getEvents, getRules, getAlerts, getMetrics, getStatus, getBlockedEvents, listServers } from "@/lib/api/shield";
import { useShieldStore } from "@/lib/stores/shieldStore";

const SWR_OPTIONS = { refreshInterval: 30_000, revalidateOnFocus: true } as const;

export function useShieldStatus() {
  const setStatus = useShieldStore((s) => s.setStatus);
  return useSWR("shield-status", getStatus, {
    ...SWR_OPTIONS,
    onSuccess: (data) => setStatus(data),
  });
}

export function useShieldMetrics() {
  const setMetrics = useShieldStore((s) => s.setMetrics);
  return useSWR("shield-metrics", getMetrics, {
    ...SWR_OPTIONS,
    onSuccess: (data) => setMetrics(data),
  });
}

export function useShieldEvents() {
  const setEvents = useShieldStore((s) => s.setEvents);
  return useSWR("shield-events", () => getEvents(), {
    ...SWR_OPTIONS,
    onSuccess: (data) => setEvents(data),
  });
}

export function useBlockedEvents(limit = 1) {
  return useSWR(
    `shield-blocked-${limit}`,
    () => getBlockedEvents(limit),
    SWR_OPTIONS
  );
}

export function useShieldRules() {
  const setRules = useShieldStore((s) => s.setRules);
  return useSWR("shield-rules", () => getRules(), {
    ...SWR_OPTIONS,
    onSuccess: (data) => setRules(data),
  });
}

export function useShieldAlerts() {
  const setAlerts = useShieldStore((s) => s.setAlerts);
  return useSWR("shield-alerts", getAlerts, {
    ...SWR_OPTIONS,
    onSuccess: (data) => setAlerts(data),
  });
}

export function useShieldServers() {
  return useSWR("shield-servers", listServers, SWR_OPTIONS);
}

export function mutateServers() {
  return mutate("shield-servers");
}
