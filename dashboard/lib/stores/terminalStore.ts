import { create } from "zustand";
import { persist } from "zustand/middleware";

type HistoryEntry = {
  type: "input" | "output" | "error";
  text: string;
};

type TerminalState = {
  isOpen: boolean;
  history: HistoryEntry[];
  commandHistory: string[];
  historyIndex: number;

  toggle: () => void;
  setOpen: (open: boolean) => void;
  addInput: (text: string) => void;
  addOutput: (text: string) => void;
  addError: (text: string) => void;
  clearHistory: () => void;
  setHistoryIndex: (index: number) => void;
};

export const useTerminalStore = create<TerminalState>()(
  persist(
    (set) => ({
      isOpen: false,
      history: [],
      commandHistory: [],
      historyIndex: -1,

      toggle: () => set((s) => ({ isOpen: !s.isOpen })),
      setOpen: (open) => set({ isOpen: open }),

      addInput: (text) =>
        set((s) => ({
          history: [...s.history, { type: "input", text }],
          commandHistory: [text, ...s.commandHistory].slice(0, 50),
          historyIndex: -1,
        })),

      addOutput: (text) =>
        set((s) => ({
          history: [...s.history, { type: "output", text }],
        })),

      addError: (text) =>
        set((s) => ({
          history: [...s.history, { type: "error", text }],
        })),

      clearHistory: () => set({ history: [], historyIndex: -1 }),

      setHistoryIndex: (index) => set({ historyIndex: index }),
    }),
    {
      name: "gremlyn-terminal",
      storage: {
        getItem: (name) => {
          const str = sessionStorage.getItem(name);
          return str ? JSON.parse(str) : null;
        },
        setItem: (name, value) => sessionStorage.setItem(name, JSON.stringify(value)),
        removeItem: (name) => sessionStorage.removeItem(name),
      },
      partialize: (state) => ({ commandHistory: state.commandHistory }),
    },
  ),
);
