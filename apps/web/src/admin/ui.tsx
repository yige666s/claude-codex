import { ReactNode } from "react";
import { AlertCircle, ShieldCheck } from "lucide-react";
import { Alert, AlertDescription } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { cn } from "../lib/utils";

export function AdminShell({ rail, sidebar, children, navigationOpen = false }: { rail: ReactNode; sidebar: ReactNode; children: ReactNode; navigationOpen?: boolean }) {
  return (
    <main className={cn("admin-shell", navigationOpen && "navigation-open")}>
      {rail}
      {sidebar}
      <section className="admin-main">{children}</section>
    </main>
  );
}

export function AdminSidebar({ children }: { children: ReactNode }) {
  return <aside className="admin-sidebar">{children}</aside>;
}

export function AdminPageHeader({
  breadcrumb,
  leading,
  title,
  badge,
  description,
  search,
  action
}: {
  breadcrumb?: ReactNode;
  leading?: ReactNode;
  title: ReactNode;
  badge?: ReactNode;
  description?: ReactNode;
  search?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <header className="admin-header">
      <div className="admin-header-copy">
        <div className="admin-header-breadcrumb">
          {leading}
          {breadcrumb}
        </div>
        <div className="admin-header-title-row">
          <h1>{title}</h1>
          {badge}
        </div>
        {description && <p>{description}</p>}
      </div>
      <div className="admin-header-tools">
        {search}
        {action}
      </div>
    </header>
  );
}

export function AdminSplitPane({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("admin-skill-layout", className)}>{children}</div>;
}

export function AdminListPanel({ children, className }: { children: ReactNode; className?: string }) {
  return <section className={cn("admin-list-panel", className)}>{children}</section>;
}

export function AdminDetailPanel({ children, className }: { children: ReactNode; className?: string }) {
  return <section className={cn("admin-detail-panel", className)}>{children}</section>;
}

export function AdminEmptyState({
  icon,
  title,
  children,
  className
}: {
  icon?: ReactNode;
  title: ReactNode;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("admin-empty", className)}>
      {icon}
      <strong>{title}</strong>
      {children && <div className="admin-empty-content">{children}</div>}
    </div>
  );
}

export function AdminSearchBox({
  icon,
  children,
  className
}: {
  icon?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("admin-search", className)}>
      {icon}
      {children}
    </div>
  );
}

export function AdminFilterBar({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("admin-filter-row shadcn-admin-filter-bar", className)}>{children}</div>;
}

export function AdminField({ label, children, className }: { label?: string; children: ReactNode; className?: string }) {
  return (
    <label className={cn("admin-field shadcn-admin-field", className)}>
      {label && <span>{label}</span>}
      {children}
    </label>
  );
}

export function AdminActionRow({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("admin-action-row shadcn-admin-action-row", className)}>{children}</div>;
}

export function AdminSectionNotice({
  tone = "default",
  children,
  onDismiss
}: {
  tone?: "default" | "success" | "warning" | "destructive";
  children: ReactNode;
  onDismiss?: () => void;
}) {
  return (
    <Alert className={cn("admin-banner", tone === "destructive" ? "error" : "ok")} variant={tone}>
      {tone === "destructive" ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
      <AlertDescription>{children}</AlertDescription>
      {onDismiss && (
        <Button className="icon ghost" variant="ghost" size="icon" onClick={onDismiss} title="Dismiss" aria-label="Dismiss">
          <span aria-hidden="true">x</span>
        </Button>
      )}
    </Alert>
  );
}

export function MetricCard({
  label,
  value,
  detail,
  tone = "default"
}: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  tone?: "default" | "success" | "warning" | "destructive";
}) {
  return (
    <Card className={cn("metric-card", tone !== "default" && `metric-card-${tone}`)}>
      <CardContent>
        <small>{label}</small>
        <strong>{value}</strong>
        {detail && <span>{detail}</span>}
      </CardContent>
    </Card>
  );
}

export function DataList({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("admin-table shadcn-data-list", className)}>{children}</div>;
}

export function DataListRow({
  children,
  active,
  asButton = false,
  onClick,
  className
}: {
  children: ReactNode;
  active?: boolean;
  asButton?: boolean;
  onClick?: () => void;
  className?: string;
}) {
  const classes = cn("admin-table-row", asButton && "button-row", active && "active", className);
  if (asButton) {
    return (
      <Button className={classes} variant="ghost" onClick={onClick}>
        {children}
      </Button>
    );
  }
  return <div className={classes}>{children}</div>;
}

export function AdminStatusBadge({ value }: { value: string }) {
  const normalized = value.toLowerCase().replace(/[^a-z0-9_-]+/g, "-");
  const variant = normalized === "active" || normalized === "succeeded" || normalized === "completed" || normalized === "passed"
    ? "success"
    : normalized === "failed" || normalized === "banned" || normalized === "disabled" || normalized === "ignored"
      ? "destructive"
      : normalized === "warning" || normalized === "in-review" || normalized === "running" || normalized === "queued"
        ? "warning"
        : "secondary";
  return <Badge className={`status-badge ${normalized}`} variant={variant}>{value}</Badge>;
}
