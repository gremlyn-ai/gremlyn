type StatusFooterProps = {
  shieldOnline?: boolean;
  arenaOnline?: boolean;
};

function systemStatus(shield?: boolean, arena?: boolean): { label: string; color: string } {
  if (shield !== false && arena !== false) return { label: "NOMINAL", color: "text-primary" };
  if (shield === false && arena === false) return { label: "OFFLINE", color: "text-error" };
  return { label: "DEGRADED", color: "text-secondary" };
}

export function StatusFooter({ shieldOnline, arenaOnline }: StatusFooterProps) {
  const status = systemStatus(shieldOnline, arenaOnline);

  return (
    <footer className="mt-auto bg-surface-container-lowest border-t border-outline-variant/10 px-8 py-3 flex justify-between items-center text-[10px] font-mono text-on-surface-variant">
      <div className="flex gap-6">
        <span>
          SYSTEM_STATUS: <span className={`${status.color} font-bold`}>{status.label}</span>
        </span>
        <span>
          LATENCY: <span className="text-primary font-bold">12ms</span>
        </span>
        <span>
          UPLINK: <span className="text-primary font-bold">ENCRYPTED_AES256</span>
        </span>
      </div>
      <div className="flex gap-4 items-center">
        <span>THREAD_POOL: 2,492/4,000</span>
        <div className="w-32 h-1 bg-surface-container-high overflow-hidden">
          <div className="bg-primary h-full w-3/5" />
        </div>
      </div>
    </footer>
  );
}
