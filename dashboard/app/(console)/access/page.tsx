"use client";

import { useState, type FormEvent } from "react";
import { Check, Minus, UserPlus } from "lucide-react";
import { useToast } from "@/components/toast";
import { Badge, Button, Card, CardHeader, ErrorNote, Field, Input, PageHeader, Select, Spinner, TableWrap, td, th } from "@/components/ui";
import { api, errorText } from "@/lib/api";
import { useResource, useSession } from "@/lib/session";
import type { Member, Op, Role } from "@/lib/types";

const ROLES: Role[] = ["owner", "employee", "accountant"];
const OPS: Op[] = ["read", "write", "delete"];

const roleBlurb: Record<Role, string> = {
  owner: "Everything, including members and deletes.",
  employee: "Reads and edits records. Cannot delete.",
  accountant: "Read-only, everywhere.",
};

const tableLabel: Record<string, string> = {
  apps: "Apps",
  audit_logs: "Audit log",
  clients: "Clients",
  companies: "Workspace",
  heal_proposals: "Healing proposals",
  shares: "Share links",
};

export default function AccessPage() {
  const { me, policy } = useSession();
  const notify = useToast();
  const members = useResource<{ members: Member[] }>("/api/v1/memberships");
  const [saving, setSaving] = useState(false);
  const [role, setRole] = useState<Role>("employee");
  const isOwner = me.role === "owner";

  async function add(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const formEl = e.currentTarget;
    const f = new FormData(formEl);
    setSaving(true);
    try {
      await api("/api/v1/memberships", { method: "POST", body: { email: String(f.get("email")).trim(), role: f.get("role") } });
      notify("success", `${f.get("email")} now has ${f.get("role")} access.`);
      formEl.reset();
      await members.reload();
    } catch (err) {
      notify("error", errorText(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <PageHeader
        title="Access"
        description="Who is in this workspace and what each role may do. The same rules are compiled into Postgres row-level security, so they hold even if an app forgets to check."
      />

      <div className="grid gap-6 lg:grid-cols-5">
        <Card className="lg:col-span-3">
          <CardHeader title="Members" description={members.data ? `${members.data.members.length} people` : undefined} />
          {members.error ? (
            <div className="p-5">
              <ErrorNote onRetry={members.reload}>{members.error.text}</ErrorNote>
            </div>
          ) : !members.data ? (
            <Spinner />
          ) : (
            <ul className="divide-y divide-border">
              {members.data.members.map((m) => (
                <li key={m.user_id} className="flex items-center justify-between gap-3 px-5 py-3">
                  <span className="truncate">
                    {m.email}
                    {m.user_id === me.user_id && <span className="ml-2 text-xs text-fg-subtle">you</span>}
                  </span>
                  <Badge tone={m.role === "owner" ? "info" : "neutral"}>{m.role}</Badge>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader title="Add a member" description={isOwner ? "They need an account first. Adding an existing member changes their role." : "Only owners can add members."} />
          <form onSubmit={add} className="space-y-4 p-5">
            <Field label="Email">
              <Input name="email" type="email" required disabled={!isOwner} />
            </Field>
            <Field label="Role" hint={roleBlurb[role]}>
              <Select name="role" value={role} onChange={(e) => setRole(e.target.value as Role)} disabled={!isOwner} className="capitalize">
                {ROLES.map((r) => (
                  <option key={r} value={r}>
                    {r[0].toUpperCase() + r.slice(1)}
                  </option>
                ))}
              </Select>
            </Field>
            <Button type="submit" variant="primary" icon={UserPlus} loading={saving} disabled={!isOwner} className="w-full">
              Add member
            </Button>
          </form>
        </Card>
      </div>

      <Card className="mt-6">
        <CardHeader title="Permission matrix" description={`From policy.json. Rows are matched on ${policy.tenant_field}; anything not listed is denied.`} />
        <TableWrap>
          <table className="w-full min-w-[620px]">
            <thead className="border-b border-border">
              <tr>
                <th className={th}>Resource</th>
                {ROLES.map((r) => (
                  <th key={r} className={`${th} capitalize`}>
                    {r}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {Object.entries(policy.tables).map(([table, rule]) => (
                <tr key={table}>
                  <td className={td}>
                    <p className="font-medium">{tableLabel[table] ?? table}</p>
                    <p className="font-mono text-xs text-fg-subtle">{table}</p>
                  </td>
                  {ROLES.map((r) => (
                    <td key={r} className={td}>
                      <div className="flex flex-wrap gap-1">
                        {OPS.map((op) =>
                          rule[op].includes(r) ? (
                            <Badge key={op} tone="success" icon={Check}>
                              {op}
                            </Badge>
                          ) : (
                            <span key={op} className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-fg-subtle line-through decoration-fg-subtle/50">
                              <Minus className="size-3" strokeWidth={1.5} />
                              {op}
                            </span>
                          ),
                        )}
                      </div>
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </TableWrap>
      </Card>
    </>
  );
}
