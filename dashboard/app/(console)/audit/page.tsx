"use client";

import { useMemo, useState } from "react";
import { RotateCw, ScrollText, ShieldCheck, ShieldX } from "lucide-react";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorNote, PageHeader, Select, Spinner, TableWrap, cx, td, th } from "@/components/ui";
import { dateTime, relative } from "@/lib/format";
import { useResource } from "@/lib/session";
import type { AuditRow } from "@/lib/types";

type Outcome = "all" | "allowed" | "denied";

export default function AuditPage() {
  const { data, error, loading, reload } = useResource<{ audit: AuditRow[] }>("/api/v1/audit?limit=500");
  const [outcome, setOutcome] = useState<Outcome>("all");
  const [table, setTable] = useState("all");

  const rows = data?.audit ?? [];
  const tables = useMemo(() => Array.from(new Set(rows.map((r) => r.table).filter(Boolean))).sort(), [rows]);
  const shown = rows.filter((r) => (outcome === "all" || (outcome === "allowed") === r.allowed) && (table === "all" || r.table === table));

  return (
    <>
      <PageHeader title="Audit log" description="Every decision the gateway made for this workspace, allowed or denied, with how long it took. Entries cannot be edited or deleted from the console." />

      <Card>
        <CardHeader
          title="Events"
          description={data ? `Showing ${shown.length} of the latest ${rows.length}` : undefined}
          action={
            <div className="flex flex-wrap gap-2">
              <div className="inline-flex rounded-lg border border-border p-0.5" role="group" aria-label="Outcome">
                {(["all", "allowed", "denied"] as Outcome[]).map((o) => (
                  <button
                    key={o}
                    onClick={() => setOutcome(o)}
                    aria-pressed={outcome === o}
                    className={cx("focus-ring rounded-md px-2.5 py-1 text-[13px] capitalize transition-colors", outcome === o ? "bg-surface-2 font-medium text-fg" : "text-fg-muted hover:text-fg")}
                  >
                    {o}
                  </button>
                ))}
              </div>
              <Select value={table} onChange={(e) => setTable(e.target.value)} className="h-8 !w-44 text-[13px]" aria-label="Resource">
                <option value="all">All resources</option>
                {tables.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </Select>
              <Button size="sm" icon={RotateCw} onClick={reload} loading={loading && !!data} aria-label="Refresh" />
            </div>
          }
        />
        {error ? (
          <div className="p-5">
            <ErrorNote onRetry={reload}>{error.text}</ErrorNote>
          </div>
        ) : loading && !data ? (
          <Spinner />
        ) : shown.length === 0 ? (
          <EmptyState icon={ScrollText} title={rows.length ? "No events match these filters" : "No events yet"}>
            {rows.length ? "Try a different outcome or resource." : "Activity appears here as soon as anyone uses an app."}
          </EmptyState>
        ) : (
          <TableWrap>
            <table className="w-full min-w-[760px]">
              <thead className="border-b border-border">
                <tr>
                  <th className={th}>When</th>
                  <th className={th}>Outcome</th>
                  <th className={th}>Action</th>
                  <th className={th}>Role</th>
                  <th className={th}>Detail</th>
                  <th className={`${th} text-right`}>Latency</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {shown.map((r, i) => (
                  <tr key={i} className="hover:bg-surface-2/50">
                    <td className={`${td} whitespace-nowrap text-fg-muted`} title={dateTime(r.created_at)}>
                      {relative(r.created_at)}
                    </td>
                    <td className={td}>
                      {r.allowed ? (
                        <Badge tone="info" icon={ShieldCheck}>
                          Allowed
                        </Badge>
                      ) : (
                        <Badge tone="danger" icon={ShieldX}>
                          Denied
                        </Badge>
                      )}
                    </td>
                    <td className={`${td} whitespace-nowrap`}>
                      <span className="font-medium">{r.action}</span> <span className="text-fg-muted">{r.table}</span>
                    </td>
                    <td className={`${td} capitalize text-fg-muted`}>{r.actor_role || "—"}</td>
                    <td className={`${td} max-w-sm truncate text-fg-muted`} title={r.reason}>
                      {r.reason || "—"}
                    </td>
                    <td className={`${td} text-right tabular-nums text-fg-muted`}>{r.latency_ms} ms</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Card>
    </>
  );
}
