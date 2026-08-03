"use client";

export default function AboutError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <div className="ml-64 min-h-screen bg-surface flex items-center justify-center">
      <div className="bg-surface-container-low p-12 border-l-2 border-error max-w-lg">
        <h2 className="font-headline text-2xl font-bold text-error mb-4 uppercase">
          ABOUT_ERROR
        </h2>
        <p className="font-mono text-xs text-on-surface-variant mb-6">
          {error.message}
        </p>
        <button
          onClick={reset}
          className="bg-primary text-on-primary px-6 py-3 font-mono font-bold text-xs hover:shadow-[0_0_10px_rgba(142,255,113,0.3)] transition-all"
        >
          RETRY_CONNECTION
        </button>
      </div>
    </div>
  );
}
