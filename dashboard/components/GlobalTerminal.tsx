"use client";

import { useRef, useEffect, useState, useCallback } from "react";
import { useTerminalStore } from "@/lib/stores/terminalStore";
import { execCommand } from "@/lib/api/exec";

export function GlobalTerminal() {
  const isOpen = useTerminalStore((s) => s.isOpen);
  const history = useTerminalStore((s) => s.history);
  const commandHistory = useTerminalStore((s) => s.commandHistory);
  const historyIndex = useTerminalStore((s) => s.historyIndex);
  const toggle = useTerminalStore((s) => s.toggle);
  const addInput = useTerminalStore((s) => s.addInput);
  const addOutput = useTerminalStore((s) => s.addOutput);
  const addError = useTerminalStore((s) => s.addError);
  const clearHistory = useTerminalStore((s) => s.clearHistory);
  const setHistoryIndex = useTerminalStore((s) => s.setHistoryIndex);

  const [input, setInput] = useState("");
  const [executing, setExecuting] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Auto-scroll on new output
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [history]);

  // Focus input when opened
  useEffect(() => {
    if (isOpen && inputRef.current) {
      inputRef.current.focus();
    }
  }, [isOpen]);

  const handleSubmit = useCallback(async () => {
    const cmd = input.trim();
    if (!cmd) return;

    setInput("");
    addInput(cmd);

    // Client-side commands
    if (cmd === "clear") {
      clearHistory();
      return;
    }

    setExecuting(true);
    try {
      const res = await execCommand(cmd);
      if (res.error) {
        addError(res.error);
      }
      if (res.output) {
        addOutput(res.output);
      }
    } catch {
      addError("Failed to reach Shield API");
    } finally {
      setExecuting(false);
    }
  }, [input, addInput, addOutput, addError, clearHistory]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter") {
        e.preventDefault();
        handleSubmit();
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        const nextIndex = Math.min(historyIndex + 1, commandHistory.length - 1);
        if (nextIndex >= 0 && commandHistory[nextIndex]) {
          setHistoryIndex(nextIndex);
          setInput(commandHistory[nextIndex]);
        }
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        const nextIndex = historyIndex - 1;
        if (nextIndex < 0) {
          setHistoryIndex(-1);
          setInput("");
        } else {
          setHistoryIndex(nextIndex);
          setInput(commandHistory[nextIndex] ?? "");
        }
      } else if (e.key === "Escape" || e.key === "`") {
        if (e.key === "`") e.preventDefault();
        toggle();
      }
    },
    [handleSubmit, historyIndex, commandHistory, setHistoryIndex, toggle],
  );

  return (
    <div
      className={`fixed top-0 left-0 right-0 z-[100] transition-transform duration-200 ease-out ${
        isOpen ? "translate-y-0" : "-translate-y-full"
      }`}
    >
      <div className="bg-black/95 backdrop-blur-sm border-b-2 border-primary/30 h-[40vh] flex flex-col">
        {/* Scanline overlay */}
        <div className="absolute inset-0 pointer-events-none opacity-5 scanline-arena" />

        {/* Header */}
        <div className="flex items-center justify-between px-4 py-2 border-b border-outline-variant/10 shrink-0">
          <div className="flex items-center gap-3">
            <span className="material-symbols-outlined text-primary text-sm">terminal</span>
            <span className="font-mono text-[10px] text-primary uppercase tracking-widest font-bold">
              GREMLYN_CONSOLE
            </span>
            <span className="font-mono text-[10px] text-on-surface-variant">
              beta
            </span>
          </div>
          <button
            onClick={toggle}
            className="text-on-surface-variant hover:text-error hover:bg-surface-container-low p-1 transition-all"
          >
            <span className="material-symbols-outlined text-sm">close</span>
          </button>
        </div>

        {/* Output area */}
        <div
          ref={scrollRef}
          className="flex-1 overflow-y-auto p-4 font-mono text-[11px] leading-relaxed space-y-0.5"
        >
          {/* Welcome message */}
          {history.length === 0 && (
            <div className="text-on-surface-variant">
              <div className="text-primary font-bold mb-1">GREMLYN TERMINAL beta</div>
              <div>Type &apos;help&apos; for available commands. Press ` or Escape to close.</div>
            </div>
          )}

          {history.map((entry, i) => (
            <div key={i}>
              {entry.type === "input" && (
                <div className="text-on-surface-variant">
                  <span className="text-primary">gremlyn&gt;</span> {entry.text}
                </div>
              )}
              {entry.type === "output" && (
                <pre className="text-primary/80 whitespace-pre-wrap">{entry.text}</pre>
              )}
              {entry.type === "error" && (
                <pre className="text-error whitespace-pre-wrap">[ERROR] {entry.text}</pre>
              )}
            </div>
          ))}

          {executing && (
            <div className="text-on-surface-variant animate-pulse">processing...</div>
          )}
        </div>

        {/* Input */}
        <div className="flex items-center gap-2 px-4 py-3 border-t border-outline-variant/10 shrink-0 bg-black/50">
          <span className="font-mono text-[11px] text-primary font-bold shrink-0">gremlyn&gt;</span>
          <input
            ref={inputRef}
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="type a command..."
            disabled={executing}
            className="flex-1 bg-transparent border-0 outline-none font-mono text-[11px] text-on-surface placeholder:text-on-surface-variant/30 caret-primary disabled:opacity-50"
            autoComplete="off"
            spellCheck={false}
          />
        </div>
      </div>
    </div>
  );
}
