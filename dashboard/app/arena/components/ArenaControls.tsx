"use client";

import { useState } from "react";

type ArenaControlsProps = {
  servers: string[];
  selectedServer: string;
  onServerChange: (server: string) => void;
  intensity: number;
  onIntensityChange: (n: number) => void;
};

export function ArenaControls({
  servers,
  selectedServer,
  onServerChange,
  intensity,
  onIntensityChange,
}: ArenaControlsProps) {
  const [customMode, setCustomMode] = useState(false);

  return (
    <div className="col-span-12 lg:col-span-4 space-y-6">
      {/* Target Agent */}
      <div className="bg-surface-container-low p-6 space-y-4">
        <div className="flex justify-between items-center">
          <label className="font-headline text-sm font-bold uppercase tracking-widest text-secondary block">
            Target Agent
          </label>
          <button
            onClick={() => {
              setCustomMode(!customMode);
              onServerChange("");
            }}
            className="font-mono text-[10px] text-on-surface-variant hover:text-secondary hover:bg-surface-container-high px-2 py-1 transition-all"
          >
            {customMode ? "FROM_SHIELD" : "MANUAL_INPUT"}
          </button>
        </div>
        {customMode ? (
          <input
            type="text"
            className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-4 placeholder:text-on-surface-variant/30 focus:ring-1 focus:ring-secondary"
            placeholder="ENTER_SERVER_ID..."
            value={selectedServer}
            onChange={(e) => onServerChange(e.target.value)}
          />
        ) : (
          <div className="relative">
            <select
              className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-4 appearance-none focus:ring-1 focus:ring-secondary"
              value={selectedServer}
              onChange={(e) => onServerChange(e.target.value)}
            >
              <option value="">SELECT_TARGET...</option>
              {servers.map((s) => (
                <option key={s} value={s}>
                  {s.toUpperCase()}
                </option>
              ))}
            </select>
            <div className="absolute right-4 top-1/2 -translate-y-1/2 pointer-events-none">
              <span className="material-symbols-outlined text-on-surface-variant">
                expand_more
              </span>
            </div>
          </div>
        )}
      </div>

      {/* Intensity Slider */}
      <div className="bg-surface-container-low p-6 space-y-6">
        <div className="flex justify-between items-center">
          <label className="font-headline text-sm font-bold uppercase tracking-widest text-secondary block">
            Intensity Slider
          </label>
          <span className="font-mono text-xl text-secondary font-bold">
            {String(intensity).padStart(2, "0")}
          </span>
        </div>
        <input
          type="range"
          min="1"
          max="10"
          value={intensity}
          onChange={(e) => onIntensityChange(Number(e.target.value))}
          className="w-full h-1 bg-surface-container-highest appearance-none cursor-pointer accent-secondary"
        />
        <div className="flex justify-between font-mono text-[10px] text-on-surface-variant">
          <span>MIN_STABLE</span>
          <span>MAX_CHAOS</span>
        </div>
      </div>

      {/* Warning box */}
      <div className="bg-secondary/5 border border-secondary/10 p-6">
        <div className="flex items-center gap-3 text-secondary mb-2">
          <span className="material-symbols-outlined text-sm">warning</span>
          <span className="font-headline font-bold text-xs uppercase tracking-widest">
            Warning
          </span>
        </div>
        <p className="text-[11px] font-mono text-on-surface-variant leading-relaxed">
          High intensity levels (&gt;08) may cause permanent node desynchronization
          within the sandbox environment. Proceed with extreme caution.
        </p>
      </div>
    </div>
  );
}
