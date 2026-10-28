"use client";

import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CircleAlert, CircleCheck, X } from "lucide-react";

type Toast = { id: number; tone: "success" | "error"; text: string };
type Notify = (tone: Toast["tone"], text: string) => void;

const ToastContext = createContext<Notify>(() => {});

export function useToast() {
  return useContext(ToastContext);
}

let nextId = 1;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const dismiss = useCallback((id: number) => setToasts((t) => t.filter((x) => x.id !== id)), []);

  const notify = useCallback<Notify>(
    (tone, text) => {
      const id = nextId++;
      setToasts((t) => [...t.slice(-3), { id, tone, text }]);
      setTimeout(() => dismiss(id), tone === "error" ? 7000 : 4000);
    },
    [dismiss],
  );

  return (
    <ToastContext.Provider value={notify}>
      {children}
      <div className="pointer-events-none fixed inset-x-4 bottom-4 z-50 flex flex-col items-end gap-2 sm:left-auto sm:w-96" aria-live="polite">
        <AnimatePresence initial={false}>
          {toasts.map((t) => (
            <motion.div
              key={t.id}
              layout
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.98 }}
              transition={{ type: "spring", stiffness: 380, damping: 32 }}
              className="pointer-events-auto flex w-full items-start gap-3 rounded-xl border border-border bg-surface px-4 py-3 shadow-sm"
              role={t.tone === "error" ? "alert" : "status"}
            >
              {t.tone === "success" ? (
                <CircleCheck className="mt-0.5 size-4 shrink-0 text-success" strokeWidth={1.5} />
              ) : (
                <CircleAlert className="mt-0.5 size-4 shrink-0 text-danger" strokeWidth={1.5} />
              )}
              <p className="flex-1 text-[13px] leading-5">{t.text}</p>
              <button onClick={() => dismiss(t.id)} className="focus-ring rounded text-fg-subtle hover:text-fg" aria-label="Dismiss">
                <X className="size-4" strokeWidth={1.5} />
              </button>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
    </ToastContext.Provider>
  );
}
