"use client";

import { useState, type FormEvent } from "react";
import { AppWindow, CircleDot, ExternalLink, HeartPulse, Play, Plus, RotateCw, Square, Trash2 } from "lucide-react";
import { ConfirmDialog } from "@/components/dialog";
import { useToast } from "@/components/toast";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorNote, Input, PageHeader, Spinner } from "@/components/ui";
import { api, errorText } from "@/lib/api";
import { useResource, useSession } from "@/lib/session";
import { shortId } from "@/lib/format";
import type { App, AppStatus } from "@/lib/types";

const statusTone: Record<AppStatus, "success" | "neutral" | "warning" | "danger" | "info"> = {
  running: "success",
  deploying: "info",
  registered: "neutral",
  stopped: "neutral",
  failed: "danger",
};

const statusLabel: Record<AppStatus, string> = {
  running: "Running",
  deploying: "Deploying",
  registered: "Not deployed",
  stopped: "Stopped",
  failed: "Failed",
};

export default function AppsPage() {
  const { can } = useSession();
  const notify = useToast();
  const { data, error, loading, reload } = useResource<{ apps: App[] }>("/api/v1/apps");
  const [busy, setBusy] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [toDelete, setToDelete] = useState<App | null>(null);
  const canWrite = can("apps", "write");
  const canDelete = can("apps", "delete");

  async function run(key: string, fn: () => Promise<unknown>, ok: string) {
    setBusy(key);
    try {
      await fn();
      notify("success", ok);
      await reload();
    } catch (e) {
      notify("error", errorText(e));
      await reload();
    } finally {
      setBusy(null);
    }
  }

  async function create(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const formEl = e.currentTarget;
    const name = String(new FormData(formEl).get("name") ?? "").trim();
    if (!name) return;
    setCreating(true);
    try {
      await api("/api/v1/apps", { method: "POST", body: { name, repo: "sample-app" } });
      formEl.reset();
      notify("success", `${name} is registered. Deploy it when you're ready.`);
      await reload();
    } catch (err) {
      notify("error", errorText(err));
    } finally {
      setCreating(false);
    }
  }

  const apps = data?.apps ?? [];

  return (
    <>
      <PageHeader title="Apps" description="Each app runs in its own container and reaches data only through the gateway. It never holds a database password." />

      {canWrite && (
        <Card className="mb-6">
          <form onSubmit={create} className="flex flex-col gap-3 p-5 sm:flex-row sm:items-end">
            <label className="flex-1 space-y-1.5">
              <span className="text-[13px] font-medium">New app</span>
              <Input name="name" placeholder="Client Tracker" maxLength={80} required />
              <span className="block text-xs text-fg-muted">Built from the Client Tracker template. Deploying from a git repository is coming next.</span>
            </label>
            <Button type="submit" variant="primary" icon={Plus} loading={creating} className="sm:mb-6">
              Create app
            </Button>
          </form>
        </Card>
      )}

      <Card>
        <CardHeader title="Your apps" description={data ? `${apps.length} ${apps.length === 1 ? "app" : "apps"} in this workspace` : undefined} />
        {error ? (
          <div className="p-5">
            <ErrorNote onRetry={reload}>{error.text}</ErrorNote>
          </div>
        ) : loading && !data ? (
          <Spinner />
        ) : apps.length === 0 ? (
          <EmptyState icon={AppWindow} title="No apps yet">
            {canWrite ? "Create one above, then deploy it to get its own URL." : "An owner or employee can create the first app."}
          </EmptyState>
        ) : (
          <ul className="divide-y divide-border">
            {apps.map((a) => {
              const isBusy = busy?.startsWith(a.id) ?? false;
              return (
                <li key={a.id} className="flex flex-col gap-4 px-5 py-4 md:flex-row md:items-center">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="font-medium">{a.name}</p>
                      <Badge tone={statusTone[a.status]} icon={CircleDot}>
                        {statusLabel[a.status]}
                      </Badge>
                    </div>
                    <p className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-fg-muted">
                      {a.status === "running" && a.url ? (
                        <a href={a.url} target="_blank" rel="noreferrer" className="focus-ring inline-flex items-center gap-1 rounded font-medium text-fg hover:underline">
                          {a.url.replace(/^https?:\/\//, "")} <ExternalLink className="size-3" strokeWidth={1.5} />
                        </a>
                      ) : (
                        <span>No public URL</span>
                      )}
                      {a.container_id && <span className="font-mono">container {shortId(a.container_id, 12)}</span>}
                      <span>template: client tracker</span>
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {canWrite && (
                      <Button
                        size="sm"
                        variant={a.status === "running" ? "secondary" : "primary"}
                        icon={a.status === "running" ? RotateCw : Play}
                        loading={busy === `${a.id}:deploy`}
                        disabled={isBusy || a.status === "deploying"}
                        onClick={() => run(`${a.id}:deploy`, () => api(`/api/v1/apps/${a.id}/deploy`, { method: "POST" }), `${a.name} is live.`)}
                      >
                        {a.status === "running" ? "Redeploy" : "Deploy"}
                      </Button>
                    )}
                    {a.status === "running" && (
                      <Button
                        size="sm"
                        icon={HeartPulse}
                        loading={busy === `${a.id}:health`}
                        disabled={isBusy}
                        onClick={async () => {
                          setBusy(`${a.id}:health`);
                          try {
                            const h = await api<{ healthy: boolean; container_status: string }>(`/api/v1/apps/${a.id}/health`);
                            notify(h.healthy ? "success" : "error", h.healthy ? `${a.name} is healthy.` : `${a.name} did not answer its health check (container ${h.container_status}).`);
                          } catch (e) {
                            notify("error", errorText(e));
                          } finally {
                            setBusy(null);
                          }
                        }}
                      >
                        Check
                      </Button>
                    )}
                    {canWrite && a.status === "running" && (
                      <Button
                        size="sm"
                        icon={Square}
                        loading={busy === `${a.id}:stop`}
                        disabled={isBusy}
                        onClick={() => run(`${a.id}:stop`, () => api(`/api/v1/apps/${a.id}/stop`, { method: "POST" }), `${a.name} is stopped.`)}
                      >
                        Stop
                      </Button>
                    )}
                    {canDelete && (
                      <Button size="sm" variant="ghost" icon={Trash2} disabled={isBusy} onClick={() => setToDelete(a)} aria-label={`Delete ${a.name}`}>
                        <span className="sr-only md:not-sr-only">Delete</span>
                      </Button>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Card>

      <ConfirmDialog
        open={toDelete !== null}
        onClose={() => setToDelete(null)}
        title={`Delete ${toDelete?.name}?`}
        description="Its container is stopped and removed, and the app disappears from this workspace. Client records are kept."
        confirmLabel="Delete app"
        busy={busy === `${toDelete?.id}:delete`}
        onConfirm={async () => {
          if (!toDelete) return;
          await run(`${toDelete.id}:delete`, () => api(`/api/v1/apps/${toDelete.id}`, { method: "DELETE" }), `${toDelete.name} was deleted.`);
          setToDelete(null);
        }}
      />
    </>
  );
}
