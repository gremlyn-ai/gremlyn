"use client";

import Image from "next/image";
import type { GremlinInfo } from "@/lib/api/types";

type GremlinSelectorProps = {
  gremlins: GremlinInfo[];
  selected: string[];
  onSelectionChange: (selected: string[]) => void;
};

const SKELETON_COUNT = 4;

export function GremlinSelector({ gremlins, selected, onSelectionChange }: GremlinSelectorProps) {
  const activeCount = selected.length;

  function toggle(name: string) {
    if (selected.includes(name)) {
      onSelectionChange(selected.filter((s) => s !== name));
    } else {
      onSelectionChange([...selected, name]);
    }
  }

  const isLoading = gremlins.length === 0;

  return (
    <div className="col-span-12 lg:col-span-8 space-y-6">
      <div className="flex items-center justify-between">
        <h4 className="font-headline text-lg font-medium flex items-center gap-2">
          <span className="material-symbols-outlined text-secondary">psychology</span>
          SELECT CHAOS AGENT
        </h4>
        <span className="font-mono text-[10px] text-on-surface-variant px-2 py-0.5 bg-surface-container-high">
          {activeCount} LOADED
        </span>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {isLoading
          ? Array.from({ length: SKELETON_COUNT }).map((_, i) => (
              <div key={i} className="bg-surface-container-low p-5 animate-pulse">
                <div className="flex justify-between items-start mb-6">
                  <div className="w-16 h-16 bg-surface-container-highest" />
                  <div className="w-11 h-6 bg-surface-container-highest" />
                </div>
                <div className="h-4 w-32 bg-surface-container-highest mb-2" />
                <div className="h-3 w-full bg-surface-container-highest" />
              </div>
            ))
          : gremlins.map((g) => {
              const isSelected = selected.includes(g.name);
              return (
                <div
                  key={g.name}
                  className="bg-surface-container-low p-5 group hover:bg-surface-container-high transition-all border-l-2 border-transparent hover:border-secondary"
                >
                  <div className="flex justify-between items-start mb-6">
                    <div className="w-16 h-16 bg-surface-container-lowest relative overflow-hidden">
                      <Image
                        src="/gremlyn-white.png"
                        alt={g.name}
                        width={64}
                        height={64}
                        className="object-cover grayscale group-hover:grayscale-0 transition-all"
                      />
                      <div className="absolute inset-0 bg-secondary/5 opacity-0 group-hover:opacity-100" />
                    </div>

                    <button
                      onClick={() => toggle(g.name)}
                      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center transition-colors ${
                        isSelected
                          ? "bg-secondary/20"
                          : "bg-surface-container-highest"
                      }`}
                    >
                      <span
                        className={`inline-block h-5 w-5 transition-transform ${
                          isSelected
                            ? "translate-x-5 bg-secondary"
                            : "translate-x-0.5 bg-on-surface-variant"
                        }`}
                      />
                    </button>
                  </div>

                  <h5 className="font-headline font-bold text-on-surface group-hover:text-secondary transition-colors">
                    {g.name.toUpperCase()}
                  </h5>
                  <p className="text-xs text-on-surface-variant mt-2 leading-relaxed">
                    {g.description}
                  </p>
                </div>
              );
            })}
      </div>
    </div>
  );
}
