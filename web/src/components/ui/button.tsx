import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md font-medium transition-colors disabled:pointer-events-none disabled:opacity-45 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        // `accent` is for the ONE affirmative action on a surface (approve, endorse). If two
        // elements on a screen are accent-coloured, neither reads as the primary decision.
        accent:
          "bg-[--color-accent] text-[--color-accent-ink] hover:brightness-110",
        default:
          "border border-[--color-line-strong] bg-[--color-raised] text-[--color-ink] hover:border-[--color-ink-3]",
        ghost:
          "text-[--color-ink-2] hover:bg-[--color-raised] hover:text-[--color-ink]",
        danger:
          "border border-[--color-danger]/40 bg-[--color-danger-soft] text-[--color-danger] hover:border-[--color-danger]",
        link: "text-[--color-accent] underline-offset-4 hover:underline",
      },
      size: {
        sm: "h-7 px-2.5 text-[11px]",
        default: "h-8 px-3 text-xs",
        lg: "h-9 px-4 text-[13px]",
        icon: "size-8",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export interface ButtonProps
  extends React.ComponentProps<"button">,
    VariantProps<typeof buttonVariants> {}

export function Button({
  className,
  variant,
  size,
  type = "button",
  ...props
}: ButtonProps) {
  return (
    // type defaults to "button": a bare <button> inside a form submits it, which is how a
    // secondary action silently performs the primary one.
    <button
      type={type}
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    />
  );
}

export { buttonVariants };
