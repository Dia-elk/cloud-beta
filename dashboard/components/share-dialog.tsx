"use client";

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Dialog } from "./dialog";
import { useToast } from "./toast";
import { Button, Field, Input, Select } from "./ui";
import { api, errorText, GATEWAY } from "@/lib/api";
import { dateTime } from "@/lib/format";
import type { Client } from "@/lib/types";

const expiries = [
  { value: "1h", label: "1 hour" },
  { value: "24h", label: "24 hours" },
  { value: "168h", label: "7 days" },
];

/** Creates a read-only link to one client record. The link is shown once. */
export function ShareDialog({ clients, initialClientId, open, onClose, onCreated }: { clients: Client[]; initialClientId?: string; open: boolean; onClose: () => void; onCreated?: () => void }) {
  const notify = useToast();
  const [clientId, setClientId] = useState(initialClientId ?? "");
  const [expiry, setExpiry] = useState("24h");
  const [busy, setBusy] = useState(false);
  const [link, setLink] = useState<{ url: string; expires: string } | null>(null);
  const [copied, setCopied] = useState(false);

  const close = () => {
    setLink(null);
    setCopied(false);
    onClose();
  };

  const selected = clientId || initialClientId || "";

  return (
    <Dialog
      open={open}
      onClose={close}
      title={link ? "Share link ready" : "Share a client record"}
      description={link ? "Copy it now. For security it is shown only once and cannot be recovered." : "Anyone with the link can read this one record until it expires or you revoke it."}
    >
      {link ? (
        <div className="space-y-4">
          <div className="flex gap-2">
            <Input readOnly value={link.url} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
            <Button
              icon={copied ? Check : Copy}
              onClick={async () => {
                await navigator.clipboard.writeText(link.url);
                setCopied(true);
              }}
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
          <p className="text-[13px] text-fg-muted">Expires {dateTime(link.expires)}.</p>
          <div className="flex justify-end">
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          </div>
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            try {
              const r = await api<{ token: string; expires_at: string }>("/api/v1/shares", { method: "POST", body: { row_id: selected, expires_in: expiry } });
              setLink({ url: `${GATEWAY}/share/${r.token}`, expires: r.expires_at });
              onCreated?.();
            } catch (err) {
              notify("error", errorText(err));
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label="Client">
            <Select value={selected} onChange={(e) => setClientId(e.target.value)} required>
              <option value="" disabled>
                Choose a client
              </option>
              {clients.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Expires after">
            <Select value={expiry} onChange={(e) => setExpiry(e.target.value)}>
              {expiries.map((x) => (
                <option key={x.value} value={x.value}>
                  {x.label}
                </option>
              ))}
            </Select>
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!selected}>
              Create link
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
