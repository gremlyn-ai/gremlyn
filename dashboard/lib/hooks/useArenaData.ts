import useSWR from "swr";
import { listGremlins, getStatus, getSession, getSessionEvents, listSessions } from "@/lib/api/arena";
import { useArenaStore } from "@/lib/stores/arenaStore";

export function useArenaGremlins() {
  const setGremlins = useArenaStore((s) => s.setGremlins);
  return useSWR("arena-gremlins", listGremlins, {
    revalidateOnFocus: false,
    onSuccess: (data) => setGremlins(data),
  });
}

export function useArenaStatus() {
  return useSWR("arena-status", getStatus, { refreshInterval: 10_000 });
}

export function useArenaSession(id: string | null) {
  const setSession = useArenaStore((s) => s.setSession);
  return useSWR(
    id ? `arena-session-${id}` : null,
    () => getSession(id!),
    {
      refreshInterval: 2_000,
      onSuccess: (data) => setSession(data),
    }
  );
}

export function useSessionEvents(id: string | null) {
  return useSWR(
    id ? `arena-events-${id}` : null,
    () => getSessionEvents(id!),
    { refreshInterval: 5_000 }
  );
}

export function useArenaSessions() {
  return useSWR("arena-sessions", listSessions, {
    refreshInterval: 10_000,
  });
}
