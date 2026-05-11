"use client";

import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)] disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-[var(--color-primary)] text-[var(--color-primary-foreground)] hover:opacity-90",
        // Legacy alias kept for Stage 6 → Stage 7 transition; existing sections still pass variant="primary".
        primary: "bg-[var(--color-primary)] text-[var(--color-primary-foreground)] hover:opacity-90",
        ghost: "hover:bg-[var(--color-muted)] text-[var(--color-foreground)]",
        outline: "border border-[var(--color-border)] hover:bg-[var(--color-muted)] text-[var(--color-foreground)]",
        link: "text-[var(--color-primary)] underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-md px-3",
        // Legacy alias for navbar links — Stage 7 will replace with `sm`.
        nav: "h-8 rounded-md px-3 text-xs",
        lg: "h-11 rounded-md px-6 text-base",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  }
);

type LegacyAnchorProps = {
  /**
   * Legacy convenience prop kept for Stage 6 → Stage 7 transition. When set, the button renders an
   * `<a>` element instead of a `<button>`, preserving the old call sites in Hero/CTA/Navbar.
   */
  href?: string;
};

export interface ButtonProps
  extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "color">,
    VariantProps<typeof buttonVariants>,
    LegacyAnchorProps {
  asChild?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, href, ...props }, ref) => {
    const classes = cn(buttonVariants({ variant, size, className }));
    if (asChild) {
      return <Slot ref={ref} className={classes} {...props} />;
    }
    if (href) {
      const { type: _type, ...anchorProps } = props as React.ButtonHTMLAttributes<HTMLButtonElement>;
      void _type;
      return (
        <a
          href={href}
          className={classes}
          {...(anchorProps as unknown as React.AnchorHTMLAttributes<HTMLAnchorElement>)}
        />
      );
    }
    return <button ref={ref} className={classes} {...props} />;
  }
);
Button.displayName = "Button";

export { buttonVariants };
