"use client";

import { useState } from "react";
import { Link2, Plus } from "lucide-react";
import { ConfirmDialog } from "@/components/dialog";
import { ShareDialog } from "@/components/share-dialog";
import { useToast } from "@/components/toast";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorNote, PageHeader, Spinner, TableWrap, td, th } from "@/components/ui";
import { api, errorText } from "@/lib/api";
import { dateTime, relative } from "@/lib/format";
import { useResource, useSession } from "@/lib/session";
import type { Client, Share } from "@/lib/types";

function shareState(s: Share): { label: string; tone: "success" | "neutral" | "danger" } {
  if (s.revoked) return { label: "Revoked", tone: "danger" };
  if (new Date(s.expires_at).getTime() < Date.now()) return { label: "Expired", tone: "neutral" };
  return { label: "Active", tone: "success" };
}

export default function SharesPage() {
  const { can } = useSession();
  const notify = useToast();
  const shares = useResource<{ shares: Share[] }>("/api/v1/shares");
  const clients = useResource<{ clients: Client[] }>("/api/v1/clients");
  const [creating, setCreating] = useState(false);
  const [toRevoke, setToRevoke] = useState<Share | null>(null);
  const [revoking, setRevoking] = useState(false);
  const list = shares.data?.shares ?? [];
  const noClients = clients.data?.clients.length === 0;

  return (
    <>
      <PageHeader
        title="Shares"
        description="Read-only links to a single record, for people outside the workspace. Only a hash of each link is stored."
        action={
          can("shares", "write") && (
            <Button variant="primary" icon={Plus} onClick={() => setCreating(true)} disabled={!clients.data || noClients} title={noClients ? "Add a client first" : undefined}>
              New share link
            </Button>
          )
        }
      />

      <Card>
        <CardHeader title="Share links" description="Newest first. Expired and revoked links stay listed for the record." />
        {shares.error ? (
          <div className="p-5">
            <ErrorNote onRetry={shares.reload}>{shares.error.text}</ErrorNote>
          </div>
        ) : shares.loading && !shares.data ? (
          <Spinner />
        ) : list.length === 0 ? (
          <EmptyState icon={Link2} title="No share links yet">
            {noClients ? "Add a client first, then share it from here or from the Clients page." : "Create one to give someone outside the workspace read access to one record."}
          </EmptyState>
        ) : (
          <TableWrap>
            <table className="w-full min-w-[620px]">
              <thead className="border-b border-border">
                <tr>
                  <th className={th}>Record</th>
                  <th className={th}>Status</th>
                  <th className={th}>Expires</th>
                  <th className={th}>Created</th>
                  <th className={th}>
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {list.map((s) => {
                  const st = shareState(s);
                  return (
                    <tr key={s.id}>
                      <td className={`${td} font-medium`}>{s.row_name || <span className="text-fg-muted">Deleted record</span>}</td>
                      <td className={td}>
                        <Badge tone={st.tone}>{st.label}</Badge>
                      </td>
                      <td className={`${td} whitespace-nowrap text-fg-muted`} title={dateTime(s.expires_at)}>
                        {relative(s.expires_at)}
                      </td>
                      <td className={`${td} whitespace-nowrap text-fg-muted`}>{relative(s.created_at)}</td>
                      <td className={`${td} text-right`}>
                        {can("shares", "delete") && st.label === "Active" && (
                          <Button size="sm" variant="danger" onClick={() => setToRevoke(s)}>
                            Revoke
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Card>

      {creating && clients.data && <ShareDialog open clients={clients.data.clients} onClose={() => setCreating(false)} onCreated={shares.reload} />}

      <ConfirmDialog
        open={toRevoke !== null}
        onClose={() => setToRevoke(null)}
        title="Revoke this link?"
        description={`The link to ${toRevoke?.row_name || "this record"} stops working immediately. This cannot be undone.`}
        confirmLabel="Revoke link"
        busy={revoking}
        onConfirm={async () => {
          if (!toRevoke) return;
          setRevoking(true);
          try {
            await api(`/api/v1/shares/${toRevoke.id}/revoke`, { method: "POST" });
            notify("success", "Link revoked.");
            setToRevoke(null);
            await shares.reload();
          } catch (err) {
            notify("error", errorText(err));
          } finally {
            setRevoking(false);
          }
        }}
      />
    </>
  );
}
