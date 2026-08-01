"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";

import { AdminNav } from "@/components/admin/admin-nav";
import { Loading } from "@/components/ui/feedback";
import { readSession, subscribeToSession, type SessionState } from "@/lib/auth";

/**
 * Guards every /admin/* page except login.
 *
 * This is a convenience redirect, not a security boundary — the API rejects any
 * request without a valid token regardless of what the browser renders.
 *
 * The session lives in localStorage, which does not exist during the server
 * render *or* during hydration, so it is resolved in an effect that runs after
 * mount. Until then the state is "loading" and nothing redirects. Reading it
 * any earlier is what made every refresh bounce to the login screen: the first
 * client render necessarily sees no session, and a redirect fired from that
 * render never gives the real value a chance to arrive.
 */
export default function AdminLayout({ children }: Readonly<{ children: ReactNode }>) {
  const pathname = usePathname();
  const router = useRouter();
  const isLoginPage = pathname === "/admin/login";

  const [session, setSession] = useState<SessionState>({ status: "loading" });

  useEffect(() => {
    const sync = () => setSession(readSession());
    sync();
    // Covers a sign-out in this tab and in any other one.
    return subscribeToSession(sync);
  }, []);

  useEffect(() => {
    if (isLoginPage || session.status !== "anonymous") return;

    // Remember where they were headed so signing in does not dump them on the
    // admin home, and say why the session ended when we know.
    const params = new URLSearchParams({ next: pathname });
    if (session.reason === "expired") params.set("reason", "expired");
    router.replace(`/admin/login?${params.toString()}`);
  }, [session, isLoginPage, pathname, router]);

  if (isLoginPage) {
    return <>{children}</>;
  }

  if (session.status !== "authenticated") {
    return <Loading label="Checking your session…" />;
  }

  return (
    <div className="flex min-h-full flex-1 flex-col">
      <AdminNav />
      {children}
    </div>
  );
}
