import type { ShieldEvent } from "@/lib/api/types";

type ThreatChartProps = {
  events: ShieldEvent[];
};

const DAY_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

function aggregateByDay(events: ShieldEvent[]): { day: string; count: number }[] {
  const counts: Record<number, number> = {};

  for (const e of events) {
    const d = new Date(e.timestamp || e.created_at || "");
    if (!isNaN(d.getTime())) {
      const dayOfWeek = d.getDay();
      // Convert JS day (0=Sun) to Mon-based index
      const idx = dayOfWeek === 0 ? 6 : dayOfWeek - 1;
      counts[idx] = (counts[idx] ?? 0) + 1;
    }
  }

  return DAY_LABELS.map((day, i) => ({ day, count: counts[i] ?? 0 }));
}

export function ThreatChart({ events }: ThreatChartProps) {
  const bars = aggregateByDay(events);
  const maxCount = Math.max(...bars.map((b) => b.count), 1);
  const totalBlocked = events.filter((e) => e.action_taken === "blocked").length;
  const avgPerDay = events.length > 0 ? Math.round(events.length / 7) : 0;

  return (
    <div className="bg-surface-container-low p-8 flex flex-col">
      <div className="flex justify-between items-center mb-6">
        <h4 className="font-headline text-lg font-bold uppercase tracking-tight">
          Threat_Vectors_7D
        </h4>
        <span className="material-symbols-outlined text-primary">monitoring</span>
      </div>

      <div className="flex-1 flex items-end justify-between gap-1 h-48 mb-6">
        {bars.map((bar) => {
          const height = bar.count > 0 ? Math.max((bar.count / maxCount) * 100, 4) : 4;
          return (
            <div
              key={bar.day}
              className="w-full group relative"
              style={{ height: `${height}%` }}
            >
              <div className="absolute inset-x-0 bottom-0 bg-primary group-hover:bg-tertiary transition-colors h-full" />
              {bar.count > 0 && (
                <div className="absolute -top-5 left-1/2 -translate-x-1/2 text-[9px] font-mono text-on-surface-variant opacity-0 group-hover:opacity-100 transition-opacity">
                  {bar.count}
                </div>
              )}
            </div>
          );
        })}
      </div>

      <div className="flex justify-between font-mono text-[9px] text-on-surface-variant uppercase tracking-widest px-1">
        {bars.map((bar) => (
          <span key={bar.day}>{bar.day}</span>
        ))}
      </div>

      <div className="mt-6 pt-6 border-t border-outline-variant/10">
        <div className="flex justify-between items-center text-sm">
          <span className="text-on-surface-variant">Weekly Avg</span>
          <span className="font-mono text-primary font-bold">
            {avgPerDay > 0 ? `${avgPerDay} blocks/day` : "No data"}
          </span>
        </div>
      </div>
    </div>
  );
}
