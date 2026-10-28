"use client";

import Link from "next/link";
import { ArrowUpRight, ShieldX } from "lucide-react";
import { ActivityChart } from "@/components/activity-chart";
import { Badge, Card, CardHeader, EmptyState, ErrorNote, PageHeader, Spinner } from "@/components/ui";
import { useResource, useSession } from "@/lib/session";
import { compact, relative } from "@/lib/format";
import type { App, AuditRow, AuditSummary, Client } from "@/lib/types";

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-xl border border-border bg-surface px-5 py-4 shadow-sm">
      <p className="text-xs font-medium uppercase tracking-wide text-fg-subtle">{label}</p>
      <p className="mt-2 text-2xl font-semibold tabular-nums">{value}</p>
      {hint && <p className="mt-0.5 text-xs text-fg-muted">{hint}</p>}
    </div>
  );
}

export default function OverviewPage() {
  const { me } = useSession();
  const summary = useResource<AuditSummary>("/api/v1/audit/summary");
  const apps = useResource<{ apps: App[] }>("/api/v1/apps");
  const clients = useResource<{ clients: Client[] }>("/api/v1/clients");
  const audit = useResource<{ audit: AuditRow[] }>("/api/v1/audit?limit=200");

  const s = summary.data;
  const running = apps.data?.apps.filter((a) => a.status === "running").length ?? 0;
  const denials = audit.data?.audit.filter((a) => !a.allowed).slice(0, 6) ?? [];
  const denyRate = s && s.total > 0 ? Math.round((s.denied / s.total) * 100) : 0;

  return (
    <>
      <PageHeader title={me.company_name} description="Every request your apps make goes through the gateway. Here is what it allowed and what it stopped." />

      {summary.error ? (
        <ErrorNote onRetry={summary.reload}>{summary.error.text}</ErrorNote>
      ) : (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <Stat label="Requests" value={s ? compact(s.total) : "—"} hint="All time" />
          <Stat label="Denied" value={s ? compact(s.denied) : "—"} hint={s ? `${denyRate}% of requests` : undefined} />
          <Stat label="Avg. latency" value={s ? `${s.avg_latency_ms.toFixed(1)} ms` : "—"} hint="Gateway decision time" />
          <Stat label="Apps running" value={apps.data ? `${running} / ${apps.data.apps.length}` : "—"} hint={clients.data ? `${clients.data.clients.length} client records` : undefined} />
        </div>
      )}

      <div className="mt-6 grid gap-6 lg:grid-cols-5">
        <Card className="lg:col-span-3">
          <CardHeader title="Requests per day" description="Last 7 days, from the audit log." />
          <div className="px-5 pb-5 pt-4">{s ? <ActivityChart perDay={s.per_day} /> : <Spinner />}</div>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader
            title="Recent denials"
            description="Blocked by policy or row-level security."
            action={
              <Link href="/audit" className="focus-ring flex items-center gap-1 rounded text-[13px] text-fg-muted hover:text-fg">
                Audit log <ArrowUpRight className="size-3.5" strokeWidth={1.5} />
              </Link>
            }
          />
          {audit.loading && !audit.data ? (
            <Spinner />
          ) : denials.length === 0 ? (
            <EmptyState icon={ShieldX} title="Nothing blocked yet">
              When someone tries to read another workspace&apos;s data or act beyond their role, it shows up here.
            </EmptyState>
          ) : (
            <ul className="divide-y divide-border">
              {denials.map((d, i) => (
                <li key={i} className="px-5 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <p className="truncate text-[13px] font-medium">
                      {d.action} on {d.table}
                    </p>
                    <span className="shrink-0 text-xs text-fg-subtle">{relative(d.created_at)}</span>
                  </div>
                  <div className="mt-1 flex items-center gap-2">
                    <Badge tone="danger">Denied</Badge>
                    <span className="truncate text-xs text-fg-muted">{d.reason}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </>
  );
}
