"use client";
import { useEffect, useState } from "react";
import { api } from "../lib/api";

// Sample app: a simple client tracker. It has NO database password and
// NO direct database access. Every request goes through the gateway,
// which enforces the tenant policy and Postgres RLS.
export default function Home() {
  const [token, setToken] = useState("");
  const [email, setEmail] = useState("owner@example.ma");
  const [password, setPassword] = useState("beta-password-1");
  const [clients, setClients] = useState<any[]>([]);
  const [name, setName] = useState("");
  const [cemail, setCemail] = useState("");
  const [msg, setMsg] = useState("");
  const [shareUrl, setShareUrl] = useState("");

  async function load(t: string) {
    const r = await api("/api/v1/clients", t);
    if (r.ok) setClients((r.data as any).clients || []);
    else setMsg(JSON.stringify(r.data));
  }
  useEffect(() => {
    const t = localStorage.getItem("cloud_beta_token");
    if (t) {
      setToken(t);
      load(t);
    }
  }, []);
  async function login() {
    const r = await api("/api/v1/auth/login", "", { method: "POST", body: JSON.stringify({ email, password }) });
    if (r.ok && (r.data as any).token) {
      localStorage.setItem("cloud_beta_token", (r.data as any).token);
      setToken((r.data as any).token);
      load((r.data as any).token);
    } else setMsg(JSON.stringify(r.data));
  }
  async function create() {
    const r = await api("/api/v1/clients", token, { method: "POST", body: JSON.stringify({ name, email: cemail, notes: "" }) });
    setMsg(JSON.stringify(r.data));
    if (r.ok) load(token);
  }
  async function share(id: string) {
    const r = await api("/api/v1/shares", token, { method: "POST", body: JSON.stringify({ row_id: id, expires_in: "1h" }) });
    if (r.ok) setShareUrl(`${window.location.origin.replace("3001", "8080")}${(r.data as any).url}`);
    setMsg(JSON.stringify(r.data));
  }
  if (!token)
    return (
      <main style={{ maxWidth: 480, margin: "40px auto", padding: 16 }}>
        <h1>Client Tracker (sample app)</h1>
        <p>Sign in with the gateway account you registered in the dashboard. This app never touches the database directly.</p>
        <div style={{ display: "grid", gap: 8 }}>
          <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" />
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password" />
          <button onClick={login}>Sign in via gateway</button>
          {msg && <p>{msg}</p>}
        </div>
      </main>
    );
  return (
    <main style={{ maxWidth: 720, margin: "24px auto", padding: 16 }}>
      <h1>Client Tracker (sample app)</h1>
      <p>Data is yours only — the gateway + RLS enforce company_id on every row.</p>
      <div style={{ display: "flex", gap: 8 }}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Client name" />
        <input value={cemail} onChange={(e) => setCemail(e.target.value)} placeholder="Client email" />
        <button onClick={create}>Add client</button>
      </div>
      <ul>
        {clients.map((c) => (
          <li key={c.id}>{c.name} — {c.email} <button onClick={() => share(c.id)}>Share 1h</button></li>
        ))}
      </ul>
      {shareUrl && <p>Share link (single row, expires in 1h): <a href={shareUrl}>{shareUrl}</a></p>}
      {msg && <p>{msg}</p>}
    </main>
  );
}
