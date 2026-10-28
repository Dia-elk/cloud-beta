"use client";

import { useState } from "react";
import { ShieldCheck, ShieldX } from "lucide-react";
import type { AuditSummary } from "@/lib/types";
import { compact } from "@/lib/format";

type Day = { date: string; allowed: number; denied: number };

/** The last 7 calendar days, zero-filled, so gaps read as "no traffic". */
function lastSevenDays(perDay: AuditSummary["per_day"]): Day[] {
  const byDate = new Map(perDay.map((d) => [d.date, d]));
  const out: Day[] = [];
  for (let i = 6; i >= 0; i--) {
    const d = new Date();
    d.setDate(d.getDate() - i);
    const key = d.toISOString().slice(0, 10);
    const row = byDate.get(key);
    out.push({ date: key, allowed: row?.allowed ?? 0, denied: row?.denied ?? 0 });
  }
  return out;
}

function niceMax(n: number): number {
  if (n <= 4) return 4;
  const pow = 10 ** Math.floor(Math.log10(n));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * pow).find((s) => s * 4 >= n) ?? 10 * pow;
  return step * 4;
}

const weekday = (iso: string) => new Date(iso + "T12:00:00").toLocaleDateString("en", { weekday: "short" });
const longDate = (iso: string) => new Date(iso + "T12:00:00").toLocaleDateString("en", { weekday: "long", month: "short", day: "numeric" });

/** Requests per day, stacked allowed / denied. Values also live in the audit log table. */
export function ActivityChart({ perDay }: { perDay: AuditSummary["per_day"] }) {
  const days = lastSevenDays(perDay);
  const max = niceMax(Math.max(...days.map((d) => d.allowed + d.denied)));
  const ticks = [0, 1, 2, 3, 4].map((i) => (max / 4) * i);
  const [hover, setHover] = useState<number | null>(null);
  const H = 160;
  const GAP = 2;

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-4 text-[13px] text-fg-muted" aria-hidden>
        <span className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-sm bg-chart-allowed" />
          <ShieldCheck className="size-3.5" strokeWidth={1.5} />
          Allowed
        </span>
        <span className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-sm bg-chart-denied" />
          <ShieldX className="size-3.5" strokeWidth={1.5} />
          Denied
        </span>
      </div>
      <div className="relative flex gap-3" role="img" aria-label={`Requests per day for the last 7 days: ${days.map((d) => `${weekday(d.date)} ${d.allowed} allowed, ${d.denied} denied`).join("; ")}`}>
        <div className="relative w-8 shrink-0 text-right text-[11px] tabular-nums text-fg-subtle" style={{ height: H }}>
          {ticks.map((t) => (
            <span key={t} className="absolute right-0 -translate-y-1/2" style={{ top: H - (t / max) * H }}>
              {compact(t)}
            </span>
          ))}
        </div>
        <div className="relative flex-1">
          <div className="absolute inset-x-0 top-0" style={{ height: H }}>
            {ticks.map((t) => (
              <div key={t} className="absolute inset-x-0 border-t border-border/70" style={{ top: H - (t / max) * H }} />
            ))}
          </div>
          <div className="relative grid grid-cols-7 gap-2" style={{ height: H }}>
            {days.map((d, i) => {
              const total = d.allowed + d.denied;
              const hAllowed = (d.allowed / max) * H;
              const hDenied = (d.denied / max) * H;
              const both = d.allowed > 0 && d.denied > 0;
              return (
                <div
                  key={d.date}
                  className="group relative flex h-full cursor-default flex-col items-center justify-end"
                  onMouseEnter={() => setHover(i)}
                  onMouseLeave={() => setHover(null)}
                >
                  <div className="flex w-full max-w-9 flex-col justify-end" style={{ height: H }}>
                    {d.denied > 0 && (
                      <div className="w-full rounded-t bg-chart-denied transition-opacity group-hover:opacity-90" style={{ height: Math.max(hDenied - (both ? GAP : 0), 2), marginBottom: both ? GAP : 0 }} />
                    )}
                    {d.allowed > 0 && (
                      <div className={`w-full bg-chart-allowed transition-opacity group-hover:opacity-90 ${d.denied > 0 ? "" : "rounded-t"}`} style={{ height: Math.max(hAllowed, 2) }} />
                    )}
                  </div>
                  {hover === i && (
                    <div className="pointer-events-none absolute bottom-full z-10 mb-2 w-max rounded-lg border border-border bg-surface px-3 py-2 text-[12px] shadow-sm">
                      <p className="font-medium text-fg">{longDate(d.date)}</p>
                      <p className="mt-1 flex items-center gap-1.5 text-fg-muted">
                        <span className="size-2 rounded-sm bg-chart-allowed" /> {d.allowed} allowed
                      </p>
                      <p className="flex items-center gap-1.5 text-fg-muted">
                        <span className="size-2 rounded-sm bg-chart-denied" /> {d.denied} denied
                      </p>
                      <p className="mt-1 text-fg-subtle">{total} total</p>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
          <div className="mt-2 grid grid-cols-7 gap-2 text-center text-[11px] text-fg-subtle">
            {days.map((d) => (
              <span key={d.date}>{weekday(d.date)}</span>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
