import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

/*
 * Status colour is semantic, never decorative. The hue is always paired with the label text,
 * so the meaning survives for anyone who cannot distinguish the colours.
 */
const badgeVariants = cva(
  "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em]",
  {
    variants: {
      tone: {
        neutral:
          "border-[--color-line-strong] bg-[--color-raised] text-[--color-ink-2]",
        accent:
          "border-[--color-accent]/30 bg-[--color-accent-soft] text-[--color-accent]",
        pending:
          "border-[--color-pending]/30 bg-[--color-pending-soft] text-[--color-pending]",
        danger:
          "border-[--color-danger]/30 bg-[--color-danger-soft] text-[--color-danger]",
        info: "border-[--color-info]/30 bg-[--color-info-soft] text-[--color-info]",
      },
    },
    defaultVariants: { tone: "neutral" },
  },
);

export interface BadgeProps
  extends React.ComponentProps<"span">,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, tone, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ tone }), className)} {...props} />;
}

export { badgeVariants };
