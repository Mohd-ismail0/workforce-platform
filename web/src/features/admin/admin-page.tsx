import { useEffect, useState } from "react";
import {
  Activity as ActivityIcon,
  ArrowLeft,
  Bot,
  FileCheck2,
  HelpCircle,
  LayoutDashboard,
  Package,
  PlayCircle,
  Repeat,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import { listTasks, type Task } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useSession } from "@/app/session";
import {
  Activity,
  Agents,
  Decisions,
  Gates,
  Handoffs,
  Integrations,
  Overview,
  Registry,
  Runs,
  TaskDetail,
} from "@/features/admin/console";

/**
 * Admin.
 *
 * Where the technical surfaces live now. These are real working screens over the kernel's own
 * vocabulary — gate schemas, run lifecycles, registry transitions, raw payloads — and they are
 * genuinely useful for operating and debugging the platform.
 *
 * They are behind Admin because they are not the employee experience: someone doing their work
 * should never have to read a business key or a lifecycle state to decide whether to approve
 * something. Each will be replaced by a product surface (a decision card, a task workspace, a
 * preview) as those land; nothing here is deleted while it is still the only way to see
 * something true.
 */

// Nav icons are LUCIDE icons, not the console page components of the same name — passing a
// page component where an icon belongs is a type error, and it would render the whole page
// inside the nav button.
const TABS = [
  { id: "overview", label: "Overview", icon: LayoutDashboard },
  { id: "gates", label: "Questions", icon: HelpCircle },
  { id: "decisions", label: "Proposals", icon: ShieldCheck },
  { id: "runs", label: "Runs", icon: PlayCircle },
  { id: "handoffs", label: "Handoffs", icon: Repeat },
  { id: "registry", label: "Registry", icon: Package },
  { id: "integrations", label: "Integrations", icon: FileCheck2 },
  { id: "agents", label: "Agent defs", icon: Bot },
  { id: "activity", label: "Activity", icon: ActivityIcon },
] as const;

export function AdminPage() {
  const { me } = useSession();
  const [tab, setTab] = useState<(typeof TABS)[number]["id"]>("overview");
  const [error, setError] = useState("");
  const [openTask, setOpenTask] = useState<Task>();

  if (!me) return null;

  return (
    <div className="legacy">
      <div className="flex items-start justify-between gap-6 border-b border-[--color-line] px-6 py-5">
        <div>
          <p className="eyebrow">Admin</p>
          <h1 className="mt-1 flex items-center gap-2 text-lg font-semibold tracking-tight">
            <Terminal className="size-4 text-[--color-ink-3]" />
            Technical console
          </h1>
          <p className="mt-1 max-w-2xl text-[11px] leading-relaxed text-[--color-ink-3]">
            The kernel&apos;s own surfaces, in its own vocabulary. Use these to
            inspect and operate the platform; use the workspace to do work.
          </p>
        </div>
        <Badge tone="neutral">not the employee experience</Badge>
      </div>

      <nav className="flex flex-wrap gap-1 border-b border-[--color-line] px-4 py-2">
        {TABS.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            onClick={() => {
              setTab(id);
              setOpenTask(undefined);
            }}
            className={
              tab === id
                ? "flex items-center gap-1.5 rounded-md bg-[--color-raised] px-2.5 py-1.5 text-[11px] font-medium text-[--color-ink]"
                : "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[11px] text-[--color-ink-3] hover:bg-[--color-raised] hover:text-[--color-ink-2]"
            }
          >
            <Icon className="size-3.5" />
            {label}
          </button>
        ))}
      </nav>

      {error ? (
        <div className="flex items-center justify-between gap-3 border-b border-[--color-danger]/40 bg-[--color-danger-soft] px-6 py-2">
          <span className="text-[11px] text-[--color-danger]">{error}</span>
          <button
            className="text-[11px] text-[--color-danger] underline"
            onClick={() => setError("")}
          >
            Dismiss
          </button>
        </div>
      ) : null}

      <div className="console-host p-6">
        {openTask ? (
          <div className="console-surface">
            <Button
              variant="ghost"
              size="sm"
              className="mb-3"
              onClick={() => setOpenTask(undefined)}
            >
              <ArrowLeft className="size-3.5" />
              Back to tasks
            </Button>
            <TaskDetail
              task={openTask}
              close={() => setOpenTask(undefined)}
              refresh={() => undefined}
              setError={setError}
            />
          </div>
        ) : (
          <ConsoleScreen
            tab={tab}
            me={me}
            setError={setError}
            onOpenTask={setOpenTask}
          />
        )}
      </div>
    </div>
  );
}

function ConsoleScreen({
  tab,
  me,
  setError,
  onOpenTask,
}: {
  tab: (typeof TABS)[number]["id"];
  me: NonNullable<ReturnType<typeof useSession>["me"]>;
  setError: (v: string) => void;
  onOpenTask: (t: Task) => void;
}) {
  switch (tab) {
    case "gates":
      return <Shell><Gates setError={setError} /></Shell>;
    case "decisions":
      return <Shell><Decisions me={me} setError={setError} /></Shell>;
    case "runs":
      return <Shell><Runs setError={setError} /></Shell>;
    case "handoffs":
      return <Shell><Handoffs me={me} setError={setError} /></Shell>;
    case "registry":
      return <Shell><Registry me={me} setError={setError} /></Shell>;
    case "integrations":
      return <Shell><Integrations setError={setError} /></Shell>;
    case "agents":
      return <Shell><Agents setError={setError} /></Shell>;
    case "activity":
      return <Shell><Activity setError={setError} /></Shell>;
    default:
      return (
        <Shell>
          <Overview me={me} setError={setError} />
          <TaskPicker onOpen={onOpenTask} onError={setError} />
        </Shell>
      );
  }
}

/**
 * The console pages paint their own light surface. Wrapping them keeps the old stylesheet's
 * assumptions (`body`-level colours, full-bleed panels) from bleeding into the dark shell.
 */
function Shell({ children }: { children: React.ReactNode }) {
  return <div className="console-surface">{children}</div>;
}

function TaskPicker({
  onOpen,
  onError,
}: {
  onOpen: (t: Task) => void;
  onError: (v: string) => void;
}) {
  const [tasks, setTasks] = useState<Task[]>([]);
  useEffect(() => {
    listTasks()
      .then(setTasks)
      .catch((e: Error) => onError(e.message));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  if (!tasks.length) return null;
  return (
    <div className="mt-6">
      <p className="eyebrow mb-2">Open a task in the console</p>
      <div className="flex flex-wrap gap-1.5">
        {tasks.map((t) => (
          <button
            key={t.id}
            onClick={() => onOpen(t)}
            className="rounded border border-[--color-line-strong] px-2 py-1 text-[10px] text-[--color-ink-2] hover:border-[--color-ink-3]"
          >
            {t.title}
          </button>
        ))}
      </div>
    </div>
  );
}
