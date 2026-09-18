import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowRight, CircleDashed, Inbox } from "lucide-react";
import {
  listDecisions,
  listPeople,
  listWorkBoard,
  type Decision,
  type Person,
  type WorkBoardItem,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";

/**
 * My work — the default screen, and the one the product is judged on.
 *
 * The list comes from the SERVER, projected for the signed-in person. That
 * matters beyond convenience: the previous version fetched every task in the
 * organisation and filtered in the browser, which means the browser had already
 * been given everything. Here the set a person may see is decided server-side
 * from the authenticated identity, so one person cannot ask for another's board.
 *
 * Work is grouped by WHAT THE PERSON OWES, not by lifecycle state. "I am
 * accountable for this", "I am executing this for someone else" and "this is an
 * offer I have not answered" are different obligations that happen to involve the
 * same person; merging them hides which one they actually carry.
 *
 * Decisions are deliberately a separate section pointing at the inbox rather than
 * a group of tasks. "My decisions" and "my commitments" are different things, and
 * folding a question into a task list turns answering it into doing the work.
 *
 * Completed work is not shown: this is an obligation list, and finished items live
 * on their task and project surfaces rather than padding this one.
 */
export function MyWorkPage() {
  const [board, setBoard] = useState<WorkBoardItem[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [people, setPeople] = useState<Person[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    Promise.all([listWorkBoard(), listDecisions(), listPeople()])
      .then(([b, d, p]) => {
        if (cancelled) return;
        setBoard(b);
        setDecisions(d);
        setPeople(p);
      })
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  const nameOf = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of people) m.set(p.id, p.name);
    return (id: string) => m.get(id) || id;
  }, [people]);

  const groups = useMemo(() => {
    const blocked = board.filter((b) => b.blocker_task_id);
    const blockedIds = new Set(blocked.map((b) => b.task_id));
    return [
      {
        key: "accountable",
        title: "You are accountable",
        hint: "You own the outcome, whoever does the work.",
        items: board.filter(
          (b) => b.relevance === "accountable" && !blockedIds.has(b.task_id),
        ),
      },
      {
        key: "executing",
        title: "You are executing",
        hint: "Someone else owns the outcome; you are doing the work.",
        items: board.filter(
          (b) => b.relevance === "executing" && !blockedIds.has(b.task_id),
        ),
      },
      {
        key: "offered",
        title: "Offered to you",
        hint: "Not yours until you accept it.",
        items: board.filter((b) => b.relevance === "handoff_offered"),
      },
      {
        key: "blocked",
        title: "Going nowhere",
        hint: "Something else has to move first. Whose work it is, is named.",
        items: blocked,
      },
    ].filter((g) => g.items.length);
  }, [board]);

  const nothing =
    !board.length && !decisions.length;

  return (
    <>
      <PageHeader
        eyebrow="My work"
        title="What is on your plate"
        description="What you carry, what is waiting on an answer, and what cannot move yet."
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
        ) : nothing ? (
          <Panel className="p-8 text-center">
            <CircleDashed className="mx-auto size-5 text-[--color-ink-3]" />
            <p className="mt-3 text-[13px] font-medium">Nothing on your plate</p>
            <p className="mx-auto mt-1 max-w-md text-[11px] leading-relaxed text-[--color-ink-3]">
              Work appears here when you own it, when you are executing it for
              someone else, or when someone offers it to you.
            </p>
          </Panel>
        ) : (
          <>
            {decisions.length ? (
              <section>
                <div className="mb-2 flex items-baseline gap-2">
                  <h2 className="text-[12px] font-semibold">Waiting on your decision</h2>
                  <span className="tabular text-[11px] text-[--color-ink-3]">
                    {decisions.length}
                  </span>
                  <span className="text-[11px] text-[--color-ink-3]">
                    Answering these is a different act from doing the work.
                  </span>
                </div>
                <Link
                  to="/inbox"
                  className="group flex items-center gap-3 rounded-[--radius-panel] border border-[--color-pending]/40 bg-[--color-pending-soft] px-4 py-3"
                >
                  <span className="min-w-0 flex-1 text-[12px]">
                    {decisions.length} request
                    {decisions.length === 1 ? "" : "s"} need an answer from you
                  </span>
                  <Badge tone="pending">open inbox</Badge>
                  <ArrowRight className="size-3.5 shrink-0 transition-transform group-hover:translate-x-0.5" />
                </Link>
              </section>
            ) : null}

            {groups.map((group) => (
              <section key={group.key}>
                <div className="mb-2 flex items-baseline gap-2">
                  <h2 className="text-[12px] font-semibold">{group.title}</h2>
                  <span className="tabular text-[11px] text-[--color-ink-3]">
                    {group.items.length}
                  </span>
                  {group.hint ? (
                    <span className="text-[11px] text-[--color-ink-3]">{group.hint}</span>
                  ) : null}
                </div>
                <div className="space-y-1.5">
                  {group.items.map((b) => (
                    <BoardRow
                      key={b.task_id}
                      item={b}
                      blockerOwner={
                        b.blocker_owner_id ? nameOf(b.blocker_owner_id) : ""
                      }
                    />
                  ))}
                </div>
              </section>
            ))}
          </>
        )}
      </div>
    </>
  );
}

function BoardRow({
  item,
  blockerOwner,
}: {
  item: WorkBoardItem;
  blockerOwner: string;
}) {
  return (
    <Link
      to={`/tasks/${item.task_id}`}
      className="group flex items-center gap-3 rounded-[--radius-panel] border border-[--color-line] bg-[--color-surface] px-4 py-3 transition-colors hover:border-[--color-line-strong]"
    >
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[12px] font-medium">{item.title}</span>
        <span className="mt-0.5 flex flex-wrap items-center gap-2 text-[11px] text-[--color-ink-3]">
          <span>{item.status.replace(/_/g, " ")}</span>
          {item.blocker_task_id ? (
            <>
              <span>·</span>
              <span>
                waiting on “{item.blocker_title}”
                {blockerOwner ? ` — ${blockerOwner}` : ""}
              </span>
            </>
          ) : null}
        </span>
      </span>
      {item.blocker_task_id ? <Badge tone="danger">blocked</Badge> : null}
      {item.relevance === "handoff_offered" ? (
        <Badge tone="pending">offer</Badge>
      ) : null}
      <ArrowRight className="size-3.5 shrink-0 text-[--color-ink-3] transition-transform group-hover:translate-x-0.5" />
    </Link>
  );
}
