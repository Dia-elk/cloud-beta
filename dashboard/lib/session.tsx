"use client";

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { api, ApiError, SESSION_ENDED } from "./api";
import type { Company, Me, Op, Policy } from "./types";

type Session = {
  me: Me;
  policy: Policy;
  companies: Company[];
  can: (table: string, op: Op) => boolean;
  switchCompany: (id: string) => Promise<void>;
  signOut: () => Promise<void>;
};

const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const s = useContext(SessionContext);
  if (!s) throw new Error("useSession outside SessionProvider");
  return s;
}

/** Loads the signed-in user, or sends them to sign in. */
export function SessionProvider({ children, fallback }: { children: ReactNode; fallback: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [state, setState] = useState<{ me: Me; policy: Policy; companies: Company[] } | null>(null);
  const [error, setError] = useState<ApiError | null>(null);

  const load = useCallback(async () => {
    try {
      const [me, policy, { companies }] = await Promise.all([
        api<Me>("/api/v1/me"),
        api<Policy>("/api/v1/policy"),
        api<{ companies: Company[] }>("/api/v1/companies"),
      ]);
      setState({ me, policy, companies });
      setError(null);
    } catch (e) {
      if (e instanceof ApiError && SESSION_ENDED.has(e.code)) {
        router.replace(`/login?next=${encodeURIComponent(pathname)}`);
        return;
      }
      setError(e instanceof ApiError ? e : null);
    }
  }, [router, pathname]);

  useEffect(() => {
    load();
  }, [load]);

  const switchCompany = useCallback(
    async (id: string) => {
      await api("/api/v1/auth/switch-company", { method: "POST", body: { company_id: id } });
      setState(null);
      await load();
      router.refresh();
    },
    [load, router],
  );

  const signOut = useCallback(async () => {
    await api("/api/v1/auth/logout", { method: "POST" }).catch(() => {});
    router.replace("/login");
  }, [router]);

  if (!state) {
    if (error) {
      return (
        <div className="grid min-h-dvh place-items-center p-6 text-center">
          <div>
            <p className="font-medium">The console could not load.</p>
            <p className="mt-1 text-fg-muted">{error.text}</p>
            <button className="focus-ring mt-4 rounded-lg border border-border px-3 py-1.5" onClick={load}>
              Try again
            </button>
          </div>
        </div>
      );
    }
    return <>{fallback}</>;
  }

  const can = (table: string, op: Op) => state.policy.tables[table]?.[op]?.includes(state.me.role) ?? false;

  return <SessionContext.Provider value={{ ...state, can, switchCompany, signOut }}>{children}</SessionContext.Provider>;
}

/** Loads a gateway resource for the current page, with reload. */
export function useResource<T>(path: string | null) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    if (!path) return;
    setLoading(true);
    try {
      setData(await api<T>(path));
      setError(null);
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError("GATEWAY_000", 500, { en: "Something went wrong.", fr: "Une erreur est survenue.", ar: "حدث خطأ." }));
    } finally {
      setLoading(false);
    }
  }, [path]);

  useEffect(() => {
    reload();
  }, [reload]);

  return { data, error, loading, reload };
}
