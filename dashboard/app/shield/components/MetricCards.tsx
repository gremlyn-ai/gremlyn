type MetricCardsProps = {
  blockedCount: number | null;
  serverCount: number | null;
  uptime: string | null;
  alertCounts: Record<string, number> | null;
};

type MetricCard = {
  label: string;
  value: string;
  suffix?: string;
  icon: string;
  borderColor: string;
  iconColor: string;
};

function formatCount(n: number): string {
  return n >= 1_000 ? `${(n / 1_000).toFixed(1)}k` : String(n);
}

export function MetricCards({ blockedCount, serverCount, uptime, alertCounts }: MetricCardsProps) {
  const totalAlerts = alertCounts
    ? Object.values(alertCounts).reduce((sum, v) => sum + v, 0)
    : null;

  const cards: MetricCard[] = [
    {
      label: "Blocked (24h)",
      value: blockedCount != null ? formatCount(blockedCount) : "---",
      icon: "security",
      borderColor: "border-primary",
      iconColor: "text-primary",
    },
    {
      label: "Alerts",
      value: totalAlerts != null ? formatCount(totalAlerts) : "---",
      icon: "notifications",
      borderColor: "border-tertiary",
      iconColor: "text-tertiary",
    },
    {
      label: "Servers",
      value: serverCount != null ? String(serverCount) : "---",
      suffix: "nodes",
      icon: "dns",
      borderColor: "border-outline-variant",
      iconColor: "text-on-surface-variant",
    },
    {
      label: "Uptime",
      value: uptime ?? "---",
      icon: "timer",
      borderColor: "border-primary",
      iconColor: "text-primary",
    },
  ];

  return (
    <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
      {cards.map((m) => (
        <div
          key={m.label}
          className={`bg-surface-container-low p-6 border-l-2 ${m.borderColor}`}
        >
          <div className="flex justify-between items-start mb-4">
            <span className="text-[10px] font-mono text-on-surface-variant uppercase tracking-widest">
              {m.label}
            </span>
            <span
              className={`material-symbols-outlined ${m.iconColor} text-sm`}
              style={
                m.icon === "timer"
                  ? { fontVariationSettings: "'FILL' 1" }
                  : undefined
              }
            >
              {m.icon}
            </span>
          </div>
          <div className="flex items-end gap-2">
            <span className={`text-4xl font-headline font-bold leading-none ${m.value === "---" ? "text-on-surface-variant animate-pulse" : "text-on-surface"}`}>
              {m.value}
            </span>
            {m.suffix && (
              <span className="text-xs font-mono mb-1 text-on-surface-variant">
                {m.suffix}
              </span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
