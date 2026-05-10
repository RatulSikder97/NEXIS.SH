import type { ReactNode } from "react";

export function Button({
  children,
  href,
  variant = "primary",
  size = "lg",
  className,
}: {
  children: ReactNode;
  href?: string;
  variant?: "primary" | "ghost";
  size?: "lg" | "md" | "nav";
  className?: string;
}) {
  const heightClass =
    size === "nav" ? "h-[32px]" : size === "md" ? "h-[44px]" : "h-[44px]";

  const base = [
    "inline-flex items-center justify-center gap-2 rounded-[6px] font-medium",
    "transition-transform duration-150 ease-out",
    "border border-solid",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-background",
    heightClass,
    size === "nav" ? "text-[14px] px-[14px]" : "text-[14px] px-[18px]",
  ].join(" ");

  const classes =
    variant === "primary"
      ? [
          base,
          "bg-primary text-on-primary",
          "hover:scale-[1.01] border-primary",
          "focus-visible:ring-accent focus-visible:ring-offset-[3px]",
        ].join(" ")
      : [
          base,
          "bg-transparent text-accent border-border-hover",
          "hover:border-primary",
          "focus-visible:ring-primary focus-visible:ring-offset-[3px]",
        ].join(" ");

  const finalClassName = [classes, className].filter(Boolean).join(" ");

  if (href) {
    return (
      <a href={href} className={finalClassName}>
        {children}
      </a>
    );
  }

  return <button className={finalClassName}>{children}</button>;
}

