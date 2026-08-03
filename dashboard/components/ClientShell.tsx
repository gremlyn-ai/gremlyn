"use client";

import { useEffect } from "react";
import { GlobalTerminal } from "./GlobalTerminal";
import { useTerminalStore } from "@/lib/stores/terminalStore";

export function ClientShell({ children }: { children: React.ReactNode }) {
  const toggle = useTerminalStore((s) => s.toggle);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      // Ignore backtick when typing in inputs/textareas
      if (
        e.key === "`" &&
        !["INPUT", "TEXTAREA", "SELECT"].includes(
          (e.target as HTMLElement)?.tagName,
        )
      ) {
        e.preventDefault();
        toggle();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [toggle]);

  return (
    <>
      <GlobalTerminal />
      {children}
    </>
  );
}
