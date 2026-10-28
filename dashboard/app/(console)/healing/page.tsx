"use client";

import { useState } from "react";
import { CircleCheck, Eye, ScanSearch, ShieldCheck, Wrench, Zap } from "lucide-react";
import { useToast } from "@/components/toast";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorNote, PageHeader, Select, Spinner } from "@/components/ui";
import { api, errorText } from "@/lib/api";
import { relative } from "@/lib/format";
import { useResource, useSession } from "@/lib/session";
import type { Proposal } from "@/lib/types";

const statusTone = { pending: "warning", approved: "info", applied: "success" } as const;

export default function HealingPage() {
  const { can } = useSession();
  const notify = useToast();
  const { data, error, loading, reload } = useResource<{ proposals: Proposal[] }>("/api/v1/heal/proposals");
  const [busy, setBusy] = useState<string | null>(null);
  const [field, setField] = useState("email");
  const canWrite = can("heal_proposals", "write");
  const proposals = data?.proposals ?? [];

  async function act(key: string, fn: () => Promise<string>) {
    setBusy(key);
    try {
      notify("success", await fn());
      await reload();
    } catch (e) {
      notify("error", errorText(e));
    } finally {
      setBusy(null);
    }
  }

  return (
    <>
      <PageHeader
        title="Healing"
        description="When an app breaks because the data it expects changed, the gateway proposes a fix. Nothing changes until an owner approves it, and only fixes on a safe list can be applied automatically."
      />

      {canWrite && (
        <Card className="mb-6">
          <div className="grid gap-6 p-5 md:grid-cols-2">
            <div>
              <p className="font-medium">Scan the schema</p>
              <p className="mt-1 text-[13px] text-fg-muted">Checks the clients table for missing columns and proposes a restore if any are gone.</p>
              <Button
                className="mt-4"
                icon={ScanSearch}
                loading={busy === "detect"}
                onClick={() =>
                  act("detect", async () => {
                    const r = await api<{ status: string; missing: string[] }>("/api/v1/heal/detect", { method: "POST" });
                    return r.status === "healthy" ? "Schema is healthy. Nothing to fix." : `Found missing column(s): ${r.missing.join(", ")}. A fix is waiting for approval.`;
                  })
                }
              >
                Scan now
              </Button>
            </div>
            <div>
              <p className="font-medium">Rehearse a break</p>
              <p className="mt-1 text-[13px] text-fg-muted">Creates a practice proposal without touching any table. Practice proposals can be approved but never applied.</p>
              <div className="mt-4 flex gap-2">
                <Select value={field} onChange={(e) => setField(e.target.value)} className="!w-36" aria-label="Field to break">
                  <option value="email">email</option>
                  <option value="notes">notes</option>
                  <option value="name">name</option>
                </Select>
                <Button
                  icon={Zap}
                  loading={busy === "simulate"}
                  onClick={() =>
                    act("simulate", async () => {
                      await api("/api/v1/heal/simulate-break", { method: "POST", body: { field } });
                      return `Practice proposal created for '${field}'.`;
                    })
                  }
                >
                  Rehearse
                </Button>
              </div>
            </div>
          </div>
        </Card>
      )}

      <Card>
        <CardHeader title="Proposals" description="Newest first." />
        {error ? (
          <div className="p-5">
            <ErrorNote onRetry={reload}>{error.text}</ErrorNote>
          </div>
        ) : loading && !data ? (
          <Spinner />
        ) : proposals.length === 0 ? (
          <EmptyState icon={ShieldCheck} title="No proposals">
            Scan the schema, or rehearse a break to see the approval flow.
          </EmptyState>
        ) : (
          <ul className="divide-y divide-border">
            {proposals.map((p) => (
              <li key={p.id} className="flex flex-col gap-3 px-5 py-4 md:flex-row md:items-start">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="font-medium">{p.title}</p>
                    <Badge tone={statusTone[p.status]}>{p.status}</Badge>
                    {!p.applyable && (
                      <Badge icon={Eye} tone="neutral">
                        preview only
                      </Badge>
                    )}
                  </div>
                  <p className="mt-1.5 text-[13px] leading-relaxed text-fg-muted">{p.diff}</p>
                  <p className="mt-1.5 text-xs text-fg-subtle">{relative(p.created_at)}</p>
                </div>
                {canWrite && (
                  <div className="flex shrink-0 gap-2">
                    {p.status === "pending" && (
                      <Button
                        size="sm"
                        icon={CircleCheck}
                        loading={busy === `${p.id}:approve`}
                        onClick={() =>
                          act(`${p.id}:approve`, async () => {
                            await api(`/api/v1/heal/${p.id}/approve`, { method: "POST" });
                            return p.applyable ? "Approved. You can apply it now." : "Approved. This one is preview-only: apply the change by hand.";
                          })
                        }
                      >
                        Approve
                      </Button>
                    )}
                    {p.status === "approved" && p.applyable && (
                      <Button
                        size="sm"
                        variant="primary"
                        icon={Wrench}
                        loading={busy === `${p.id}:apply`}
                        onClick={() =>
                          act(`${p.id}:apply`, async () => {
                            const r = await api<{ applied_columns: string[] }>(`/api/v1/heal/${p.id}/apply`, { method: "POST" });
                            return `Restored ${r.applied_columns.join(", ")}.`;
                          })
                        }
                      >
                        Apply fix
                      </Button>
                    )}
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </>
  );
}
