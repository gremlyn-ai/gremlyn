"use client";

export default function ArenaSessionsError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <div className="ml-64 min-h-screen bg-surface flex items-center justify-center">
      <div className="bg-surface-container-low p-12 border-l-2 border-secondary max-w-lg">
        <h2 className="font-headline text-2xl font-bold text-secondary mb-4 uppercase">
          ARENA_SESSIONS_ERROR
        </h2>
        <p className="font-mono text-xs text-on-surface-variant mb-6">
          {error.message}
        </p>
        <button
          onClick={reset}
          className="bg-secondary text-on-secondary px-6 py-3 font-mono font-bold text-xs hover:shadow-[0_0_10px_rgba(255,113,104,0.3)] transition-all"
        >
          RETRY_CONNECTION
        </button>
      </div>
    </div>
  );
}
