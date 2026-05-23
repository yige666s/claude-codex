import { FormEvent, ReactNode } from "react";
import { ArrowRight, Check, CheckCircle2, LockKeyhole, Mail, UserRound } from "lucide-react";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import type { Status } from "../workspace/workspaceTypes";

export type AuthMode = "login" | "register" | "forgot" | "reset";

type AuthPageProps = {
  mode: AuthMode;
  status: Status;
  forgotCooldownSeconds?: number;
  onModeChange: (mode: AuthMode) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  statusLine: (status: Status) => ReactNode;
};

export function AuthPage({ mode, status, forgotCooldownSeconds = 0, onModeChange, onSubmit, statusLine }: AuthPageProps) {
  const isLogin = mode === "login";
  const isRegister = mode === "register";
  const isForgot = mode === "forgot";
  const isReset = mode === "reset";
  const photoWords = isReset ? ["RESET", "KEY"] : isForgot ? ["MAIL", "LINK"] : isLogin ? ["SIGN", "IN"] : ["JOIN", "UP"];
  const submitDisabled = status.tone === "busy" || (isForgot && forgotCooldownSeconds > 0);
  const submitLabel = isReset
    ? "Reset Password"
    : isForgot
      ? forgotCooldownSeconds > 0
        ? `Send Reset Link (${forgotCooldownSeconds}s)`
        : "Send Reset Link"
      : isLogin
        ? "Login"
        : "Create Account";
  return (
    <main className="auth-page">
      <section className="auth-stage" aria-label="AgentAPI access">
        <div className="auth-photo-panel" aria-hidden="true">
          <div className="auth-photo-copy">
            <span>{photoWords[0]}</span>
            <span>{photoWords[1]}</span>
          </div>
          <div className="auth-photo-link">{isForgot || isReset ? "ACCOUNT RECOVERY" : isLogin ? "CREATE ACCOUNT" : "SIGN IN"}</div>
        </div>
        <form className="auth-card" onSubmit={onSubmit}>
          <div className="auth-close-dot" aria-hidden="true">×</div>
          <div className="auth-head">
            <p>{isReset ? "Choose a new password" : isForgot ? "Recover access" : isLogin ? "Welcome back" : "Start building"}</p>
            <h1>{isReset ? "Reset your password" : isForgot ? "Send a reset link" : isLogin ? "Sign in to AgentAPI" : "Create your workspace"}</h1>
          </div>
          {!isForgot && !isReset && (
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
                variant={isRegister ? "secondary" : "ghost"}
                onClick={() => onModeChange("register")}
                aria-selected={isRegister}
                role="tab"
              >
                Register
              </Button>
            </div>
          )}
          <div className="auth-fields">
            {!isReset && (
              <label className="auth-field">
                <span>Email</span>
                <div className="auth-input-shell">
                  <Mail size={17} />
                  <Input name="email" type="email" autoComplete="email" placeholder="you@example.com" required />
                </div>
              </label>
            )}
            {isRegister && (
              <label className="auth-field">
                <span>Name</span>
                <div className="auth-input-shell">
                  <UserRound size={17} />
                  <Input name="displayName" autoComplete="name" placeholder="Display name" />
                </div>
              </label>
            )}
            {!isForgot && (
              <label className="auth-field">
                <span>{isReset ? "New password" : "Password"}</span>
                <div className="auth-input-shell">
                  <LockKeyhole size={17} />
                  <Input name="password" type="password" aria-label="Password" autoComplete={isLogin ? "current-password" : "new-password"} placeholder={isReset ? "New password" : "Password"} required minLength={8} />
                </div>
              </label>
            )}
            {(isRegister || isReset) && (
              <label className="auth-field">
                <span>Confirm</span>
                <div className="auth-input-shell">
                  <Check size={17} />
                  <Input name="confirmPassword" type="password" aria-label="Repeat secret" autoComplete="new-password" placeholder="Repeat password" required minLength={8} />
                </div>
              </label>
            )}
          </div>
          {isLogin && (
            <div className="auth-forgot-action">
              <button type="button" onClick={() => onModeChange("forgot")}>Forgot password?</button>
            </div>
          )}
          <div>
            <Button className="auth-submit" variant="primary" type="submit" size="lg" disabled={submitDisabled}>
              {submitLabel}
              <ArrowRight size={18} />
            </Button>
          </div>
          <div className="auth-alt-action">
            <span>{isForgot || isReset ? "Remembered it?" : isLogin ? "No workspace yet?" : "Already a member?"}</span>
            <button type="button" onClick={() => onModeChange(isLogin ? "register" : "login")}>
              {isLogin ? "Register" : "Login"}
            </button>
          </div>
          {status.tone === "ok" ? (
            <div className="auth-success" role="status" aria-live="polite">
              <CheckCircle2 size={15} />
              <span>{status.text}</span>
            </div>
          ) : statusLine(status)}
        </form>
      </section>
    </main>
  );
}
