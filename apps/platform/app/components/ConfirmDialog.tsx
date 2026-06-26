"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { AlertTriangle } from "lucide-react";

/**
 * In-app confirmation, replacing the browser's `window.confirm`. A `ConfirmProvider`
 * high in the tree renders a single modal; descendants call `useConfirm()` and
 * `await confirm({ ... })`, which resolves to true/false when the user chooses. The
 * styling mirrors the other overlays (e.g. DeployModal).
 */

export interface ConfirmOptions {
  title: string;
  body?: string;
  /** Confirm button label; defaults to "Confirm". */
  confirmLabel?: string;
  /** Style the confirm action as destructive (red). */
  danger?: boolean;
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn | null>(null);

/** The confirm function. Throws if used outside a ConfirmProvider. */
export function useConfirm(): ConfirmFn {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error("useConfirm must be used within a ConfirmProvider");
  return ctx;
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<{
    opts: ConfirmOptions;
    resolve: (ok: boolean) => void;
  } | null>(null);

  const confirm = useCallback<ConfirmFn>(
    (opts) => new Promise<boolean>((resolve) => setPending({ opts, resolve })),
    [],
  );

  const settle = useCallback(
    (ok: boolean) => {
      pending?.resolve(ok);
      setPending(null);
    },
    [pending],
  );

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending && <ConfirmModal opts={pending.opts} onSettle={settle} />}
    </ConfirmContext.Provider>
  );
}

function ConfirmModal({
  opts,
  onSettle,
}: {
  opts: ConfirmOptions;
  onSettle: (ok: boolean) => void;
}) {
  // Escape cancels; Enter confirms — keyboard parity with native confirm().
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onSettle(false);
      if (e.key === "Enter") onSettle(true);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onSettle]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={opts.title}
      onMouseDown={() => onSettle(false)}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm"
    >
      <div
        onMouseDown={(e) => e.stopPropagation()}
        className="flex w-full max-w-sm flex-col overflow-hidden rounded-xl border border-black/10 bg-white shadow-xl dark:border-white/10 dark:bg-zinc-900"
      >
        <div className="flex items-start gap-3 px-4 py-4">
          {opts.danger && (
            <AlertTriangle
              size={18}
              className="mt-0.5 shrink-0 text-red-500"
              aria-hidden
            />
          )}
          <div className="min-w-0">
            <h3 className="text-sm font-semibold">{opts.title}</h3>
            {opts.body && (
              <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
                {opts.body}
              </p>
            )}
          </div>
        </div>
        <footer className="flex justify-end gap-2 border-t border-black/10 px-4 py-3 dark:border-white/10">
          <button
            type="button"
            onClick={() => onSettle(false)}
            className="rounded-md px-3 py-1 text-sm text-zinc-600 transition-colors hover:bg-black/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.08]"
          >
            Cancel
          </button>
          <button
            type="button"
            autoFocus
            onClick={() => onSettle(true)}
            className={`rounded-md px-3 py-1 text-sm font-medium text-white transition-colors ${
              opts.danger
                ? "bg-red-600 hover:bg-red-500"
                : "bg-sky-600 hover:bg-sky-500"
            }`}
          >
            {opts.confirmLabel ?? "Confirm"}
          </button>
        </footer>
      </div>
    </div>
  );
}
