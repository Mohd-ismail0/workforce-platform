import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowRight, CircleDashed, Inbox } from "lucide-react";
import {
  listDecisions,
  listTasks,
  listProposals,
  type Decision,
  type Proposal,
  type Task,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";
import { useSession } from "@/app/session";

/**
 * My work.
 *
 * The default screen, and the one the product is judged on: a person should be able to see what
 * is theirs, what is waiting, and what needs them, without reading a lifecycle state.
 *
 * Statuses are grouped into four buckets that answer different questions rather than mirroring
 * the state machine:
 *   Waiting on you  - it cannot move without this person
 *   In flight       - someone or something is working
 *   Blocked         - it cannot proceed, and the reason is named
 *   Recently done   - it finished, so the list is not only an ever-growing backlog
 */
export function MyWorkPage() {
  const { me } = useSession();
  const [tasks, setTasks] = useState<Task[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [proposals, setProposals] = useState<Proposal[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    Promise.all([listTasks(), listDecisions(), listProposals()])
      .then(([t, d, p]) => {
        if (cancelled) return;
        setTasks(t);
        setDecisions(d);
        setProposals(p);
      })
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  // "My work" must mean MINE.
  //
  // The list endpoints are organisation-scoped, so without this filter every colleague's tasks
  // appear here. That reads as a personal backlog while actually being the whole organisation's
  // — the worst kind of wrong, because it looks plausible.
  const mine = tasks.filter(
    (t) => !me || t.owner_id === me.id || t.assignee_id === me.id,
  );

  // Highest revision per task. A task can carry several proposals, and taking whichever the
  // list happened to return last would sometimes present an older revision as the current one.
  const proposalByTask = new Map<string, Proposal>();
  for (const p of proposals) {
    const existing = proposalByTask.get(p.task_id);
    if (!existing || p.revision > existing.revision) {
      proposalByTask.set(p.task_id, p);
    }
  }
  const needing = new Set(decisions.map((d) => d.task_id));

  const open = mine.filter((t) => !["done", "cancelled"].includes(t.status));
  const groups: Array<{ key: string; title: string; hint: string; items: Task[] }> =
    [
      {
        key: "yours",
        title: "Waiting on you",
        hint: "Nothing else can happen until you decide.",
        items: open.filter((t) => needing.has(t.id)),
      },
      {
        key: "flight",
        title: "In flight",
        hint: "Being prepared or executed right now.",
        items: open.filter(
          (t) =>
            !needing.has(t.id) &&
            ["in_progress", "in_review", "ready"].includes(t.status),
        ),
      },
      {
        key: "blocked",
        title: "Blocked",
        hint: "Cannot proceed. The reason is on the task.",
        items: open.filter((t) => t.status === "blocked"),
      },
      {
        key: "done",
        title: "Recently completed",
        hint: "",
        items: mine
          .filter((t) => ["done", "cancelled"].includes(t.status))
          .slice(0, 5),
      },
    ].filter((g) => g.items.length);

  return (
    <>
      <PageHeader
        eyebrow="My work"
        title="What is on your plate"
        description="Everything you own, what is waiting on a decision, and what has just finished."
        actions={
          <Link
            to="/inbox"
            className="inline-flex items-center gap-1.5 rounded-md border border-[--color-line-strong] px-3 py-1.5 text-[11px] hover:border-[--color-ink-3]"
          >
            <Inbox className="size-3.5" />
            Go to inbox
          </Link>
        }
      />

      <div className="space-y-5 p-6">
        {error ? (
          <Panel className="border-[--color-danger]/40 p-4">
            <p className="text-[12px] font-medium">Could not load your work</p>
            <p className="mt-1 text-[11px] text-[--color-ink-3]">{error}</p>
          </Panel>
        ) : loading ? (
          <p className="text-[12px] text-[--color-ink-3]">Loading…</p>
        ) : !mine.length ? (
          <Panel className="p-8 text-center">
            <CircleDashed className="mx-auto size-5 text-[--color-ink-3]" />
            <p className="mt-3 text-[13px] font-medium">No work yet</p>
            <p className="mx-auto mt-1 max-w-md text-[11px] leading-relaxed text-[--color-ink-3]">
              Tasks appear here when they are assigned to you or to an agent
              working on your behalf.
            </p>
          </Panel>
        ) : (
          groups.map((group) => (
            <section key={group.key}>
              <div className="mb-2 flex items-baseline gap-2">
                <h2 className="text-[12px] font-semibold">{group.title}</h2>
                <span className="tabular text-[11px] text-[--color-ink-3]">
                  {group.items.length}
                </span>
                {group.hint ? (
                  <span className="text-[11px] text-[--color-ink-3]">
                    {group.hint}
                  </span>
                ) : null}
              </div>
              <div className="space-y-1.5">
                {group.items.map((t) => (
                  <TaskRow
                    key={t.id}
                    task={t}
                    proposal={proposalByTask.get(t.id)}
                    needsYou={needing.has(t.id)}
                  />
                ))}
              </div>
            </section>
          ))
        )}
      </div>
    </>
  );
}

function TaskRow({
  task,
  proposal,
  needsYou,
}: {
  task: Task;
  proposal?: Proposal;
  needsYou: boolean;
}) {
  return (
    <Link
      to={`/tasks/${task.id}`}
      className="group flex items-center gap-3 rounded-[--radius-panel] border border-[--color-line] bg-[--color-surface] px-4 py-3 transition-colors hover:border-[--color-line-strong]"
    >
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[12px] font-medium">
          {task.title}
        </span>
        <span className="mt-0.5 flex items-center gap-2 text-[11px] text-[--color-ink-3]">
          <span>{task.status.replace(/_/g, " ")}</span>
          {proposal ? (
            <>
              <span>·</span>
              <span>
                revision {proposal.revision} · {proposal.status.replace(/_/g, " ")}
              </span>
            </>
          ) : null}
        </span>
      </span>
      {needsYou ? <Badge tone="pending">needs you</Badge> : null}
      <ArrowRight className="size-3.5 shrink-0 text-[--color-ink-3] transition-transform group-hover:translate-x-0.5" />
    </Link>
  );
}
