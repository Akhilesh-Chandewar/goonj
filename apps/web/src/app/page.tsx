import { APP_NAME, APP_TAGLINE } from "@goonj/shared";

export default function Home() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center bg-zinc-950 font-sans text-zinc-50">
      <main className="flex w-full max-w-3xl flex-col items-center gap-6 px-8 py-32 text-center">
        <span className="rounded-full border border-red-500/40 bg-red-500/10 px-4 py-1 text-sm font-medium text-red-400">
          🔴 audio-only · live-first
        </span>
        <h1 className="bg-gradient-to-r from-white via-zinc-200 to-zinc-400 bg-clip-text text-6xl font-bold tracking-tight text-transparent">
          {APP_NAME}
        </h1>
        <p className="max-w-md text-lg leading-8 text-zinc-400">{APP_TAGLINE}</p>
        <p className="max-w-md text-sm leading-6 text-zinc-500">
          Phase 0 scaffold is running. The Go API is wired at{" "}
          <code className="rounded bg-white/10 px-1.5 py-0.5 font-mono text-[0.9em]">
            NEXT_PUBLIC_API_URL
          </code>{" "}
          — auth, uploads, and the persistent player land in Phase 1, live
          audio in Phase 2.
        </p>
      </main>
    </div>
  );
}
