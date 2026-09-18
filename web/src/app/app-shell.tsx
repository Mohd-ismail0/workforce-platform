import {
  Activity,
  BookOpen,
  Bot,
  FolderKanban,
  Inbox,
  LayoutDashboard,
  Settings2,
  Users,
} from "lucide-react";
import { NavLink, Outlet } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useSession } from "@/app/session";
import { useInboxCount } from "@/features/inbox/use-inbox-count";

/**
 * The six areas of the workspace, in the order a person needs them.
 *
 * This replaced ten flat pages named after kernel objects (Integrations, Runs, Registry,
 * Needs input). Those are real and still reachable, but under Admin: an employee should never
 * have to read a lifecycle state or a business key to do their work.
 */
const AREAS = [
  { to: "/", label: "My work", icon: LayoutDashboard, end: true },
  { to: "/inbox", label: "Inbox", icon: Inbox, badge: true },
  { to: "/projects", label: "Projects", icon: FolderKanban },
  { to: "/knowledge", label: "Knowledge", icon: BookOpen },
  { to: "/agents", label: "Agents", icon: Bot },
  { to: "/team", label: "Team", icon: Users },
] as const;

export function AppShell() {
  const { me, signOut, mode } = useSession();
  const pending = useInboxCount();

  return (
    <div className="flex h-dvh overflow-hidden bg-[--color-canvas]">
      <aside className="flex w-56 shrink-0 flex-col border-r border-[--color-line] bg-[--color-surface]">
        <div className="flex items-center gap-2.5 px-4 py-4">
          <span className="grid size-7 place-items-center rounded-md bg-[--color-accent] text-[13px] font-bold text-[--color-accent-ink]">
            W
          </span>
          <span className="text-[13px] font-semibold tracking-tight">
            workforce
          </span>
        </div>

        <nav className="flex flex-col gap-0.5 px-2">
          {AREAS.map(({ to, label, icon: Icon, ...rest }) => (
            <NavLink
              key={to}
              to={to}
              end={"end" in rest ? rest.end : false}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-[12px] transition-colors",
                  isActive
                    ? "bg-[--color-raised] font-medium text-[--color-ink]"
                    : "text-[--color-ink-2] hover:bg-[--color-raised] hover:text-[--color-ink]",
                )
              }
            >
              {({ isActive }) => (
                <>
                  <Icon
                    className={cn(
                      "size-4 shrink-0",
                      isActive ? "text-[--color-accent]" : "text-[--color-ink-3]",
                    )}
                  />
                  <span className="flex-1">{label}</span>
                  {"badge" in rest && rest.badge && pending > 0 ? (
                    <span className="min-w-5 rounded-full bg-[--color-pending-soft] px-1.5 text-center text-[10px] font-semibold text-[--color-pending]">
                      {pending}
                    </span>
                  ) : null}
                </>
              )}
            </NavLink>
          ))}
        </nav>

        <div className="mt-4 border-t border-[--color-line] px-2 pt-2">
          <NavLink
            to="/admin"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-[12px]",
                isActive
                  ? "bg-[--color-raised] text-[--color-ink]"
                  : "text-[--color-ink-3] hover:bg-[--color-raised] hover:text-[--color-ink-2]",
              )
            }
          >
            <Settings2 className="size-4 shrink-0" />
            Admin
          </NavLink>
        </div>

        <div className="mt-auto p-2">
          {/*
            Precise about what is and is not simulated. An earlier revision said "Nothing leaves
            this machine", which is false the moment an operator configures a real harness: model
            calls then traverse the gateway to a provider. Overstating containment is worse than
            saying less, because it is the kind of claim someone relies on.
          */}
          <div className="mb-2 rounded-md border border-[--color-line] px-2.5 py-2">
            <div className="flex items-center gap-1.5">
              <Activity className="size-3 text-[--color-ink-3]" />
              <span className="eyebrow">Development</span>
            </div>
            <p className="mt-1 text-[10px] leading-snug text-[--color-ink-3]">
              Business effects are simulated — no email, stock or document is
              really changed. Agent runs use the built-in simulator unless an
              operator has configured a real harness, in which case model calls
              leave via the gateway.
            </p>
          </div>

          <div className="flex items-center gap-2 rounded-md px-2 py-1.5">
            <span className="grid size-7 shrink-0 place-items-center rounded-full bg-[--color-raised] text-[11px] font-semibold text-[--color-ink-2]">
              {me?.name?.[0] ?? "?"}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[11px] font-medium">
                {me?.name}
              </span>
              <span className="block truncate text-[10px] text-[--color-ink-3]">
                {me?.org_id}
              </span>
            </span>
            <Badge tone={me?.role === "admin" ? "accent" : "neutral"}>
              {me?.role}
            </Badge>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="mt-1 w-full justify-start text-[11px]"
            onClick={signOut}
          >
            {mode === "session" ? "Sign out" : "Clear token"}
          </Button>
        </div>
      </aside>

      <main className="min-w-0 flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}

/**
 * Page chrome: eyebrow, title, and a slot for actions.
 *
 * Every page uses it so the title, spacing and action placement are identical everywhere —
 * a page that invents its own header is how a product starts to feel assembled rather than
 * designed.
 */
export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description?: string;
  actions?: React.ReactNode;
}) {
  return (
    <header className="flex items-start justify-between gap-6 border-b border-[--color-line] px-6 py-5">
      <div className="min-w-0">
        <p className="eyebrow">{eyebrow}</p>
        <h1 className="mt-1 text-lg font-semibold tracking-tight">{title}</h1>
        {description ? (
          <p className="mt-1 max-w-2xl text-[11px] leading-relaxed text-[--color-ink-3]">
            {description}
          </p>
        ) : null}
      </div>
      {actions ? (
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      ) : null}
    </header>
  );
}
