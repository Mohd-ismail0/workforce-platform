import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { AlertTriangle, Check, CircleDot, ThumbsUp } from "lucide-react";
import {
  acceptHandoff,
  cancelHandoff,
  clarifyHandoff,
  declineHandoff,
  getTask,
  listGates,
  listHandoffs,
  listProposals,
  listRuns,
  type Gate,
  type HandoffOffer,
  type Proposal,
  type Task,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Panel, PanelHeader } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";
import { useSession } from "@/app/session";
import { OperationHeading, OperationPreview } from "@/features/task/previews";
import { GateForm } from "@/features/inbox/gate-form";
import { respondToGate } from "@/api";

/**
 * The canonical task workspace.
 *
 * Every view links here, and this page answers the whole question in one place instead of
 * scattering it across four backend-derived screens:
 *
 *   What is this?            objective and the task's own description
 *   Who is involved?         owner, assignee, and what it is waiting on
 *   What has been prepared?  the frozen proposal revision, rendered as the change it would make
 *   What do I do now?        the one available action for this person
 *   What happened?           activity, technical detail collapsed by default
 *
 * The technical panel at the bottom is deliberately closed. It is there for debugging and for
 * the admin surface, not because a person completing work should read it.
 */
export function TaskPage() {
  const { id = "" } = useParams();
  const { me } = useSession();
  const [task, setTask] = useState<Task>();
  const [proposals, setProposals] = useState<Proposal[]>([]);
  const [gates, setGates] = useState<Gate[]>([]);
  const [handoffs, setHandoffs] = useState<HandoffOffer[]>([]);
  const [runs, setRuns] = useState<Awaited<ReturnType<typeof listRuns>>>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    setLoading(true);
    Promise.all([
      getTask(id),
      listProposals(),
      listGates(),
      listHandoffs(),
      listRuns(),
    ])
      .then(([t, p, g, h, r]) => {
        setTask(t);
        setProposals(p.filter((x) => x.task_id === id));
        setGates(g.filter((x) => x.task_id === id));
        setHandoffs(h.filter((x) => x.task_id === id));
        setRuns(r.filter((x) => x.task_id === id));
        setError("");
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (id) load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  if (loading) {
    return (
      <p className="p-6 text-[12px] text-[--color-ink-3]">Loading task…</p>
    );
  }
  if (error || !task) {
    return (
      <Panel className="m-6 border-[--color-danger]/40 p-4">
        <p className="text-[12px] font-medium">
          This task could not be loaded
        </p>
        <p className="mt-1 text-[11px] text-[--color-ink-3]">
          {error || "It may not exist, or it may belong to another organisation."}
        </p>
      </Panel>
    );
  }

  // Newest revision first: it is the one anybody would act on.
  const current = [...proposals].sort((a, b) => b.revision - a.revision)[0];
  const openGate = gates.find((g) => g.status !== "resolved");
  const incoming = handoffs.find(
    (h) => h.state === "offered" && h.recipient_id === task.assignee_id,
  );

  return (
    <>
      <PageHeader
        eyebrow="Task"
        title={task.title}
        description={task.description || undefined}
        actions={<Badge tone="neutral">{task.status.replace(/_/g, " ")}</Badge>}
      />

      <div className="grid gap-4 p-6 lg:grid-cols-[minmax(0,1fr)_280px]">
        <div className="space-y-4">
          {actionError ? (
            <p className="text-[11px] text-[--color-danger]">{actionError}</p>
          ) : null}

          {/* The one thing to do next, before any detail. */}
          {openGate ? (
            <Panel>
              <PanelHeader
                title="This task is waiting on an answer"
                subtitle="An agent stopped rather than guess. Answering resumes it."
              />
              <div className="p-4">
                <p className="text-[13px] font-medium">{openGate.prompt}</p>
                <div className="mt-3">
                  <GateForm
                    gate={openGate}
                    busy={busy}
                    onError={setActionError}
                    onSubmit={async (response) => {
                      setBusy(true);
                      setActionError("");
                      try {
                        await respondToGate(openGate.id, {
                          revision: openGate.revision,
                          response,
                        });
                        window.dispatchEvent(new Event("inbox-updated"));
                        load();
                      } catch (e) {
                        setActionError((e as Error).message);
                      } finally {
                        setBusy(false);
                      }
                    }}
                  />
                </div>
              </div>
            </Panel>
          ) : null}

          {current ? (
            <PreparedChange proposal={current} />
          ) : (
            <Panel>
              <PanelHeader title="Nothing prepared yet" />
              <div className="p-4">
                <p className="text-[11px] leading-relaxed text-[--color-ink-3]">
                  No proposal exists for this task. When an agent prepares work,
                  the change it would make appears here as a reviewable object —
                  it is never executed on the strength of an agent&apos;s word.
                </p>
              </div>
            </Panel>
          )}

          <Panel>
            <PanelHeader title="Activity" />
            <div className="divide-y divide-[--color-line]">
              {!runs.length && !handoffs.length ? (
                <p className="p-4 text-[11px] text-[--color-ink-3]">
                  No runs or handoffs recorded for this task.
                </p>
              ) : null}
              {runs.map((run) => (
                <div key={run.id} className="flex items-start gap-3 p-3.5">
                  <CircleDot
                    className={
                      run.status === "failed"
                        ? "mt-0.5 size-3.5 shrink-0 text-[--color-danger]"
                        : "mt-0.5 size-3.5 shrink-0 text-[--color-ink-3]"
                    }
                  />
                  <div className="min-w-0 flex-1">
                    <p className="text-[11px]">
                      <span className="font-medium">Agent run</span>{" "}
                      <span className="text-[--color-ink-3]">
                        {run.status === "waiting"
                          ? "paused, waiting for input"
                          : run.status}
                      </span>
                    </p>
                    <p className="mt-0.5 text-[11px] text-[--color-ink-3]">
                      {run.result_summary ||
                        run.failure_reason ||
                        run.intent ||
                        "No detail recorded"}
                    </p>
                  </div>
                </div>
              ))}
              {handoffs.map((h) => (
                <HandoffCard
                  key={h.id}
                  handoff={h}
                  viewer={me?.id || ""}
                  onChanged={load}
                  onError={setActionError}
                />
              ))}
            </div>
          </Panel>

          <TechnicalPanel
            task={task}
            proposals={proposals}
            runs={runs.length}
            gates={gates.length}
          />
        </div>

        <aside className="space-y-4">
          <Panel>
            <PanelHeader title="Who is involved" />
            <dl className="divide-y divide-[--color-line]">
              {[
                ["Owner", task.owner_id],
                ["Assigned to", task.assignee_id || "unassigned"],
                ["Waiting on", openGate ? "you" : "—"],
              ].map(([label, value]) => (
                <div key={label} className="flex items-center justify-between px-4 py-2.5">
                  <dt className="text-[11px] text-[--color-ink-3]">{label}</dt>
                  <dd className="font-mono text-[11px]">{value}</dd>
                </div>
              ))}
            </dl>
            {incoming ? (
              <div className="border-t border-[--color-line] p-4">
                <Badge tone="info">offer waiting for you</Badge>
              </div>
            ) : null}
          </Panel>

          <Panel>
            <PanelHeader title="Versions" />
            <div className="space-y-1.5 p-4">
              {proposals.length ? (
                proposals
                  .sort((a, b) => b.revision - a.revision)
                  .map((p) => (
                    <div
                      key={p.id}
                      className="flex items-center justify-between text-[11px]"
                    >
                      <span className="font-mono">revision {p.revision}</span>
                      <Badge
                        tone={
                          p.status.includes("pending") ? "pending" : "neutral"
                        }
                      >
                        {p.status.replace(/_/g, " ")}
                      </Badge>
                    </div>
                  ))
              ) : (
                <p className="text-[11px] text-[--color-ink-3]">None yet</p>
              )}
            </div>
          </Panel>
        </aside>
      </div>
    </>
  );
}

/**
 * The prepared change.
 *
 * Rendered read-only here: the decision itself belongs in the inbox, where the person's
 * authority is evaluated against that specific request. Showing approve buttons on every
 * surface invites approving from a place that never checked who may approve.
 */
function PreparedChange({ proposal }: { proposal: Proposal }) {
  return (
    <Panel>
      <PanelHeader
        title={proposal.summary}
        subtitle={`Revision ${proposal.revision} · frozen. Decisions are made from the inbox.`}
        actions={
          <Badge
            tone={proposal.status.includes("pending") ? "pending" : "accent"}
          >
            {proposal.status.replace(/_/g, " ")}
          </Badge>
        }
      />
      <div className="space-y-3 p-4">
        <div className="flex items-start gap-2 rounded-md border border-[--color-line-strong] bg-[--color-raised] p-3">
          <Check className="mt-0.5 size-3.5 shrink-0 text-[--color-accent]" />
          <p className="text-[11px] leading-relaxed text-[--color-ink-2]">
            {proposal.operations.length} operation
            {proposal.operations.length === 1 ? "" : "s"} would be applied on
            approval. Each is version-checked: if the target changes first,
            execution is refused rather than applied against stale data.
          </p>
        </div>
        {proposal.operations.map((op, i) => (
          <div key={op.id ?? i} className="space-y-2">
            <OperationHeading op={op} />
            <OperationPreview op={op} />
          </div>
        ))}
        {proposal.failure_reason ? (
          <div className="flex items-start gap-2 rounded-md border border-[--color-danger]/40 bg-[--color-danger-soft] p-3">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-[--color-danger]" />
            <p className="text-[11px] text-[--color-danger]">
              {proposal.failure_reason}
            </p>
          </div>
        ) : null}
        <p className="font-mono text-[10px] text-[--color-ink-3]">
          digest {proposal.digest.slice(0, 24)}
        </p>
      </div>
    </Panel>
  );
}

function TechnicalPanel({
  task,
  proposals,
  runs,
  gates,
}: {
  task: Task;
  proposals: Proposal[];
  runs: number;
  gates: number;
}) {
  return (
    <details className="rounded-[--radius-panel] border border-[--color-line] bg-[--color-surface]">
      <summary className="cursor-pointer px-4 py-3 text-[11px] text-[--color-ink-3]">
        Technical detail
      </summary>
      <pre className="overflow-auto border-t border-[--color-line] p-4 font-mono text-[10px] leading-relaxed text-[--color-ink-2]">
        {JSON.stringify(
          {
            task: { id: task.id, version: task.version, status: task.status },
            proposals: proposals.map((p) => ({
              id: p.id,
              revision: p.revision,
              status: p.status,
            })),
            runs,
            gates,
          },
          null,
          2,
        )}
      </pre>
    </details>
  );
}

/**
 * One handoff offer, with the replies that are actually available to THIS
 * person.
 *
 * A handoff is not just "accept": a recipient who cannot do the work must be
 * able to decline with a reason, and one who has a question must be able to ask
 * it rather than guess. The creator, separately, withdraws their own offer.
 * Showing only "Accept" forced people to take work they could not do, or to
 * leave it unanswered forever.
 *
 * Which actions appear is derived from the viewer's relationship to the offer
 * (recipient or creator) and the offer's state. The server re-checks all of
 * this; the UI only avoids offering an action that would be refused.
 */
function HandoffCard({
  handoff,
  viewer,
  onChanged,
  onError,
}: {
  handoff: HandoffOffer;
  viewer: string;
  onChanged: () => void;
  onError: (message: string) => void;
}) {
  const [note, setNote] = useState("");
  const [mode, setMode] = useState<"" | "decline" | "clarify" | "cancel">("");
  const [busy, setBusy] = useState(false);

  const open = handoff.state === "offered" || handoff.state === "clarification_requested";
  const isRecipient = viewer !== "" && handoff.recipient_id === viewer;
  const isCreator = viewer !== "" && handoff.created_by === viewer;

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
      setMode("");
      setNote("");
      onChanged();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const tone =
    handoff.state === "accepted"
      ? "accent"
      : handoff.state === "declined"
        ? "danger"
        : handoff.state === "expired" || handoff.state === "cancelled"
          ? "neutral"
          : "pending";

  return (
    <div className="flex items-start gap-3 p-3.5">
      <CircleDot className="mt-0.5 size-3.5 shrink-0 text-[--color-ink-3]" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[11px] font-medium">Handoff</span>
          <Badge tone={tone}>{handoff.state.replace(/_/g, " ")}</Badge>
          <span className="text-[11px] text-[--color-ink-3]">
            {handoff.role === "owner" ? "ownership" : "execution"}
            {handoff.hop_depth > 1 ? ` · transfer ${handoff.hop_depth}` : ""}
          </span>
        </div>
        <p className="mt-0.5 text-[11px] text-[--color-ink-3]">
          {handoff.summary || "No summary given"}
        </p>
        {handoff.reason ? (
          <p className="mt-1 text-[11px] text-[--color-ink-2]">
            <span className="text-[--color-ink-3]">
              {handoff.state === "clarification_requested" ? "Question: " : "Note: "}
            </span>
            {handoff.reason}
          </p>
        ) : null}
        {open && handoff.expires_at ? (
          <p className="mt-1 text-[10px] text-[--color-ink-3]">
            Expires {new Date(handoff.expires_at).toLocaleString()} if not
            answered
          </p>
        ) : null}

        {open && isRecipient ? (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="accent"
              disabled={busy}
              onClick={() => void run(() => acceptHandoff(handoff.id))}
            >
              Accept
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => setMode(mode === "decline" ? "" : "decline")}
            >
              Decline
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => setMode(mode === "clarify" ? "" : "clarify")}
            >
              Ask a question
            </Button>
          </div>
        ) : null}

        {open && isCreator && !isRecipient ? (
          <div className="mt-2">
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => setMode(mode === "cancel" ? "" : "cancel")}
            >
              Withdraw offer
            </Button>
          </div>
        ) : null}

        {open && mode ? (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Input
              autoFocus
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder={
                mode === "clarify"
                  ? "What do you need to know?"
                  : mode === "decline"
                    ? "Why can't you take this?"
                    : "Why are you withdrawing it?"
              }
              className="max-w-sm"
            />
            <Button
              size="sm"
              variant={mode === "cancel" ? "ghost" : "default"}
              disabled={busy || (mode !== "cancel" && !note.trim())}
              onClick={() =>
                void run(() => {
                  if (mode === "decline") return declineHandoff(handoff.id, note.trim());
                  if (mode === "clarify") return clarifyHandoff(handoff.id, note.trim());
                  return cancelHandoff(handoff.id, note.trim());
                })
              }
            >
              {mode === "clarify"
                ? "Ask"
                : mode === "decline"
                  ? "Decline handoff"
                  : "Withdraw"}
            </Button>
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => setMode("")}>
              Cancel
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
