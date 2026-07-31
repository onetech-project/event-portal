"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useSyncExternalStore, type ReactNode } from "react";

import { AdminNav } from "@/components/admin/admin-nav";
import { Loading } from "@/components/ui/feedback";
import { isAuthenticated } from "@/lib/auth";

/** Re-reads the token when another tab signs in or out. */
function subscribeToAuth(onChange: () => void) {
  window.addEventListener("storage", onChange);
  return () => window.removeEventListener("storage", onChange);
}

/**
 * Guards every /admin/* page except login.
 *
 * This is a convenience redirect, not a security boundary — the API rejects any
 * request without a valid token regardless of what the browser renders.
 *
 * The token lives in localStorage, which does not exist during the server
 * render, so it is read through useSyncExternalStore with an unauthenticated
 * server snapshot; the real value arrives at hydration.
 */
export default function AdminLayout({ children }: Readonly<{ children: ReactNode }>) {
  const pathname = usePathname();
  const router = useRouter();
  const isLoginPage = pathname === "/admin/login";

  const authed = useSyncExternalStore(
    subscribeToAuth,
    () => isAuthenticated(),
    () => false,
  );

  useEffect(() => {
    if (!isLoginPage && !authed) {
      router.replace("/admin/login");
    }
  }, [authed, isLoginPage, router]);

  if (isLoginPage) {
    return <>{children}</>;
  }

  if (!authed) {
    return <Loading label="Checking your session…" />;
  }

  return (
    <div className="flex min-h-full flex-1 flex-col">
      <AdminNav />
      {children}
    </div>
  );
}
