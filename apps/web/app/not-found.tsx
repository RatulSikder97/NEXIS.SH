import Link from "next/link";

import { Logo } from "@/components/ui/Logo";

export default function NotFound() {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-8 px-6 py-24">
      <Link href="/" className="flex flex-col items-center gap-4 text-center">
        <Logo variant="hero" priority />
        <p className="text-[15px] text-text-secondary">That page does not exist.</p>
        <span className="text-[14px] font-medium text-primary">Return home</span>
      </Link>
    </div>
  );
}
