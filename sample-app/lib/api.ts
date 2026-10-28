// Gateway client. Sessions live in httpOnly cookies set by the gateway;
// this code never sees or stores a token. On an expired session it
// refreshes once and retries; errors surface as ApiError with a code.

export const GATEWAY = (process.env.NEXT_PUBLIC_GATEWAY_URL ?? "http://localhost:8080").replace(/\/$/, "");

type Lang = "en" | "fr" | "ar";
export type Messages = Record<Lang, string>;

export class ApiError extends Error {
  constructor(
    public code: string,
    public status: number,
    public messages: Messages,
  ) {
    super(messages.en);
  }
  /** The message in the reader's language, falling back to English. */
  get text(): string {
    return this.messages[language()] || this.messages.en;
  }
}

export const SESSION_ENDED = new Set(["AUTH_004", "AUTH_005", "AUTH_006", "AUTH_012"]);

const NETWORK: Messages = {
  en: "Can't reach the gateway. Check that it is running and try again.",
  fr: "Impossible de joindre la passerelle. Vérifiez qu'elle fonctionne et réessayez.",
  ar: "تعذر الوصول إلى البوابة. تحقق من أنها تعمل ثم حاول مرة أخرى.",
};

function language(): Lang {
  if (typeof navigator === "undefined") return "en";
  const l = navigator.language.slice(0, 2);
  return l === "fr" || l === "ar" ? l : "en";
}

let refreshing: Promise<boolean> | null = null;

function refreshSession(): Promise<boolean> {
  refreshing ??= fetch(`${GATEWAY}/api/v1/auth/refresh`, { method: "POST", credentials: "include" })
    .then((r) => r.ok)
    .catch(() => false)
    .finally(() => setTimeout(() => (refreshing = null), 0));
  return refreshing;
}

type Options = { method?: "GET" | "POST" | "PUT" | "DELETE"; body?: unknown };

export async function api<T = unknown>(path: string, opts: Options = {}): Promise<T> {
  const send = () =>
    fetch(`${GATEWAY}${path}`, {
      method: opts.method ?? "GET",
      credentials: "include",
      headers: opts.body !== undefined ? { "Content-Type": "application/json" } : undefined,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    });

  let res: Response;
  try {
    res = await send();
    if (res.status === 401 && !path.startsWith("/api/v1/auth/")) {
      const body = await res.clone().json().catch(() => ({}));
      if ((body.code === "AUTH_005" || body.code === "AUTH_004") && (await refreshSession())) {
        res = await send();
      }
    }
  } catch {
    throw new ApiError("NETWORK", 0, NETWORK);
  }

  const data = await res.json().catch(() => null);
  if (!res.ok) {
    if (data?.code && data?.message) throw new ApiError(data.code, res.status, data.message);
    throw new ApiError("GATEWAY_000", res.status, NETWORK);
  }
  return data as T;
}

export function errorText(err: unknown): string {
  return err instanceof ApiError ? err.text : "Something went wrong. Please try again.";
}
