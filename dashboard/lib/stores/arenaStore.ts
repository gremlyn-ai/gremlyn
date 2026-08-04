import { create } from "zustand";
import { persist, type StorageValue } from "zustand/middleware";
import type { ArenaSession, ArenaEvent, GremlinInfo } from "@/lib/api/types";

type ArenaState = {
  currentSession: ArenaSession | null;
  events: ArenaEvent[];
  gremlins: GremlinInfo[];
  wsConnected: boolean;
  loading: boolean;
  error: string | null;
  showTerminal: boolean;

  setSession: (session: ArenaSession | null) => void;
  addEvent: (event: ArenaEvent) => void;
  clearEvents: () => void;
  setGremlins: (g: GremlinInfo[]) => void;
  setWsConnected: (connected: boolean) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  toggleTerminal: () => void;
};

/** Only the live session survives a reload — events are refetched from the REST API. */
type ArenaPersistedState = Pick<ArenaState, "currentSession">;

export const useArenaStore = create<ArenaState>()(
  persist<ArenaState, [], [], ArenaPersistedState>(
    (set) => ({
      currentSession: null,
      events: [],
      gremlins: [],
      wsConnected: false,
      loading: false,
      error: null,
      showTerminal: false,

      setSession: (session) => set({ currentSession: session }),

      addEvent: (event) =>
        set((state) => ({ events: [...state.events, event] })),

      clearEvents: () => set({ events: [] }),

      setGremlins: (gremlins) => set({ gremlins }),

      setWsConnected: (connected) => set({ wsConnected: connected }),

      setLoading: (loading) => set({ loading }),

      setError: (error) => set({ error }),

      toggleTerminal: () => set((state) => ({ showTerminal: !state.showTerminal })),
    }),
    {
      name: "gremlyn-arena",
      storage: {
        getItem: (name) => {
          const str = sessionStorage.getItem(name);
          return str ? (JSON.parse(str) as StorageValue<ArenaPersistedState>) : null;
        },
        setItem: (name, value) => sessionStorage.setItem(name, JSON.stringify(value)),
        removeItem: (name) => sessionStorage.removeItem(name),
      },
      partialize: (state) => ({ currentSession: state.currentSession }),
    },
  ),
);
