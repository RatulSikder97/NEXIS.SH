import Image from "next/image";

type LogoProps = {
  className?: string;
  priority?: boolean;
  /** Visual size preset; uses full SVG intrinsic ratio (540×140) */
  variant?: "nav" | "footer" | "hero";
};

const variantClass: Record<NonNullable<LogoProps["variant"]>, string> = {
  /** Readable lockup in the sticky header */
  nav: "h-12 w-auto sm:h-[52px] md:h-[58px]",
  footer: "h-14 w-auto sm:h-16 md:h-[72px] max-w-full",
  hero: "h-16 w-auto sm:h-20 md:h-24 max-w-full",
};

export function Logo({
  className,
  priority = false,
  variant = "nav",
}: LogoProps) {
  return (
    <Image
      src="/logo.svg"
      alt="Nexis"
      width={540}
      height={140}
      priority={priority}
      unoptimized
      className={[variantClass[variant], "min-w-0 shrink-0 object-left object-contain", className]
        .filter(Boolean)
        .join(" ")}
    />
  );
}
