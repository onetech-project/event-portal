"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { PageHeading, StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api-client";
import { storeToken } from "@/lib/auth";
import { useAdminLogin } from "@/lib/queries";
import { loginSchema, type LoginForm } from "@/lib/schemas";

export default function AdminLoginPage() {
  const router = useRouter();
  const login = useAdminLogin();

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "" },
  });

  const onSubmit = handleSubmit(async (values) => {
    const response = await login.mutateAsync(values);
    storeToken(response.token, response.expires_at);
    router.replace("/admin/events");
  });

  const error = login.error instanceof ApiError ? login.error : null;

  return (
    <main className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center px-6 py-10">
      <PageHeading title="Admin sign in" />

      <Card>
        <CardContent className="space-y-4">
          {error ? <StatusAlert>{error.message}</StatusAlert> : null}

          <form onSubmit={onSubmit} aria-label="Sign in" className="space-y-4">
            <Field label="Email" error={errors.email?.message}>
              <Input type="email" autoComplete="username" {...register("email")} />
            </Field>
            <Field label="Password" error={errors.password?.message}>
              <Input
                type="password"
                autoComplete="current-password"
                {...register("password")}
              />
            </Field>
            <Button type="submit" disabled={login.isPending} className="w-full">
              {login.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
