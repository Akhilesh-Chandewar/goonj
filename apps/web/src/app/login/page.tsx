"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { api, saveTokens } from "@/lib/api";

type Mode = "login" | "register";

export default function LoginPage() {
  const router = useRouter();
  const [mode, setMode] = useState<Mode>("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [username, setUsername] = useState("");
  const [asCreator, setAsCreator] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      if (mode === "register") {
        const res = await api("/auth/register", {
          method: "POST",
          body: JSON.stringify({
            email,
            password,
            username: username.toLowerCase(),
            as_creator: asCreator,
          }),
        });
        saveTokens((res as { tokens: { access_token: string; refresh_token: string; token_type: string; expires_in: number } }).tokens);
      } else {
        const res = await api("/auth/login", {
          method: "POST",
          body: JSON.stringify({ email, password }),
        });
        saveTokens((res as { tokens: { access_token: string; refresh_token: string; token_type: string; expires_in: number } }).tokens);
      }
      router.push(asCreator && mode === "register" ? "/studio/live" : "/live");
    } catch (e) {
      const msg = (e as Error).message;
      if (msg.includes("already registered")) {
        setError("That email already has an account — switch to Log in below.");
        setMode("login");
      } else {
        setError(msg);
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="flex min-h-screen items-center justify-center bg-zinc-950 px-6 text-zinc-50">
      <div className="w-full max-w-md rounded-2xl border border-zinc-800 bg-zinc-900 p-8">
        <h1 className="text-2xl font-bold">
          {mode === "login" ? "Welcome back" : "Join Goonj"}
        </h1>
        <p className="mt-1 text-sm text-zinc-400">
          {mode === "login"
            ? "Log in to listen and chat."
            : "Create an account — optionally as a creator."}
        </p>

        <div className="mt-6 space-y-4">
          {mode === "register" && (
            <div>
              <input
                value={username}
                onChange={(e) =>
                  setUsername(e.target.value.replace(/[^a-z0-9_]/g, "").slice(0, 30))
                }
                placeholder="username (a-z, 0-9, underscore)"
                className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
              />
              {username.length > 0 && username.length < 3 && (
                <p className="mt-1 text-xs text-zinc-500">
                  At least 3 characters.
                </p>
              )}
            </div>
          )}
          <input
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="email"
            type="email"
            className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
          />
          <input
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="password (8+ chars)"
            type="password"
            className="w-full rounded-lg bg-zinc-950 px-4 py-2.5 outline-none ring-zinc-700 focus:ring-2 focus:ring-red-500"
          />

          {mode === "register" && (
            <label className="flex items-center gap-2 text-sm text-zinc-300">
              <input
                type="checkbox"
                checked={asCreator}
                onChange={(e) => setAsCreator(e.target.checked)}
                className="h-4 w-4 accent-red-600"
              />
              Create a creator channel (can go live 🔴)
            </label>
          )}

          {error && <p className="text-sm text-red-400">{error}</p>}

          <button
            onClick={submit}
            disabled={
              busy ||
              !email ||
              !password ||
              (mode === "register" && username.length < 3)
            }
            className="w-full rounded-full bg-red-600 py-3 font-semibold hover:bg-red-500 disabled:opacity-40"
          >
            {busy ? "…" : mode === "login" ? "Log in" : "Create account"}
          </button>

          <p className="text-center text-sm text-zinc-400">
            {mode === "login" ? "New here?" : "Already have an account?"}{" "}
            <button
              onClick={() => setMode(mode === "login" ? "register" : "login")}
              className="text-red-400 underline"
            >
              {mode === "login" ? "Register" : "Log in"}
            </button>
          </p>
        </div>
      </div>
    </main>
  );
}
