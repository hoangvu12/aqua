import { cva, type VariantProps } from "class-variance-authority";
import type { ButtonHTMLAttributes } from "react";
import { cn } from "@/lib/utils";

// shadcn-style button themed to Aqua tokens. Every variant carries default /
// hover / active / disabled states (product register: no half-built controls).
const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-[var(--radius-tile)] text-sm font-semibold transition-colors duration-150 ease-[var(--ease-out-quart)] outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:pointer-events-none disabled:opacity-45 select-none",
  {
    variants: {
      variant: {
        accent: "bg-accent text-on-accent active:bg-accent-hi",
        surface:
          "bg-surface text-fg border border-hairline active:bg-surface-hi",
        ghost: "bg-transparent text-fg-dim active:bg-surface",
      },
      size: {
        md: "h-12 px-4",
        lg: "h-14 px-5 text-base",
        sm: "h-9 px-3 text-xs",
        icon: "h-12 w-12",
      },
    },
    defaultVariants: { variant: "surface", size: "md" },
  },
);

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {}

export function Button({ className, variant, size, ...props }: ButtonProps) {
  return <button className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
