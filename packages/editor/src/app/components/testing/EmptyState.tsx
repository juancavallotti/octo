"use client";

import type { ReactNode } from "react";

/**
 * The centred "nothing here yet" panel the Testing tab shows in place of a suite.
 *
 * There are several of these and they are not interchangeable — a draft that cannot
 * store a suite, a document with no flows, and a flow that simply has no tests yet are
 * three different situations, and telling the user which one they are in is the whole
 * job. Only the last is an invitation to do something.
 */
export default function EmptyState({
  icon,
  title,
  children,
  action,
}: {
  icon?: ReactNode;
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-1 items-center justify-center p-8">
      <div className="max-w-md text-center">
        {icon && (
          <div className="mb-3 flex justify-center text-zinc-400 dark:text-zinc-500">
            {icon}
          </div>
        )}
        <h2 className="text-sm font-medium text-zinc-700 dark:text-zinc-200">{title}</h2>
        {children && (
          <p className="mt-2 text-xs leading-relaxed text-zinc-500 dark:text-zinc-400">
            {children}
          </p>
        )}
        {action && <div className="mt-4 flex justify-center">{action}</div>}
      </div>
    </div>
  );
}
