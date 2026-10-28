"use client";

import { useEffect, useState, type ReactNode } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { motion } from "framer-motion";
import {
  Activity,
  AppWindow,
  ChevronsUpDown,
  ExternalLink,
  KeyRound,
  LayoutGrid,
  Link2,
  LogOut,
  Menu,
  Moon,
  ScrollText,
  ShieldCheck,
  Sun,
  Users,
  Wrench,
  X,
} from "lucide-react";
import { useSession } from "@/lib/session";
import { useToast } from "./toast";
import { errorText } from "@/lib/api";
import { cx } from "./ui";

const SAMPLE_APP_URL = process.env.NEXT_PUBLIC_SAMPLE_APP_URL ?? "http://localhost:3001";

const nav = [
  { href: "/overview", label: "Overview", icon: LayoutGrid },
  { href: "/apps", label: "Apps", icon: AppWindow },
  { href: "/clients", label: "Clients", icon: Users },
  { href: "/shares", label: "Shares", icon: Link2 },
  { href: "/access", label: "Access", icon: KeyRound },
  { href: "/healing", label: "Healing", icon: Wrench },
  { href: "/audit", label: "Audit log", icon: ScrollText },
];

export function Brand() {
  return (
    <span className="flex items-center gap-2">
      <span className="grid size-7 place-items-center rounded-lg bg-primary text-primary-fg">
        <ShieldCheck className="size-4" strokeWidth={1.5} />
      </span>
      <span className="font-display text-xl leading-none">Cloud</span>
    </span>
  );
}

function ThemeToggle() {
  const [dark, setDark] = useState(false);
  useEffect(() => setDark(document.documentElement.classList.contains("dark")), []);
  const toggle = () => {
    const next = !dark;
    setDark(next);
    document.documentElement.classList.toggle("dark", next);
    try {
      localStorage.setItem("theme", next ? "dark" : "light");
    } catch {}
  };
  return (
    <button onClick={toggle} className="focus-ring grid size-8 place-items-center rounded-lg text-fg-muted hover:bg-surface-2 hover:text-fg" aria-label={dark ? "Use light theme" : "Use dark theme"}>
      {dark ? <Sun className="size-4" strokeWidth={1.5} /> : <Moon className="size-4" strokeWidth={1.5} />}
    </button>
  );
}

function WorkspaceSwitcher() {
  const { me, companies, switchCompany } = useSession();
  const notify = useToast();
  const [busy, setBusy] = useState(false);
  return (
    <label className="relative block">
      <span className="sr-only">Workspace</span>
      <select
        value={me.company_id}
        disabled={busy || companies.length < 2}
        onChange={async (e) => {
          setBusy(true);
          try {
            await switchCompany(e.target.value);
          } catch (err) {
            notify("error", errorText(err));
          } finally {
            setBusy(false);
          }
        }}
        className="focus-ring h-10 w-full cursor-pointer appearance-none truncate rounded-lg border border-border bg-surface pl-3 pr-8 text-left text-[13px] font-medium disabled:cursor-default"
      >
        {companies.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </select>
      <ChevronsUpDown className="pointer-events-none absolute right-2.5 top-3 size-4 text-fg-subtle" strokeWidth={1.5} />
    </label>
  );
}

function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const { me, signOut } = useSession();
  return (
    <div className="flex h-full flex-col gap-6 p-4">
      <div className="px-1 pt-1">
        <Brand />
      </div>
      <WorkspaceSwitcher />
      <nav className="flex flex-col gap-0.5" aria-label="Console">
        {nav.map(({ href, label, icon: Icon }) => {
          const active = pathname === href || pathname.startsWith(href + "/");
          return (
            <Link
              key={href}
              href={href}
              onClick={onNavigate}
              className={cx(
                "focus-ring relative flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13px] transition-colors",
                active ? "font-medium text-fg" : "text-fg-muted hover:bg-surface-2 hover:text-fg",
              )}
              aria-current={active ? "page" : undefined}
            >
              {active && <motion.span layoutId="nav-active" className="absolute inset-0 rounded-lg bg-surface-2" transition={{ type: "spring", stiffness: 500, damping: 40 }} />}
              <Icon className="relative size-4" strokeWidth={1.5} />
              <span className="relative">{label}</span>
            </Link>
          );
        })}
      </nav>
      <a
        href={SAMPLE_APP_URL}
        target="_blank"
        rel="noreferrer"
        className="focus-ring flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13px] text-fg-muted hover:bg-surface-2 hover:text-fg"
      >
        <Activity className="size-4" strokeWidth={1.5} />
        Open Client Tracker
        <ExternalLink className="ml-auto size-3.5" strokeWidth={1.5} />
      </a>
      <div className="mt-auto border-t border-border pt-4">
        <div className="flex items-center gap-2 px-1">
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13px] font-medium">{me.email}</p>
            <p className="text-xs capitalize text-fg-muted">{me.role}</p>
          </div>
          <ThemeToggle />
          <button onClick={signOut} className="focus-ring grid size-8 place-items-center rounded-lg text-fg-muted hover:bg-surface-2 hover:text-fg" aria-label="Sign out" title="Sign out">
            <LogOut className="size-4" strokeWidth={1.5} />
          </button>
        </div>
      </div>
    </div>
  );
}

export function Shell({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const pathname = usePathname();
  useEffect(() => setOpen(false), [pathname]);

  return (
    <div className="min-h-dvh lg:grid lg:grid-cols-[15rem_1fr]">
      <aside className="sticky top-0 hidden h-dvh border-r border-border bg-bg lg:block">
        <Sidebar />
      </aside>

      <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-border bg-bg/90 px-4 backdrop-blur lg:hidden">
        <Brand />
        <button onClick={() => setOpen(true)} className="focus-ring grid size-11 place-items-center rounded-lg text-fg-muted" aria-label="Open menu">
          <Menu className="size-5" strokeWidth={1.5} />
        </button>
      </header>
      {open && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setOpen(false)} />
          <div className="absolute inset-y-0 left-0 w-72 border-r border-border bg-bg">
            <button onClick={() => setOpen(false)} className="focus-ring absolute right-3 top-3 grid size-9 place-items-center rounded-lg text-fg-muted" aria-label="Close menu">
              <X className="size-5" strokeWidth={1.5} />
            </button>
            <Sidebar onNavigate={() => setOpen(false)} />
          </div>
        </div>
      )}

      <main className="min-w-0">
        <motion.div
          key={pathname}
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ type: "spring", stiffness: 300, damping: 34 }}
          className="mx-auto max-w-6xl px-4 py-8 md:px-8 md:py-12"
        >
          {children}
        </motion.div>
      </main>
    </div>
  );
}
