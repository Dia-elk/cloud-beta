export const GW = process.env.NEXT_PUBLIC_GATEWAY_URL || "http://localhost:8080";
export async function api(path: string, token: string, opts: RequestInit = {}) {
  const res = await fetch(`${GW}${path}`, { ...opts, headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}`, ...(opts.headers || {}) } });
  const text = await res.text();
  try { return { ok: res.ok, status: res.status, data: JSON.parse(text) }; } catch { return { ok: res.ok, status: res.status, data: { raw: text } }; }
}
