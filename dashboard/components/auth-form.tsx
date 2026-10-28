"use client";

import { useState, type FormEvent } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { motion } from "framer-motion";
import { ArrowRight } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { Brand } from "./shell";
import { Button, ErrorNote, Field, Input } from "./ui";

type Mode = "login" | "register";

/** Field-level placement for the codes a user can fix in the form. */
const fieldFor: Record<string, "email" | "password" | "company"> = {
  AUTH_002: "password",
  AUTH_003: "email",
  AUTH_009: "password",
};

export function AuthForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const params = useSearchParams();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    setBusy(true);
    setError(null);
    try {
      await api(`/api/v1/auth/${mode}`, {
        method: "POST",
        body: {
          email: form.get("email"),
          password: form.get("password"),
          ...(mode === "register" ? { company_name: form.get("company") } : {}),
        },
      });
      const next = params.get("next");
      router.replace(next && next.startsWith("/") && !next.startsWith("//") ? next : "/overview");
    } catch (err) {
      setError(err instanceof ApiError ? err : null);
      setBusy(false);
    }
  }

  const fieldError = (name: "email" | "password" | "company") => (error && fieldFor[error.code] === name ? error.text : undefined);
  const formError = error && !fieldFor[error.code] ? error.text : null;
  const isRegister = mode === "register";

  return (
    <div className="grid min-h-dvh lg:grid-cols-2">
      <div className="flex flex-col px-6 py-8 sm:px-12">
        <Brand />
        <motion.div
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ type: "spring", stiffness: 260, damping: 30 }}
          className="m-auto w-full max-w-sm py-12"
        >
          <h1 className="font-display text-4xl">{isRegister ? "Create your workspace" : "Welcome back"}</h1>
          <p className="mt-2 text-fg-muted">
            {isRegister ? "You will be its owner. Invite your team once you are in." : "Sign in to manage your apps and who can see what."}
          </p>
          <form onSubmit={submit} className="mt-8 space-y-4" noValidate>
            <Field label="Work email" error={fieldError("email")}>
              <Input name="email" type="email" autoComplete="email" required autoFocus />
            </Field>
            <Field label="Password" hint={isRegister ? "At least 8 characters." : undefined} error={fieldError("password")}>
              <Input name="password" type="password" autoComplete={isRegister ? "new-password" : "current-password"} required minLength={8} />
            </Field>
            {isRegister && (
              <Field label="Workspace name" hint="Usually your company or team." error={fieldError("company")}>
                <Input name="company" autoComplete="organization" required />
              </Field>
            )}
            {formError && <ErrorNote>{formError}</ErrorNote>}
            <Button type="submit" variant="primary" className="w-full" loading={busy}>
              {isRegister ? "Create workspace" : "Sign in"}
              {!busy && <ArrowRight className="size-4" strokeWidth={1.5} />}
            </Button>
          </form>
          <p className="mt-6 text-center text-[13px] text-fg-muted">
            {isRegister ? "Already have an account? " : "New here? "}
            <Link href={isRegister ? "/login" : "/register"} className="focus-ring rounded font-medium text-fg underline-offset-4 hover:underline">
              {isRegister ? "Sign in" : "Create a workspace"}
            </Link>
          </p>
        </motion.div>
      </div>

      <aside className="relative hidden overflow-hidden border-l border-border bg-surface lg:flex lg:flex-col lg:justify-end lg:p-12">
        <div className="absolute inset-0 opacity-[0.35] [background-image:linear-gradient(rgb(var(--border))_1px,transparent_1px),linear-gradient(90deg,rgb(var(--border))_1px,transparent_1px)] [background-size:32px_32px]" />
        <div className="relative max-w-md">
          <p className="font-display text-3xl leading-snug">The app forgot the filter. The database did not.</p>
          <p className="mt-4 text-fg-muted">
            Every app your team ships runs behind one gateway. Postgres row-level security, generated from a single policy file, decides who sees which rows, and every
            decision lands in the audit log.
          </p>
        </div>
      </aside>
    </div>
  );
}
