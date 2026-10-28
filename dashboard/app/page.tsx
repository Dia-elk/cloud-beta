"use client";
import { useEffect, useState } from "react";
import { api } from "../lib/api";

type Summary = { total: number; allowed: number; denied: number; avg_latency_ms: number; per_day: { date: string; total: number; allowed: number; denied: number }[] };

function fmt(r: { status: number; data: any }) {
  try {
    return `${r.status} ${JSON.stringify(r.data)}`;
  } catch {
    return `${r.status}`;
  }
}

function statusPill(status: string) {
  const s = (status || "").toLowerCase();
  if (s === "running") return "bg-emerald-100 text-emerald-700 ring-1 ring-emerald-200";
  if (s === "deploying") return "bg-amber-100 text-amber-700 ring-1 ring-amber-200";
  if (s === "failed") return "bg-red-100 text-red-700 ring-1 ring-red-200";
  return "bg-zinc-100 text-zinc-700 ring-1 ring-zinc-200";
}

export default function Home() {
  const [token, setToken] = useState("");
  const [email, setEmail] = useState("owner@example.ma");
  const [password, setPassword] = useState("beta-password-1");
  const [company, setCompany] = useState("Beta Company");
  const [summary, setSummary] = useState<Summary | null>(null);
  const [audit, setAudit] = useState<any[]>([]);
  const [apps, setApps] = useState<any[]>([]);
  const [clients, setClients] = useState<any[]>([]);
  const [proposals, setProposals] = useState<any[]>([]);
  const [policy, setPolicy] = useState<any | null>(null);
  const [policyError, setPolicyError] = useState("");
  const [shares, setShares] = useState<any[]>([]);
  const [sharesError, setSharesError] = useState("");
  const [msg, setMsg] = useState("");
  const [appsMsg, setAppsMsg] = useState("");
  const [healMsg, setHealMsg] = useState("");
  const [sharesMsg, setSharesMsg] = useState("");
  const [newAppName, setNewAppName] = useState("");
  const [selectedClientId, setSelectedClientId] = useState("");
  const [expiry, setExpiry] = useState("1h");
  const [createdShare, setCreatedShare] = useState<any | null>(null);

  async function load(t: string) {
    const s = await api("/api/v1/audit/summary", t);
    if (s.ok) setSummary(s.data as Summary);
    const a = await api("/api/v1/audit?limit=50", t);
    if (a.ok) setAudit((a.data as any).audit || []);
    const ap = await api("/api/v1/apps", t);
    if (ap.ok) setApps((ap.data as any).apps || []);
    else setAppsMsg(`Load apps failed: ${fmt(ap)}`);
    const cl = await api("/api/v1/clients", t);
    if (cl.ok) setClients((cl.data as any).clients || []);
    const hp = await api("/api/v1/heal/proposals", t);
    if (hp.ok) setProposals((hp.data as any).proposals || []);
    const pol = await api("/api/v1/policy", t);
    if (pol.ok) {
      setPolicy(pol.data);
      setPolicyError("");
    } else {
      setPolicy(null);
      setPolicyError(`Policy could not be loaded: ${fmt(pol)}`);
    }
    const sh = await api("/api/v1/shares", t);
    if (sh.ok) {
      setShares((sh.data as any).shares || []);
      setSharesError("");
    } else {
      setShares([]);
      setSharesError(`Shares could not be loaded: ${fmt(sh)}`);
    }
  }
  useEffect(() => {
    const t = localStorage.getItem("cloud_beta_token");
    if (t) {
      setToken(t);
      load(t);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function register() {
    const r = await api("/api/v1/auth/register", "", { method: "POST", body: JSON.stringify({ email, password, company_name: company }) });
    if (r.ok && (r.data as any).token) {
      localStorage.setItem("cloud_beta_token", (r.data as any).token);
      setToken((r.data as any).token);
      setMsg("Registered. Beta data only — this is not production.");
      load((r.data as any).token);
    } else setMsg(fmt(r));
  }
  async function login() {
    const r = await api("/api/v1/auth/login", "", { method: "POST", body: JSON.stringify({ email, password }) });
    if (r.ok && (r.data as any).token) {
      localStorage.setItem("cloud_beta_token", (r.data as any).token);
      setToken((r.data as any).token);
      load((r.data as any).token);
    } else setMsg(fmt(r));
  }
  async function approve(id: string) {
    const r = await api(`/api/v1/heal/${id}/approve`, token, { method: "POST" });
    setHealMsg(`Approve: ${fmt(r)} — approval alone never applies a fix.`);
    load(token);
  }
  async function applyHeal(id: string) {
    const r = await api(`/api/v1/heal/${id}/apply`, token, { method: "POST" });
    if (r.ok) {
      const d: any = r.data;
      setHealMsg(`Apply: ${fmt(r)} — applied: ${String(d.applied)}${d.applied_columns ? ` · applied_columns: ${JSON.stringify(d.applied_columns)}` : ""}${d.columns_after ? ` · columns_after: ${JSON.stringify(d.columns_after)}` : ""}`);
    } else if (r.status === 409) {
      setHealMsg(`Apply failed: ${fmt(r)} — approve first; nothing changed.`);
    } else if (r.status === 400) {
      setHealMsg(`Apply failed: ${fmt(r)} — preview only, not safely applyable; nothing changed.`);
    } else {
      setHealMsg(`Apply failed: ${fmt(r)} — nothing changed.`);
    }
    load(token);
  }

  async function createApp() {
    const name = newAppName.trim();
    if (!name) {
      setAppsMsg("Enter an app name first.");
      return;
    }
    const r = await api("/api/v1/apps", token, { method: "POST", body: JSON.stringify({ name, repo: "sample-app" }) });
    setAppsMsg(r.ok ? `Create app: ${fmt(r)}` : `Create app failed: ${fmt(r)}`);
    if (r.ok) setNewAppName("");
    load(token);
  }
  async function deployApp(id: string) {
    const r = await api(`/api/v1/apps/${id}/deploy`, token, { method: "POST" });
    setAppsMsg(r.ok ? `Deploy: ${fmt(r)}` : `Deploy failed: ${fmt(r)}`);
    load(token);
  }
  async function stopApp(id: string) {
    const r = await api(`/api/v1/apps/${id}/stop`, token, { method: "POST" });
    setAppsMsg(r.ok ? `Stop: ${fmt(r)}` : `Stop failed: ${fmt(r)}`);
    load(token);
  }
  async function deleteApp(id: string, name: string) {
    if (!window.confirm(`Delete app "${name}"? This cannot be undone from here.`)) return;
    const r = await api(`/api/v1/apps/${id}`, token, { method: "DELETE" });
    setAppsMsg(r.ok ? `Delete: ${fmt(r)}` : `Delete failed: ${fmt(r)} — the app was not deleted if the gateway rejected it.`);
    load(token);
  }

  async function createShare() {
    const rowId = selectedClientId || "";
    if (!rowId) {
      setSharesMsg("Select a client first.");
      return;
    }
    const r = await api("/api/v1/shares", token, { method: "POST", body: JSON.stringify({ row_id: rowId, expires_in: expiry }) });
    if (r.ok) {
      setCreatedShare(r.data);
      setSharesMsg(`Share created: ${fmt(r)}`);
    } else {
      setCreatedShare(null);
      setSharesMsg(`Create share failed: ${fmt(r)} — no share was created if the gateway rejected it.`);
    }
    load(token);
  }
  async function revokeShare(id: string) {
    const r = await api(`/api/v1/shares/${id}/revoke`, token, { method: "POST" });
    setSharesMsg(r.ok ? `Revoke: ${fmt(r)}` : `Revoke failed: ${fmt(r)}`);
    load(token);
  }

  if (!token)
    return (
      <main className="mx-auto flex min-h-screen w-full max-w-7xl flex-col justify-center px-4 py-10 sm:px-6">
        <div className="mx-auto w-full max-w-xl">
          <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-zinc-500">Cloud Beta · Console</p>
          <h1 className="mt-3 text-3xl font-bold leading-tight tracking-tight text-zinc-900 sm:text-4xl">From shadow app to governed app</h1>
          <p className="mt-3 text-[15px] leading-relaxed text-zinc-600">Beta only. Analytics below come from real audit rows in Postgres — no invented sample data at runtime.</p>
          <div className="glass mt-8 space-y-3 rounded-3xl p-5 sm:p-6">
            <label className="block text-xs font-semibold text-zinc-700">
              <span className="mb-1 block">Email</span>
              <input className="glass-input w-full rounded-xl px-3 py-2.5 text-sm text-zinc-900" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" />
            </label>
            <label className="block text-xs font-semibold text-zinc-700">
              <span className="mb-1 block">Password</span>
              <input className="glass-input w-full rounded-xl px-3 py-2.5 text-sm text-zinc-900" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password (min 8 chars)" />
            </label>
            <label className="block text-xs font-semibold text-zinc-700">
              <span className="mb-1 block">Company / workspace</span>
              <input className="glass-input w-full rounded-xl px-3 py-2.5 text-sm text-zinc-900" value={company} onChange={(e) => setCompany(e.target.value)} placeholder="Company / workspace" />
            </label>
            <div className="flex flex-wrap gap-2 pt-1">
              <button className="rounded-full bg-gradient-to-b from-[#ff4a3d] to-[#e81f14] px-5 py-2.5 text-sm font-semibold text-white shadow-sm" onClick={register}>Register</button>
              <button className="glass rounded-full px-5 py-2.5 text-sm font-semibold text-zinc-900" onClick={login}>Sign in</button>
            </div>
            {msg && <p className="break-words text-xs leading-relaxed text-zinc-600">{msg}</p>}
          </div>
          <p className="mt-4 text-center text-xs text-zinc-500">Shadow app → governed · Permission decision · Block a leak · Break &amp; heal · Revoke</p>
        </div>
      </main>
    );

  const widgets = [
    { label: "Requests", value: summary?.total ?? 0 },
    { label: "Allowed", value: summary?.allowed ?? 0 },
    { label: "Denied", value: summary?.denied ?? 0 },
    { label: "Avg latency", value: `${Math.round(summary?.avg_latency_ms ?? 0)} ms` },
    { label: "Governed apps", value: apps.length },
    { label: "Clients", value: clients.length },
  ];

  const policyTables: [string, any][] = policy && policy.tables && typeof policy.tables === "object" ? Object.entries(policy.tables) : [];
  const roleActions = (table: any, role: string) => {
    if (!table) return "—";
    const acts = ["read", "write", "delete"].filter((a) => Array.isArray(table[a]) && table[a].includes(role));
    return acts.length ? acts.join(" · ") : "—";
  };

  return (
    <main className="mx-auto w-full max-w-7xl px-4 pb-10 sm:px-6">
      <header className="glass-strong sticky top-3 z-10 mt-3 flex flex-wrap items-center justify-between gap-3 rounded-3xl px-4 py-3 sm:px-5">
        <div className="flex min-w-0 items-center gap-3">
          <span aria-hidden="true" className="flex h-9 w-9 shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br from-[#ff4a3d] to-[#e81f14] text-sm font-black text-white shadow-sm">◆</span>
          <div className="min-w-0">
            <p className="truncate text-[11px] font-semibold uppercase tracking-[0.18em] text-zinc-500">Cloud Beta · Monitor / Govern / Operate</p>
            <h1 className="truncate text-lg font-bold leading-tight tracking-tight text-zinc-900">Console <span className="text-laravel">— real audit rows</span></h1>
          </div>
        </div>
        <nav aria-label="Console sections" className="flex flex-wrap items-center gap-1.5 text-xs font-semibold">
          <a className="rounded-full bg-zinc-900/5 px-3 py-1.5 text-zinc-700" href="#analytics">Monitor</a>
          <a className="rounded-full bg-zinc-900/5 px-3 py-1.5 text-zinc-700" href="#apps">Apps</a>
          <a className="rounded-full bg-zinc-900/5 px-3 py-1.5 text-zinc-700" href="#permissions">Permissions</a>
          <a className="rounded-full bg-zinc-900/5 px-3 py-1.5 text-zinc-700" href="#shares">Shares</a>
          <a className="rounded-full bg-zinc-900/5 px-3 py-1.5 text-zinc-700" href="#heal">Operate</a>
          <button className="glass rounded-full px-3 py-1.5 text-zinc-900" onClick={() => { localStorage.removeItem("cloud_beta_token"); setToken(""); }}>Sign out</button>
        </nav>
      </header>

      <section aria-label="90-second pitch flow" className="glass mt-4 rounded-3xl p-4 sm:p-5">
        <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-zinc-500">90-second pitch flow</p>
        <ol className="mt-3 flex flex-col gap-2 text-sm font-medium text-zinc-800 sm:flex-row sm:flex-wrap sm:items-center">
          <li className="rounded-full bg-white/80 px-3 py-1.5 ring-1 ring-zinc-900/10">1 · Deploy a shadow app</li>
          <li aria-hidden="true" className="hidden text-zinc-400 sm:inline">→</li>
          <li className="rounded-full bg-white/80 px-3 py-1.5 ring-1 ring-zinc-900/10">2 · See who sees what</li>
          <li aria-hidden="true" className="hidden text-zinc-400 sm:inline">→</li>
          <li className="rounded-full bg-red-50 px-3 py-1.5 text-red-700 ring-1 ring-red-200">3 · Block a leak</li>
          <li aria-hidden="true" className="hidden text-zinc-400 sm:inline">→</li>
          <li className="rounded-full bg-white/80 px-3 py-1.5 ring-1 ring-zinc-900/10">4 · Break &amp; heal</li>
          <li aria-hidden="true" className="hidden text-zinc-400 sm:inline">→</li>
          <li className="rounded-full bg-white/80 px-3 py-1.5 ring-1 ring-zinc-900/10">5 · Revoke</li>
        </ol>
        <p className="mt-3 text-xs leading-relaxed text-zinc-600">From shadow app to governed app in one click — permission is enforced at the database, not trusted to app code.</p>
      </section>

      <section id="analytics" aria-label="Small analytics" className="mt-4 grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
        {widgets.map((w) => (
          <div key={w.label} className="glass rounded-2xl p-4">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-zinc-500">{w.label}</p>
            <p className={`mt-1 text-[22px] font-bold leading-tight tracking-tight ${w.label === "Denied" ? "text-red-600" : w.label === "Allowed" ? "text-emerald-600" : "text-zinc-900"}`}>{w.value}</p>
          </div>
        ))}
      </section>

      <div className="mt-4 grid gap-4 md:grid-cols-2">
        <section className="glass rounded-3xl p-4 sm:p-5">
          <h2 className="text-sm font-bold tracking-tight text-zinc-900">Per day (last 7 days, from audit)</h2>
          <div className="mt-3 space-y-2">
            {(summary?.per_day || []).map((d) => (
              <div key={d.date} className="flex flex-wrap justify-between gap-2 rounded-xl bg-white/70 px-3 py-2 text-xs text-zinc-700 ring-1 ring-zinc-900/5"><span className="font-semibold">{d.date}</span><span>total {d.total} · allowed {d.allowed} · <span className="font-semibold text-red-600">denied {d.denied}</span></span></div>
            ))}
            {(summary?.per_day || []).length === 0 && <p className="text-xs leading-relaxed text-zinc-500">No audit rows yet — make a request, then refresh.</p>}
          </div>
        </section>

        <section id="permissions" className="glass rounded-3xl p-4 sm:p-5">
          <h2 className="text-sm font-bold tracking-tight text-zinc-900">Permissions — workspace-wide policy</h2>
          <p className="mt-2 text-xs leading-relaxed text-zinc-600">This is the workspace-wide policy, applied to every app. It is not a separate policy per app in MVP v1.</p>
          {policy ? (
            <>
              <p className="mt-3 text-xs leading-relaxed text-zinc-700">
                Tenant field: <b>{policy.tenant_field || "company_id"}</b>
                {policy.source ? <> · source: <b>{policy.source}</b></> : null}
                {policy.default ? <> · default: <b>{policy.default}</b></> : null}
              </p>
              <p className="mt-1 text-xs leading-relaxed text-zinc-700">A user only sees rows where company_id matches their own company. Anything else is denied by default.</p>
              {policyTables.length > 0 ? (
                <div className="mt-3 overflow-x-auto">
                  <table className="w-full min-w-[480px] text-left text-xs leading-relaxed">
                    <thead><tr className="text-zinc-500"><th className="pb-2 pr-3 font-semibold">Table</th><th className="pb-2 pr-3 font-semibold">Owner</th><th className="pb-2 pr-3 font-semibold">Employee</th><th className="pb-2 font-semibold">Accountant</th></tr></thead>
                    <tbody>
                      {policyTables.map(([name, t]) => (
                        <tr key={name} className="border-t border-zinc-900/10">
                          <td className="py-2 pr-3 font-semibold text-zinc-900">{name}{t?.tenant_field ? <span className="block font-normal text-zinc-500">tenant: {t.tenant_field}</span> : null}</td>
                          <td className="py-2 pr-3 text-zinc-700">{roleActions(t, "owner")}</td>
                          <td className="py-2 pr-3 text-zinc-700">{roleActions(t, "employee")}</td>
                          <td className="py-2 text-zinc-700">{roleActions(t, "accountant")}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <p className="mt-3 break-words text-xs leading-relaxed text-zinc-600">{JSON.stringify(policy)}</p>
              )}
            </>
          ) : (
            <p className="mt-3 break-words text-xs leading-relaxed text-zinc-600">{policyError || "Policy not loaded yet."}</p>
          )}
        </section>
      </div>

      <section id="apps" className="glass mt-4 rounded-3xl p-4 sm:p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-bold tracking-tight text-zinc-900">Apps</h2>
          <button className="glass rounded-full px-3 py-1.5 text-xs font-semibold text-zinc-900" onClick={() => load(token)}>Refresh</button>
        </div>
        <div className="mt-3 flex flex-col gap-2 sm:flex-row">
          <label className="block flex-1 text-xs font-semibold text-zinc-700">
            <span className="mb-1 block">App name</span>
            <input className="glass-input w-full rounded-xl px-3 py-2.5 text-sm text-zinc-900" value={newAppName} onChange={(e) => setNewAppName(e.target.value)} placeholder="e.g. Client Tracker" />
          </label>
          <div className="flex items-end">
            <button className="rounded-full bg-gradient-to-b from-[#ff4a3d] to-[#e81f14] px-5 py-2.5 text-sm font-semibold text-white shadow-sm" onClick={createApp}>Create app</button>
          </div>
        </div>
        <p className="mt-2 text-xs leading-relaxed text-zinc-600">Client Tracker template — the only template in MVP v1; git repos are not deployed yet. Creating an app registers it; use Deploy to run it.</p>
        <ul className="mt-3 space-y-2 text-xs leading-relaxed">
          {apps.map((a) => (
            <li key={a.id} className="rounded-2xl bg-white/70 p-3 ring-1 ring-zinc-900/5">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="font-bold text-zinc-900">{a.name}</span>
                <span className={`rounded-full px-2.5 py-1 text-[11px] font-semibold ${statusPill(a.status)}`}>{a.status || "unknown"}</span>
              </div>
              <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-zinc-600">
                <span>port: {a.port ?? "—"}</span>
                {a.container_id ? <span className="break-all">container: {a.container_id}</span> : null}
                {a.repo ? <span className="break-all">repo: {a.repo}</span> : null}
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-2">
                {a.url && a.status === "running" ? <a className="rounded-full bg-zinc-900 px-3 py-1.5 font-semibold text-white" href={a.url} target="_blank" rel="noreferrer">Open app</a> : null}
                <button className="rounded-full bg-gradient-to-b from-[#ff4a3d] to-[#e81f14] px-3 py-1.5 font-semibold text-white shadow-sm" onClick={() => deployApp(a.id)}>Deploy</button>
                <button className="glass rounded-full px-3 py-1.5 font-semibold text-zinc-900" onClick={() => stopApp(a.id)}>Stop</button>
                <button className="glass rounded-full px-3 py-1.5 font-semibold text-red-700" onClick={() => deleteApp(a.id, a.name)}>Delete</button>
              </div>
            </li>
          ))}
          {apps.length === 0 && <li className="text-zinc-500">No apps yet. Use the form above to create your first app from the Client Tracker template, then Deploy it.</li>}
        </ul>
        {appsMsg && <p className="mt-3 break-words text-xs leading-relaxed text-zinc-600">{appsMsg}</p>}
      </section>

      <div className="mt-4 grid gap-4 md:grid-cols-2">
        <section id="shares" className="glass rounded-3xl p-4 sm:p-5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-sm font-bold tracking-tight text-zinc-900">Shares — expiring access</h2>
            <button className="glass rounded-full px-3 py-1.5 text-xs font-semibold text-zinc-900" onClick={() => load(token)}>Refresh</button>
          </div>
          <div className="mt-3 space-y-2">
            <label className="block text-xs font-semibold text-zinc-700">
              <span className="mb-1 block">Client</span>
              <select className="glass-input w-full rounded-xl px-3 py-2.5 text-sm text-zinc-900" value={selectedClientId} onChange={(e) => setSelectedClientId(e.target.value)}>
                <option value="">Select a client</option>
                {clients.map((c) => (
                  <option key={c.id} value={c.id}>{c.name || c.id} ({c.id})</option>
                ))}
              </select>
            </label>
            <label className="block text-xs font-semibold text-zinc-700">
              <span className="mb-1 block">Expiry</span>
              <select className="glass-input w-full rounded-xl px-3 py-2.5 text-sm text-zinc-900" value={expiry} onChange={(e) => setExpiry(e.target.value)}>
                <option value="1h">1 hour</option>
                <option value="24h">24 hours</option>
                <option value="168h">7 days</option>
              </select>
            </label>
            <button className="rounded-full bg-gradient-to-b from-[#ff4a3d] to-[#e81f14] px-5 py-2.5 text-sm font-semibold text-white shadow-sm" onClick={createShare} disabled={clients.length === 0}>Create share</button>
            {clients.length === 0 && <p className="text-xs text-zinc-500">No clients loaded, so no share can be created yet.</p>}
          </div>
          {createdShare && (
            <div className="mt-3 rounded-2xl bg-white/70 p-3 ring-1 ring-zinc-900/5">
              <p className="text-xs font-bold text-zinc-900">New share — token shown once</p>
              <p className="mt-1 break-words text-xs text-zinc-700">URL: {createdShare.url || "—"}</p>
              <p className="mt-1 break-words text-xs text-zinc-700">Token: <code className="break-all">{createdShare.token || "—"}</code></p>
              <p className="mt-1 text-xs text-zinc-600">Token shown once; only its hash is stored. Copy it now — reloading the list will not show it again.</p>
              {createdShare.expires_at ? <p className="mt-1 text-xs text-zinc-600">Expires: {createdShare.expires_at}</p> : null}
            </div>
          )}
          <ul className="mt-3 space-y-2 text-xs leading-relaxed">
            {shares.map((s) => (
              <li key={s.id} className="flex flex-wrap items-center justify-between gap-2 rounded-2xl bg-white/70 p-3 ring-1 ring-zinc-900/5">
                <span className="break-words text-zinc-700">row: {s.row_id} · expires: {s.expires_at || "—"} · revoked: {s.revoked ? "yes" : "no"}</span>
                {!s.revoked && <button className="glass rounded-full px-3 py-1.5 font-semibold text-red-700" onClick={() => revokeShare(s.id)}>Revoke</button>}
              </li>
            ))}
            {shares.length === 0 && !sharesError && <li className="text-zinc-500">No shares yet. Create one above.</li>}
          </ul>
          {sharesError && <p className="mt-3 break-words text-xs leading-relaxed text-zinc-600">{sharesError}</p>}
          {sharesMsg && <p className="mt-3 break-words text-xs leading-relaxed text-zinc-600">{sharesMsg}</p>}
        </section>

        <section id="heal" className="glass rounded-3xl p-4 sm:p-5">
          <h2 className="text-sm font-bold tracking-tight text-zinc-900">Self-healing proposals</h2>
          <p className="mt-2 text-xs leading-relaxed text-zinc-600">Approval alone never applies a fix. Apply only runs a safe whitelisted fix (for example, adding a missing clients column); anything unsafe stays preview-only and nothing is changed.</p>
          <ul className="mt-3 space-y-2 text-xs leading-relaxed">
            {proposals.map((p) => (
              <li key={p.id} className="rounded-2xl bg-white/70 p-3 ring-1 ring-zinc-900/5">
                <b className="text-zinc-900">{p.title}</b> — {p.status} — applied: {String(p.applied)}
                <div className="mt-1 break-words text-zinc-600">{p.diff}</div>
                {p.applied_columns ? <div className="mt-1 break-words text-zinc-600">applied_columns: {JSON.stringify(p.applied_columns)}</div> : null}
                {p.columns_after ? <div className="mt-1 break-words text-zinc-600">columns_after: {JSON.stringify(p.columns_after)}</div> : null}
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  {p.status === "pending" && <button className="rounded-full bg-gradient-to-b from-[#ff4a3d] to-[#e81f14] px-3 py-1.5 font-semibold text-white shadow-sm" onClick={() => approve(p.id)}>Approve (does not apply)</button>}
                  {p.status === "approved" && <button className="rounded-full bg-zinc-900 px-3 py-1.5 font-semibold text-white" onClick={() => applyHeal(p.id)}>Apply safe fix</button>}
                  {p.status === "pending" && <span className="rounded-full bg-zinc-100 px-3 py-1.5 font-semibold text-zinc-500 ring-1 ring-zinc-200">Apply: Approve first</span>}
                </div>
              </li>
            ))}
            {proposals.length === 0 && <li className="text-zinc-500">No proposals. Use the simulate-break endpoint to create one.</li>}
          </ul>
          {healMsg && <p className="mt-3 break-words text-xs leading-relaxed text-zinc-600">{healMsg}</p>}
        </section>
      </div>

      <section id="audit" className="glass mt-4 overflow-hidden rounded-3xl p-4 sm:p-5">
        <h2 className="text-sm font-bold tracking-tight text-zinc-900">Live audit trail</h2>
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[560px] text-left text-xs leading-relaxed">
            <thead><tr className="text-zinc-500"><th className="pb-2 pr-3 font-semibold">Action</th><th className="pb-2 pr-3 font-semibold">Table</th><th className="pb-2 pr-3 font-semibold">Allowed</th><th className="pb-2 pr-3 font-semibold">Reason</th><th className="pb-2 font-semibold">Latency</th></tr></thead>
            <tbody>
              {audit.map((r, i) => (
                <tr key={i} className="border-t border-zinc-900/10"><td className="py-2 pr-3">{r.action}</td><td className="py-2 pr-3">{r.table}</td><td className={`py-2 pr-3 font-semibold ${r.allowed ? "text-emerald-600" : "text-red-600"}`}>{String(r.allowed)}</td><td className="max-w-[280px] break-words py-2 pr-3 text-zinc-600">{r.reason}</td><td className="py-2">{r.latency_ms} ms</td></tr>
              ))}
            </tbody>
          </table>
        </div>
        {audit.length === 0 && <p className="mt-2 text-xs text-zinc-500">No audit rows yet.</p>}
        {msg && <p className="mt-3 break-words text-xs leading-relaxed text-zinc-600">{msg}</p>}
      </section>
    </main>
  );
}
