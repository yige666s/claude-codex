import { FormEvent, ReactNode } from "react";
import { BrandLogo } from "../../components/brand/BrandLogo";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import type { Status } from "../workspace/workspaceTypes";

type AuthPageProps = {
  mode: "login" | "register";
  status: Status;
  onModeChange: (mode: "login" | "register") => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  statusLine: (status: Status) => ReactNode;
};

export function AuthPage({ mode, status, onModeChange, onSubmit, statusLine }: AuthPageProps) {
  return (
    <main className="auth-page">
      <form className="auth-card" onSubmit={onSubmit}>
        <div className="auth-head">
          <BrandLogo />
          <div>
            <h1>AgentAPI</h1>
            <p>Consumer agent workspace</p>
          </div>
        </div>
        <div className="segmented">
          <Button type="button" variant={mode === "login" ? "primary" : "ghost"} onClick={() => onModeChange("login")}>Login</Button>
          <Button type="button" variant={mode === "register" ? "primary" : "ghost"} onClick={() => onModeChange("register")}>Register</Button>
        </div>
        <label>
          Email
          <Input name="email" type="email" autoComplete="email" required />
        </label>
        {mode === "register" && (
          <label>
            Name
            <Input name="displayName" autoComplete="name" />
          </label>
        )}
        <label>
          Password
          <Input name="password" type="password" aria-label="Password" autoComplete={mode === "login" ? "current-password" : "new-password"} required minLength={8} />
        </label>
        {mode === "register" && (
          <label>
              Confirm
              <Input name="confirmPassword" type="password" aria-label="Repeat secret" autoComplete="new-password" required minLength={8} />
          </label>
        )}
        <Button className="wide" variant="primary" type="submit">{mode === "login" ? "Login" : "Create Account"}</Button>
        {statusLine(status)}
      </form>
    </main>
  );
}
