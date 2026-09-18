import * as React from "react";
import { cn } from "@/lib/utils";

const base =
  "w-full rounded-md border border-[--color-line-strong] bg-[--color-canvas] px-2.5 py-1.5 text-xs text-[--color-ink] placeholder:text-[--color-ink-3] focus:border-[--color-accent] focus:outline-none disabled:opacity-50";

export function Input({ className, ...props }: React.ComponentProps<"input">) {
  return <input className={cn(base, className)} {...props} />;
}

export function Textarea({
  className,
  ...props
}: React.ComponentProps<"textarea">) {
  return (
    <textarea className={cn(base, "min-h-20 resize-y", className)} {...props} />
  );
}

export function Select({
  className,
  ...props
}: React.ComponentProps<"select">) {
  return (
    <select
      className={cn(base, "appearance-none pr-7", className)}
      {...props}
    />
  );
}

export function Field({
  label,
  hint,
  children,
  className,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <label className={cn("grid gap-1.5", className)}>
      <span className="text-[11px] font-medium text-[--color-ink-2]">
        {label}
      </span>
      {children}
      {hint ? (
        <span className="text-[10px] text-[--color-ink-3]">{hint}</span>
      ) : null}
    </label>
  );
}
