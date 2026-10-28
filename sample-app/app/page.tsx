"use client";

// Client Tracker: a deliberately ordinary app. It holds no database
// password and talks only to the gateway, so what each person can see
// and do is decided by the workspace policy and Postgres RLS, not here.

import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { motion } from "framer-motion";
import { Check, Copy, ExternalLink, Link2, LogOut, Pencil, Plus, Search, ShieldCheck, Trash2, Users } from "lucide-react";
import { ConfirmDialog, Dialog } from "@/components/dialog";
import { useToast } from "@/components/toast";
import { Badge, Button, Card, EmptyState, ErrorNote, Field, Input, Spinner, Textarea } from "@/components/ui";
import { api, ApiError, errorText, GATEWAY, SESSION_ENDED } from "@/lib/api";
import { relative } from "@/lib/format";
import type { Client, Me, Policy } from "@/lib/types";

const DASHBOARD_URL = process.env.NEXT_PUBLIC_DASHBOARD_URL ?? "http://localhost:3000";

type Ctx = { me: Me; policy: Policy };

export default function ClientTracker() {
  const [ctx, setCtx] = useState<Ctx | null>(null);
  const [state, setState] = useState<"loading" | "signed-out" | "ready" | "error">("loading");
  const [loadError, setLoadError] = useState("");

  const load = useCallback(async () => {
    try {
      const [me, policy] = await Promise.all([api<Me>("/api/v1/me"), api<Policy>("/api/v1/policy")]);
      setCtx({ me, policy });
      setState("ready");
    } catch (e) {
      if (e instanceof ApiError && SESSION_ENDED.has(e.code)) setState("signed-out");
      else {
        setLoadError(errorText(e));
        setState("error");
      }
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (state === "loading") return <Spinner label="Opening Client Tracker" />;
  if (state === "signed-out") return <SignIn onSignedIn={load} />;
  if (state === "error" || !ctx)
    return (
      <div className="mx-auto max-w-md p-6">
        <ErrorNote onRetry={load}>{loadError}</ErrorNote>
      </div>
    );
  return <Tracker ctx={ctx} onSignedOut={() => setState("signed-out")} />;
}

function Wordmark() {
  return (
    <span className="flex items-center gap-2">
      <span className="grid size-7 place-items-center rounded-lg border border-border bg-surface">
        <Users className="size-4" strokeWidth={1.5} />
      </span>
      <span className="font-display text-xl leading-none">Client Tracker</span>
    </span>
  );
}

function SignIn({ onSignedIn }: { onSignedIn: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    setBusy(true);
    setError("");
    try {
      await api("/api/v1/auth/login", { method: "POST", body: { email: f.get("email"), password: f.get("password") } });
      onSignedIn();
    } catch (err) {
      setError(errorText(err));
      setBusy(false);
    }
  }

  return (
    <div className="grid min-h-dvh place-items-center px-6">
      <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ type: "spring", stiffness: 260, damping: 30 }} className="w-full max-w-sm">
        <Wordmark />
        <h1 className="mt-8 font-display text-4xl">Sign in</h1>
        <p className="mt-2 text-fg-muted">Use your Cloud workspace account. Signing in to the console signs you in here too.</p>
        <form onSubmit={submit} className="mt-8 space-y-4" noValidate>
          <Field label="Email">
            <Input name="email" type="email" autoComplete="email" required autoFocus />
          </Field>
          <Field label="Password">
            <Input name="password" type="password" autoComplete="current-password" required />
          </Field>
          {error && <ErrorNote>{error}</ErrorNote>}
          <Button type="submit" variant="primary" className="w-full" loading={busy}>
            Sign in
          </Button>
        </form>
        <p className="mt-6 text-center text-[13px] text-fg-muted">
          No account?{" "}
          <a href={`${DASHBOARD_URL}/register`} className="focus-ring rounded font-medium text-fg underline-offset-4 hover:underline">
            Create a workspace
          </a>
        </p>
      </motion.div>
    </div>
  );
}

function Tracker({ ctx, onSignedOut }: { ctx: Ctx; onSignedOut: () => void }) {
  const { me, policy } = ctx;
  const notify = useToast();
  const can = (op: "read" | "write" | "delete", table = "clients") => policy.tables[table]?.[op]?.includes(me.role) ?? false;

  const [clients, setClients] = useState<Client[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<Client | "new" | null>(null);
  const [saving, setSaving] = useState(false);
  const [toDelete, setToDelete] = useState<Client | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [share, setShare] = useState<{ name: string; url: string } | null>(null);
  const [copied, setCopied] = useState(false);

  const reload = useCallback(async () => {
    try {
      setClients((await api<{ clients: Client[] }>("/api/v1/clients")).clients);
      setError("");
    } catch (e) {
      if (e instanceof ApiError && SESSION_ENDED.has(e.code)) onSignedOut();
      else setError(errorText(e));
    }
  }, [onSignedOut]);

  useEffect(() => {
    reload();
  }, [reload]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (clients ?? []).filter((c) => !q || `${c.name} ${c.email} ${c.notes}`.toLowerCase().includes(q));
  }, [clients, query]);

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const body = { name: f.get("name"), email: f.get("email"), notes: f.get("notes") };
    setSaving(true);
    try {
      if (editing === "new") await api("/api/v1/clients", { method: "POST", body });
      else if (editing) await api(`/api/v1/clients/${editing.id}`, { method: "PUT", body });
      notify("success", editing === "new" ? "Client added." : "Changes saved.");
      setEditing(null);
      await reload();
    } catch (err) {
      notify("error", errorText(err));
    } finally {
      setSaving(false);
    }
  }

  async function createShare(c: Client) {
    try {
      const r = await api<{ token: string }>("/api/v1/shares", { method: "POST", body: { row_id: c.id, expires_in: "24h" } });
      setCopied(false);
      setShare({ name: c.name, url: `${GATEWAY}/share/${r.token}` });
    } catch (err) {
      notify("error", errorText(err));
    }
  }

  const current = editing && editing !== "new" ? editing : null;

  return (
    <div className="min-h-dvh">
      <header className="sticky top-0 z-20 border-b border-border bg-bg/90 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-5xl items-center justify-between gap-4 px-4 md:px-6">
          <Wordmark />
          <div className="flex items-center gap-1">
            <span className="mr-2 hidden text-right text-xs sm:block">
              <span className="block font-medium text-fg">{me.company_name}</span>
              <span className="text-fg-muted">
                {me.email} · {me.role}
              </span>
            </span>
            <a href={`${DASHBOARD_URL}/overview`} className="focus-ring grid size-9 place-items-center rounded-lg text-fg-muted hover:bg-surface-2 hover:text-fg" title="Open the Cloud console" aria-label="Open the Cloud console">
              <ExternalLink className="size-4" strokeWidth={1.5} />
            </a>
            <button
              onClick={async () => {
                await api("/api/v1/auth/logout", { method: "POST" }).catch(() => {});
                onSignedOut();
              }}
              className="focus-ring grid size-9 place-items-center rounded-lg text-fg-muted hover:bg-surface-2 hover:text-fg"
              title="Sign out"
              aria-label="Sign out"
            >
              <LogOut className="size-4" strokeWidth={1.5} />
            </button>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-4 py-10 md:px-6">
        <div className="mb-8 flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="font-display text-4xl">Clients</h1>
            <p className="mt-2 text-fg-muted">
              {clients ? `${clients.length} ${clients.length === 1 ? "client" : "clients"} in ${me.company_name}` : "Loading your clients"}
            </p>
          </div>
          {can("write") ? (
            <Button variant="primary" icon={Plus} onClick={() => setEditing("new")}>
              Add client
            </Button>
          ) : (
            <Badge>Read-only access</Badge>
          )}
        </div>

        <Card>
          <div className="border-b border-border p-3">
            <label className="relative block">
              <span className="sr-only">Search clients</span>
              <Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-fg-subtle" strokeWidth={1.5} />
              <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search by name, email or note" className="pl-9" />
            </label>
          </div>
          {error ? (
            <div className="p-5">
              <ErrorNote onRetry={reload}>{error}</ErrorNote>
            </div>
          ) : !clients ? (
            <Spinner />
          ) : shown.length === 0 ? (
            <EmptyState icon={Users} title={clients.length ? "No clients match your search" : "No clients yet"}>
              {clients.length ? "Try another name or email." : can("write") ? "Add your first client to get started." : "Nothing has been added yet."}
            </EmptyState>
          ) : (
            <ul className="divide-y divide-border">
              {shown.map((c) => (
                <li key={c.id} className="group flex items-start gap-4 px-5 py-4">
                  <div className="grid size-9 shrink-0 place-items-center rounded-full bg-surface-2 text-[13px] font-medium text-fg-muted">{c.name.slice(0, 1).toUpperCase()}</div>
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">{c.name}</p>
                    <p className="text-[13px] text-fg-muted">{c.email || "No email"}</p>
                    {c.notes && <p className="mt-1 text-[13px] text-fg-muted">{c.notes}</p>}
                    <p className="mt-1 text-xs text-fg-subtle">Added {relative(c.created_at)}</p>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    {can("write", "shares") && <Button size="sm" variant="ghost" icon={Link2} onClick={() => createShare(c)} title="Share for 24 hours" aria-label={`Share ${c.name}`} />}
                    {can("write") && <Button size="sm" variant="ghost" icon={Pencil} onClick={() => setEditing(c)} title="Edit" aria-label={`Edit ${c.name}`} />}
                    {can("delete") && <Button size="sm" variant="ghost" icon={Trash2} onClick={() => setToDelete(c)} title="Delete" aria-label={`Delete ${c.name}`} />}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <p className="mt-6 flex items-center justify-center gap-1.5 text-xs text-fg-subtle">
          <ShieldCheck className="size-3.5" strokeWidth={1.5} />
          This app has no database password. Every request is checked by the Cloud gateway.
        </p>
      </main>

      <Dialog open={editing !== null} onClose={() => setEditing(null)} title={current ? `Edit ${current.name}` : "Add a client"}>
        <form onSubmit={save} className="space-y-4" key={current?.id ?? "new"}>
          <Field label="Name">
            <Input name="name" defaultValue={current?.name} required maxLength={120} />
          </Field>
          <Field label="Email">
            <Input name="email" type="email" defaultValue={current?.email} maxLength={200} />
          </Field>
          <Field label="Notes">
            <Textarea name="notes" defaultValue={current?.notes} maxLength={2000} />
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" onClick={() => setEditing(null)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" loading={saving}>
              {current ? "Save changes" : "Add client"}
            </Button>
          </div>
        </form>
      </Dialog>

      <ConfirmDialog
        open={toDelete !== null}
        onClose={() => setToDelete(null)}
        title={`Delete ${toDelete?.name}?`}
        description="This removes the client for everyone in your workspace."
        confirmLabel="Delete client"
        busy={deleting}
        onConfirm={async () => {
          if (!toDelete) return;
          setDeleting(true);
          try {
            await api(`/api/v1/clients/${toDelete.id}`, { method: "DELETE" });
            notify("success", `${toDelete.name} was deleted.`);
            setToDelete(null);
            await reload();
          } catch (err) {
            notify("error", errorText(err));
          } finally {
            setDeleting(false);
          }
        }}
      />

      <Dialog open={share !== null} onClose={() => setShare(null)} title={`Share ${share?.name}`} description="Read-only, valid for 24 hours. Copy it now: it is shown only once.">
        <div className="flex gap-2">
          <Input readOnly value={share?.url ?? ""} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
          <Button
            icon={copied ? Check : Copy}
            onClick={async () => {
              if (!share) return;
              await navigator.clipboard.writeText(share.url);
              setCopied(true);
            }}
          >
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>
      </Dialog>
    </div>
  );
}
