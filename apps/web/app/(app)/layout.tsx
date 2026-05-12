"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";

import { ThemeAwareLogo } from "@/components/ThemeAwareLogo";
import { Button } from "@/components/ui/Button";
import { auth } from "@/lib/auth";

export default function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();

  async function logout() {
    try {
      await auth.logout();
    } finally {
      router.push("/sign-in");
    }
  }

  return (
    <>
      <header className="sticky top-0 z-40 border-b border-[var(--color-border)] bg-[var(--color-background)]">
        <div className="mx-auto max-w-[1200px] flex h-16 items-center justify-between px-6">
          <Link href="/dashboard">
            <ThemeAwareLogo />
          </Link>
          <Button variant="ghost" onClick={logout}>
            Log out
          </Button>
        </div>
      </header>
      <main className="mx-auto max-w-[1200px] px-6 py-10">{children}</main>
    </>
  );
}
