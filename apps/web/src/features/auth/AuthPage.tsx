import { FormEvent, ReactNode } from "react";
import { ArrowRight, Check, LockKeyhole, Mail, UserRound } from "lucide-react";
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
  const isLogin = mode === "login";
  return (
    <main className="auth-page">
      <section className="auth-stage" aria-label="AgentAPI access">
        <div className="auth-brand-badge">
          <BrandLogo />
        </div>
        <div className="auth-photo-panel" aria-hidden="true">
          <div className="auth-photo-copy">
            <span>{isLogin ? "SIGN" : "JOIN"}</span>
            <span>{isLogin ? "IN" : "UP"}</span>
          </div>
          <div className="auth-photo-link">{isLogin ? "CREATE ACCOUNT" : "SIGN IN"}</div>
        </div>
        <form className="auth-card" onSubmit={onSubmit}>
          <div className="auth-close-dot" aria-hidden="true">×</div>
          <div className="auth-head">
            <p>{isLogin ? "Welcome back" : "Start building"}</p>
            <h1>{isLogin ? "Sign in to AgentAPI" : "Create your workspace"}</h1>
          </div>
          <div className="auth-mode-switch" role="tablist" aria-label="Authentication mode">
            <Button
              type="button"
              variant={isLogin ? "secondary" : "ghost"}
              onClick={() => onModeChange("login")}
              aria-selected={isLogin}
              role="tab"
            >
              Login
            </Button>
            <Button
              type="button"
              variant={!isLogin ? "secondary" : "ghost"}
              onClick={() => onModeChange("register")}
              aria-selected={!isLogin}
              role="tab"
            >
              Register
            </Button>
          </div>
          <div className="auth-fields">
            <label className="auth-field">
              <span>Email</span>
              <div className="auth-input-shell">
                <Mail size={17} />
                <Input name="email" type="email" autoComplete="email" placeholder="you@example.com" required />
              </div>
            </label>
            {!isLogin && (
              <label className="auth-field">
                <span>Name</span>
                <div className="auth-input-shell">
                  <UserRound size={17} />
                  <Input name="displayName" autoComplete="name" placeholder="Display name" />
                </div>
              </label>
            )}
            <label className="auth-field">
              <span>Password</span>
              <div className="auth-input-shell">
                <LockKeyhole size={17} />
                <Input name="password" type="password" aria-label="Password" autoComplete={isLogin ? "current-password" : "new-password"} placeholder="Password" required minLength={8} />
              </div>
            </label>
            {!isLogin && (
              <label className="auth-field">
                <span>Confirm</span>
                <div className="auth-input-shell">
                  <Check size={17} />
                  <Input name="confirmPassword" type="password" aria-label="Repeat secret" autoComplete="new-password" placeholder="Repeat password" required minLength={8} />
                </div>
              </label>
            )}
          </div>
          <div>
            <Button className="auth-submit" variant="primary" type="submit" size="lg">
              {isLogin ? "Login" : "Create Account"}
              <ArrowRight size={18} />
            </Button>
          </div>
          <div className="auth-alt-action">
            <span>{isLogin ? "No workspace yet?" : "Already a member?"}</span>
            <button type="button" onClick={() => onModeChange(isLogin ? "register" : "login")}>
              {isLogin ? "Register" : "Login"}
            </button>
          </div>
          {statusLine(status)}
        </form>
      </section>
    </main>
  );
}
