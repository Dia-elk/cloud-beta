"use client";

import type { ReactNode } from "react";
import { Shell } from "@/components/shell";
import { Spinner } from "@/components/ui";
import { SessionProvider } from "@/lib/session";

export default function ConsoleLayout({ children }: { children: ReactNode }) {
  return (
    <SessionProvider
      fallback={
        <div className="grid min-h-dvh place-items-center">
          <Spinner label="Loading your workspace" />
        </div>
      }
    >
      <Shell>{children}</Shell>
    </SessionProvider>
  );
}
