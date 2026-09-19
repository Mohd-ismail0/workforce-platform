import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  AlertTriangle,
  ArrowRight,
  Check,
  Clock,
  HelpCircle,
  Inbox,
  ThumbsUp,
} from "lucide-react";
import {
  decideProposal,
  getProposal,
  listDecisions,
  listIntegrations,
  listGates,
  listRecords,
  respondToGate,
  type ConnectorManifest,
  type Decision,
  type Gate,
  type Proposal,
  type Record_,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";
import { OperationHeading, OperationPreview } from "@/features/task/previews";
import { GateForm } from "@/features/inbox/gate-form";

/**
 * The inbox: everything waiting on this person, as prepared outcomes rather than activity.
 *
 * The organising idea (from the Agency review) is that a card shows the WORK THAT IS ALREADY
 * DONE and the single remaining decision. It deliberately does not say "should I investigate
 * the discrepancy?" — by the time something reaches a person here, the preparation has
 * happened and their judgement is the only thing missing.
 *
 * Three kinds of item share this surface because they all mean "you are the blocker":
 *   - a question an agent asked (it parked rather than guessing)
 *   - a proposal needing ENDORSEMENT (this is the work I asked for)
 *   - a proposal needing APPROVAL (authorise this exact effect)
 *
 * Endorsement and approval are shown as visibly different, because conflating them is how an
 * approval chain quietly becomes one person's signature.
 */

const KIND_COPY: Record<
  Decision["kind"],
  { label: string; verb: string; blurb: string; tone: "info" | "pending" }
> = {
  endorse: {
    label: "Needs endorsement",
    verb: "Endorse",
    blurb:
      "Confirms this is the work you asked for. It does not authorise the change — a different person approves it.",
    tone: "info",
  },
  approve: {
    label: "Needs approval",
    verb: "Approve",
    blurb:
      "Authorises exactly the operations shown. The executor applies these and nothing else.",
    tone: "pending",
  },
};

export function InboxPage() {
  const [gates, setGates] = useState<Gate[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [manifests, setManifests] = useState<ConnectorManifest[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [selected, setSelected] = useState<string>();

  const load = useCallback(() => {
    setLoading(true);
    return Promise.all([
      listGates(),
      listDecisions(),
      listIntegrations().catch(() => [] as ConnectorManifest[]),
    ])
      .then(([g, d, mf]) => {
        setGates(g);
        setDecisions(d);
        setManifests(mf);
        setLoadError("");
      })
      .catch((e: Error) => setLoadError(e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const announce = () => window.dispatchEvent(new Event("inbox-updated"));
  const total = gates.length + decisions.length;
  // Gates and endorsement requests are the person's own work; approvals are a decision on
  // someone else's. Ordering by kind keeps "do my own work" above "sign for others".
  const ordered = [...decisions].sort((a, b) =>
    a.kind === b.kind ? 0 : a.kind === "endorse" ? -1 : 1,
  );

  return (
    <>
      <PageHeader
        eyebrow="Inbox"
        title={total ? `${total} waiting on you` : "Nothing is waiting on you"}
        description="Prepared work and questions that cannot proceed without your judgement. Everything here is already done except the decision."
        actions={
          <Button variant="ghost" size="sm" onClick={() => void load()}>
            Refresh
          </Button>
        }
      />

      <div className="p-6">
        {loadError ? (
          <Panel className="border-[--color-danger]/40">
            <div className="flex items-start gap-3 p-4">
              <AlertTriangle className="mt-0.5 size-4 shrink-0 text-[--color-danger]" />
              <div>
                <p className="text-[12px] font-medium">
                  The inbox could not be loaded
                </p>
                <p className="mt-1 text-[11px] text-[--color-ink-3]">{loadError}</p>
                <p className="mt-1 text-[11px] text-[--color-ink-3]">
                  This is not the same as &ldquo;nothing is waiting&rdquo;. Do not treat this
                  screen as an empty inbox.
                </p>
              </div>
            </div>
          </Panel>
        ) : loading ? (
          <p className="text-[12px] text-[--color-ink-3]">Loading…</p>
        ) : !total ? (
          <Empty
            title="Nothing is waiting on you"
            reason="Agents are working, or everything has been decided. New questions and prepared work appear here."
          />
        ) : (
          <div className="space-y-3">
            {gates.map((gate) => (
              <GateCard
                key={gate.id}
                gate={gate}
                onAnswered={() => {
                  announce();
                  void load();
                }}
              />
            ))}
            {ordered.map((d) => (
              <DecisionCard
                key={d.id}
                manifests={manifests}
                decision={d}
                expanded={selected === d.id}
                onToggle={() => setSelected(selected === d.id ? undefined : d.id)}
                onDecided={() => {
                  announce();
                  void load();
                }}
              />
            ))}
          </div>
        )}
      </div>
    </>
  );
}

function GateCard({ gate, onAnswered }: { gate: Gate; onAnswered: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  return (
    <Panel>
      <div className="flex items-start gap-3 p-4">
        <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-md bg-[--color-info-soft] text-[--color-info]">
          <HelpCircle className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Badge tone="info">
              {gate.kind === "missing_information"
                ? "Missing information"
                : gate.kind}
            </Badge>
            <span className="text-[10px] text-[--color-ink-3]">
              an agent stopped and asked instead of guessing
            </span>
          </div>
          <p className="mt-2 text-[13px] font-medium">{gate.prompt}</p>
          <p className="mt-1 text-[11px] text-[--color-ink-3]">
            Answering resumes the paused run. No worker is held while it waits, and its output
            still becomes a proposal that needs endorsement and approval.
          </p>

          <div className="mt-3">
            <GateForm
              gate={gate}
              busy={busy}
              onError={setError}
              onSubmit={async (response) => {
                setBusy(true);
                setError("");
                try {
                  await respondToGate(gate.id, {
                    revision: gate.revision,
                    response,
                  });
                  onAnswered();
                } catch (e) {
                  const err = e as Error & { status?: number };
                  setError(
                    err.status === 403
                      ? "You are not the person this question was routed to."
                      : err.status === 409
                        ? "This question changed while you were answering. Reloading."
                        : err.message,
                  );
                  if (err.status === 409) onAnswered();
                } finally {
                  setBusy(false);
                }
              }}
            />
          </div>
          {error ? (
            <p className="mt-2 text-[11px] text-[--color-danger]">{error}</p>
          ) : null}
        </div>
      </div>
    </Panel>
  );
}

function DecisionCard({
  decision,
  manifests,
  expanded,
  onToggle,
  onDecided,
}: {
  decision: Decision;
  /** Connector manifests, so each operation can show its evidenced level. */
  manifests: ConnectorManifest[];
  expanded: boolean;
  onToggle: () => void;
  onDecided: () => void;
}) {
  const [proposal, setProposal] = useState<Proposal>();
  const [records, setRecords] = useState<Record<string, Record_>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Separate from `error`: a failure to LOAD the prepared change must disable approval rather
  // than sit silently beside a still-enabled button.
  const [loadError, setLoadError] = useState("");
  const [reason, setReason] = useState("");
  const [showReject, setShowReject] = useState(false);

  // Loading is split into two effects on purpose.
  //
  // An earlier revision did both in one effect whose dependency list included `proposal`, which
  // the effect itself set. That state change re-ran the effect, whose cleanup set `cancelled`,
  // so the record fetch that was still in flight never published — meaning the previews could
  // never resolve a target and always reported "current value not available". The flagship
  // feature was silently dead. Keying each effect on inputs it does not itself mutate fixes it.
  useEffect(() => {
    if (!expanded) return;
    let cancelled = false;
    setLoadError("");
    getProposal(decision.proposal_id)
      .then((p) => {
        if (!cancelled) setProposal(p);
      })
      .catch((e: Error) => {
        if (!cancelled) setLoadError(e.message);
      });
    return () => {
      cancelled = true;
    };
  }, [expanded, decision.proposal_id]);

  useEffect(() => {
    if (!proposal) return;
    let cancelled = false;
    const integrations = [...new Set(proposal.operations.map((o) => o.integration))];
    void (async () => {
      const found: Record<string, Record_> = {};
      await Promise.all(
        integrations.map(async (integration) => {
          try {
            const list = await listRecords(integration);
            for (const r of list) found[`${integration}:${r.id}`] = r;
          } catch {
            /* reported per operation as unknown rather than blanking the card */
          }
        }),
      );
      if (!cancelled) setRecords(found);
    })();
    return () => {
      cancelled = true;
    };
  }, [proposal]);

  /**
   * Integrity check between what the inbox listed and what the detail actually holds.
   *
   * The decision carries the revision and digest it was issued against; the proposal is fetched
   * separately. If those disagree, the person is looking at one revision and submitting proof
   * for another — which would let a stale or substituted revision be authorised. This must be
   * compared, not assumed.
   */
  const mismatch =
    proposal &&
    (proposal.revision !== decision.revision ||
      proposal.digest !== decision.digest);

  const copy = KIND_COPY[decision.kind];

  const submit = async (kind: "endorse" | "approve" | "reject") => {
    setBusy(true);
    setError("");
    try {
      await decideProposal(decision.proposal_id, kind, {
        revision: decision.revision,
        digest: decision.digest,
        ...(kind === "reject" ? { reason } : {}),
      });
      onDecided();
    } catch (e) {
      const err = e as Error & { status?: number };
      setError(
        err.status === 409
          ? "This revision changed since it was shown to you. Reloading so you approve what is actually there."
          : err.message,
      );
      if (err.status === 409) onDecided();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Panel>
      <div className="flex items-start gap-3 p-4">
        <span
          className={
            copy.tone === "pending"
              ? "mt-0.5 grid size-7 shrink-0 place-items-center rounded-md bg-[--color-pending-soft] text-[--color-pending]"
              : "mt-0.5 grid size-7 shrink-0 place-items-center rounded-md bg-[--color-info-soft] text-[--color-info]"
          }
        >
          {decision.kind === "approve" ? (
            <Check className="size-4" />
          ) : (
            <ThumbsUp className="size-4" />
          )}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={copy.tone}>{copy.label}</Badge>
            <span className="font-mono text-[10px] text-[--color-ink-3]">
              revision {decision.revision}
            </span>
          </div>
          <p className="mt-2 text-[13px] font-medium">{decision.title}</p>
          <p className="mt-1 text-[11px] text-[--color-ink-3]">{copy.blurb}</p>

          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button size="sm" variant="ghost" onClick={onToggle}>
              {expanded ? "Hide details" : "Review the prepared change"}
              <ArrowRight className="size-3.5" />
            </Button>
            <Link
              to={`/tasks/${decision.task_id}`}
              className="text-[11px] text-[--color-accent] underline-offset-4 hover:underline"
            >
              Open task
            </Link>
          </div>

          {expanded ? (
            <div className="mt-4 space-y-3">
              {error ? (
                <p className="text-[11px] text-[--color-danger]">{error}</p>
              ) : loadError ? (
                <p className="text-[11px] text-[--color-danger]">
                  The prepared change could not be loaded ({loadError}). Nothing
                  can be approved from here until it loads, because approving
                  means authorising the operations you were shown.
                </p>
              ) : !proposal ? (
                <p className="text-[11px] text-[--color-ink-3]">
                  Loading the prepared change…
                </p>
              ) : mismatch ? (
                // Refuse rather than reconcile: presenting revision A while the decision refers
                // to revision B would let a person authorise something they never saw.
                <div className="rounded-md border border-[--color-pending]/40 bg-[--color-pending-soft] p-3">
                  <p className="text-[11px] font-medium text-[--color-pending]">
                    This decision and the proposal it points at disagree
                  </p>
                  <p className="mt-1 text-[11px] text-[--color-pending]">
                    The decision refers to revision {decision.revision} (
                    {decision.digest.slice(0, 12)}), but the stored proposal is
                    revision {proposal.revision} (
                    {proposal.digest.slice(0, 12)}). Approving is disabled —
                    reload so you are deciding on what is actually there.
                  </p>
                  <Button
                    variant="default"
                    size="sm"
                    className="mt-2"
                    onClick={onDecided}
                  >
                    Reload
                  </Button>
                </div>
              ) : (
                <>
                  <AuthoritativeSummary proposal={proposal} />
                  {proposal.operations.map((op, i) => (
                    <div key={op.id ?? i} className="space-y-2">
                      <OperationHeading
                        op={op}
                        maturity={manifests.find((m) => m.id === op.integration)?.maturity?.find(
                          (x) => x.action === op.action,
                        )}
                      />
                      <OperationPreview
                        op={op}
                        target={records[`${op.integration}:${op.target_id}`]}
                      />
                    </div>
                  ))}

                  <div className="flex flex-wrap items-center gap-2 border-t border-[--color-line] pt-3">
                    {decision.kind === "endorse" ? (
                      <Button
                        variant="accent"
                        size="sm"
                        disabled={busy}
                        onClick={() => void submit("endorse")}
                      >
                        <ThumbsUp className="size-3.5" />
                        Endorse for approval
                      </Button>
                    ) : (
                      <Button
                        variant="accent"
                        size="sm"
                        disabled={busy}
                        onClick={() => void submit("approve")}
                      >
                        <Check className="size-3.5" />
                        Approve these operations
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={busy}
                      onClick={() => setShowReject((v) => !v)}
                    >
                      {/* Label says reject because that is what it does. "Request changes"
                          implies a revision workflow that does not exist yet, and a person
                          would reasonably expect an editable draft to come back. */}
                      Reject with a note
                    </Button>
                  </div>

                  {showReject ? (
                    <div className="space-y-2 rounded-md border border-[--color-line] p-3">
                      <label className="grid gap-1.5">
                        <span className="text-[11px] font-medium text-[--color-ink-2]">
                          What needs to change?
                        </span>
                        <input
                          className="w-full rounded-md border border-[--color-line-strong] bg-[--color-canvas] px-2.5 py-1.5 text-xs"
                          value={reason}
                          onChange={(e) => setReason(e.target.value)}
                          placeholder="A short note for whoever prepared this"
                        />
                      </label>
                      <Button
                        variant="danger"
                        size="sm"
                        disabled={busy || !reason.trim()}
                        onClick={() => void submit("reject")}
                      >
                        Send back with this note
                      </Button>
                    </div>
                  ) : null}
                </>
              )}
            </div>
          ) : null}
        </div>
      </div>
    </Panel>
  );
}

/**
 * The authoritative summary.
 *
 * Always rendered above the previews, in a fixed form the platform controls. A per-integration
 * renderer is allowed to be expressive; this is what the platform asserts will happen, so a
 * renderer that omitted or softened something could not mislead an approver into authorising
 * more than they saw.
 */
function AuthoritativeSummary({ proposal }: { proposal: Proposal }) {
  const byIntegration = proposal.operations.reduce<Record<string, number>>(
    (acc, op) => {
      acc[op.integration] = (acc[op.integration] ?? 0) + 1;
      return acc;
    },
    {},
  );
  return (
    <div className="rounded-md border border-[--color-line-strong] bg-[--color-raised] p-3">
      <div className="flex items-center gap-2">
        <Clock className="size-3.5 text-[--color-ink-3]" />
        <span className="eyebrow">What executes if approved</span>
      </div>
      <p className="mt-2 text-[12px]">{proposal.summary}</p>
      <p className="mt-1.5 text-[11px] text-[--color-ink-2]">
        {proposal.operations.length} operation
        {proposal.operations.length === 1 ? "" : "s"} against{" "}
        {Object.entries(byIntegration)
          .map(([k, n]) => `${n} ${k}`)
          .join(", ")}
        . Version-checked: if a target changes first, execution is refused rather than applied
        stale.
      </p>
      <p className="mt-1 font-mono text-[10px] text-[--color-ink-3]">
        revision {proposal.revision} · {proposal.digest.slice(0, 16)}
      </p>
    </div>
  );
}
