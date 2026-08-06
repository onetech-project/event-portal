import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AdminLayout from "./layout";
import { clearToken, storeToken } from "@/lib/auth";

const replace = vi.fn();
let pathname = "/admin/orders";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace, push: vi.fn() }),
  usePathname: () => pathname,
}));

// The nav pulls in the API client and icons; the guard is what is under test.
vi.mock("@/components/admin/admin-nav", () => ({
  AdminNav: () => <nav data-testid="admin-nav" />,
}));

const inAnHour = () => new Date(Date.now() + 3_600_000).toISOString();

beforeEach(() => {
  replace.mockClear();
  pathname = "/admin/orders";
  window.localStorage.clear();
});

describe("AdminLayout guard", () => {
  /**
   * The regression test for the bug this feature exists to fix: the first
   * client render has no localStorage available, and a guard that redirects
   * from it sends a signed-in admin to the login screen on every refresh.
   */
  it("does not redirect on the first render, before the session is resolved", () => {
    storeToken("jwt-abc", inAnHour());

    render(
      <AdminLayout>
        <p>orders</p>
      </AdminLayout>,
    );

    expect(replace).not.toHaveBeenCalled();
  });

  it("renders the page for a signed-in admin", async () => {
    storeToken("jwt-abc", inAnHour());

    render(
      <AdminLayout>
        <p>orders</p>
      </AdminLayout>,
    );

    expect(await screen.findByText("orders")).toBeInTheDocument();
    expect(screen.getByTestId("admin-nav")).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });

  it("shows a neutral loading state instead of the login screen while resolving", () => {
    render(
      <AdminLayout>
        <p>orders</p>
      </AdminLayout>,
    );

    expect(screen.getByText(/checking your session/i)).toBeInTheDocument();
    expect(screen.queryByText("orders")).not.toBeInTheDocument();
  });

  it("redirects to login with the requested path once resolved anonymous", async () => {
    render(
      <AdminLayout>
        <p>orders</p>
      </AdminLayout>,
    );

    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith("/admin/login?next=%2Fadmin%2Forders"),
    );
  });

  it("explains an expired session in the redirect", async () => {
    storeToken("jwt-old", new Date(Date.now() - 1_000).toISOString());

    render(
      <AdminLayout>
        <p>orders</p>
      </AdminLayout>,
    );

    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith(
        "/admin/login?next=%2Fadmin%2Forders&reason=expired",
      ),
    );
  });

  it("never guards the login page itself", () => {
    pathname = "/admin/login";

    render(
      <AdminLayout>
        <p>sign in</p>
      </AdminLayout>,
    );

    expect(screen.getByText("sign in")).toBeInTheDocument();
    expect(screen.queryByTestId("admin-nav")).not.toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });

  // A sign-out in this tab must take effect immediately; the native storage
  // event only reaches other tabs.
  it("stops rendering admin content when the session is cleared in this tab", async () => {
    storeToken("jwt-abc", inAnHour());

    render(
      <AdminLayout>
        <p>orders</p>
      </AdminLayout>,
    );
    expect(await screen.findByText("orders")).toBeInTheDocument();

    clearToken();

    await waitFor(() => expect(screen.queryByText("orders")).not.toBeInTheDocument());
  });
});
