import * as React from "react";
import { cn } from "@/lib/utils";

export function Panel({
  className,
  ...props
}: React.ComponentProps<"section">) {
  return (
    <section
      className={cn(
        "rounded-[--radius-panel] border border-[--color-line] bg-[--color-surface]",
        className,
      )}
      {...props}
    />
  );
}

export function PanelHeader({
  title,
  subtitle,
  actions,
  className,
}: {
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <header
      className={cn(
        "flex items-start justify-between gap-4 border-b border-[--color-line] px-4 py-3",
        className,
      )}
    >
      <div className="min-w-0">
        <h2 className="truncate text-[13px] font-semibold">{title}</h2>
        {subtitle ? (
          <p className="mt-0.5 text-[11px] text-[--color-ink-3]">{subtitle}</p>
        ) : null}
      </div>
      {actions ? (
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      ) : null}
    </header>
  );
}

export function PanelBody({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return <div className={cn("p-4", className)} {...props} />;
}

/**
 * Empty state.
 *
 * `reason` is required because an empty list has two very different meanings — "nothing needs
 * you" and "this is not built yet" — and a UI that renders them identically trains people to
 * distrust it. Every caller must say which one it is.
 */
export function Empty({
  title,
  reason,
  action,
  className,
}: {
  title: string;
  reason: string;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-2 px-6 py-12 text-center",
        className,
      )}
    >
      <p className="text-[13px] font-medium text-[--color-ink-2]">{title}</p>
      <p className="max-w-md text-[11px] leading-relaxed text-[--color-ink-3]">
        {reason}
      </p>
      {action ? <div className="mt-1">{action}</div> : null}
    </div>
  );
}
