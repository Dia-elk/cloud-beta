"use client";

import { useState, type FormEvent } from "react";
import { Link2, Pencil, Plus, Trash2, Users } from "lucide-react";
import { ConfirmDialog, Dialog } from "@/components/dialog";
import { ShareDialog } from "@/components/share-dialog";
import { useToast } from "@/components/toast";
import { Button, Card, CardHeader, EmptyState, ErrorNote, Field, Input, PageHeader, Spinner, TableWrap, Textarea, td, th } from "@/components/ui";
import { api, errorText } from "@/lib/api";
import { relative } from "@/lib/format";
import { useResource, useSession } from "@/lib/session";
import type { Client } from "@/lib/types";

type Editing = { mode: "create" } | { mode: "edit"; client: Client };

export default function ClientsPage() {
  const { can, me } = useSession();
  const notify = useToast();
  const { data, error, loading, reload } = useResource<{ clients: Client[] }>("/api/v1/clients");
  const [editing, setEditing] = useState<Editing | null>(null);
  const [saving, setSaving] = useState(false);
  const [toDelete, setToDelete] = useState<Client | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [sharing, setSharing] = useState<Client | null>(null);
  const clients = data?.clients ?? [];

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!editing) return;
    const f = new FormData(e.currentTarget);
    const body = { name: f.get("name"), email: f.get("email"), notes: f.get("notes") };
    setSaving(true);
    try {
      if (editing.mode === "create") {
        await api("/api/v1/clients", { method: "POST", body });
        notify("success", "Client added.");
      } else {
        await api(`/api/v1/clients/${editing.client.id}`, { method: "PUT", body });
        notify("success", "Changes saved.");
      }
      setEditing(null);
      await reload();
    } catch (err) {
      notify("error", errorText(err));
    } finally {
      setSaving(false);
    }
  }

  const current = editing?.mode === "edit" ? editing.client : null;

  return (
    <>
      <PageHeader
        title="Clients"
        description={`The records your apps store for ${me.company_name}. Row-level security keeps them invisible to every other workspace.`}
        action={
          can("clients", "write") && (
            <Button variant="primary" icon={Plus} onClick={() => setEditing({ mode: "create" })}>
              Add client
            </Button>
          )
        }
      />

      <Card>
        <CardHeader title="All clients" description={data ? `${clients.length} ${clients.length === 1 ? "record" : "records"}` : undefined} />
        {error ? (
          <div className="p-5">
            <ErrorNote onRetry={reload}>{error.text}</ErrorNote>
          </div>
        ) : loading && !data ? (
          <Spinner />
        ) : clients.length === 0 ? (
          <EmptyState icon={Users} title="No clients yet">
            Add one here or from the Client Tracker app. Both go through the same gateway.
          </EmptyState>
        ) : (
          <TableWrap>
            <table className="w-full min-w-[640px]">
              <thead className="border-b border-border">
                <tr>
                  <th className={th}>Name</th>
                  <th className={th}>Email</th>
                  <th className={th}>Notes</th>
                  <th className={th}>Added</th>
                  <th className={th}>
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {clients.map((c) => (
                  <tr key={c.id} className="transition-colors hover:bg-surface-2/50">
                    <td className={`${td} font-medium`}>{c.name}</td>
                    <td className={`${td} text-fg-muted`}>{c.email || "—"}</td>
                    <td className={`${td} max-w-xs truncate text-fg-muted`}>{c.notes || "—"}</td>
                    <td className={`${td} whitespace-nowrap text-fg-muted`}>{relative(c.created_at)}</td>
                    <td className={`${td} text-right`}>
                      <div className="flex justify-end gap-1">
                        {can("shares", "write") && (
                          <Button size="sm" variant="ghost" icon={Link2} onClick={() => setSharing(c)} aria-label={`Share ${c.name}`} title="Share" />
                        )}
                        {can("clients", "write") && (
                          <Button size="sm" variant="ghost" icon={Pencil} onClick={() => setEditing({ mode: "edit", client: c })} aria-label={`Edit ${c.name}`} title="Edit" />
                        )}
                        {can("clients", "delete") && (
                          <Button size="sm" variant="ghost" icon={Trash2} onClick={() => setToDelete(c)} aria-label={`Delete ${c.name}`} title="Delete" />
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        )}
      </Card>

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
        description="This removes the record for everyone in the workspace. Share links to it stop working."
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

      {sharing && <ShareDialog open clients={clients} initialClientId={sharing.id} onClose={() => setSharing(null)} />}
    </>
  );
}
