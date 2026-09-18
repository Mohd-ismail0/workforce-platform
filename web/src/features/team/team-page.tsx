import { useEffect, useMemo, useState } from "react";
import { Users } from "lucide-react";
import { listDecisions, listTasks, type Decision, type Task } from "@/api";
import { Empty, Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";

/**
 * Team operations.
 *
 * Deliberately aggregate. This is the surface where a "productivity dashboard" would be the
 * obvious thing to build and the wrong one: individual scoring from task counts or completion
 * rates is gamed within a sprint and punishes people for taking hard work. So this reports the
 * shape of the workload — how much is open, how much is stuck, how old is the oldest item — and
 * not who is winning.
 *
 * Per-person attribution is intentionally absent rather than merely unbuilt.
 */
export function TeamPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    Promise.all([listTasks(), listDecisions()])
      .then(([t, d]) => {
        if (cancelled) return;
        setTasks(t);
        setDecisions(d);
      })
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  const stats = useMemo(() => {
    const open = tasks.filter((t) => !["done", "cancelled"].includes(t.status));
    const blocked = open.filter((t) => t.status === "blocked");
    const awaiting = open.filter((t) => t.status === "in_review");
    const ages = open
      .map((t) => Date.parse(t.created_at))
      .filter((n) => Number.isFinite(n));
    const oldestDays = ages.length
      ? Math.floor((Date.now() - Math.min(...ages)) / 86_400_000)
      : 0;
    return {
      open: open.length,
      blocked: blocked.length,
      awaiting: awaiting.length,
      decisions: decisions.length,
      oldestDays,
    };
  }, [tasks, decisions]);

  return (
    <>
      <PageHeader
        eyebrow="Team"
        title="Team operations"
        description="How work is flowing across the organisation. Aggregate by design — this surface does not rank individuals."
      />

      <div className="space-y-4 p-6">
        {error ? (
          <Panel className="border-[--color-danger]/40 p-4">
            <p className="text-[12px] font-medium">Could not load team metrics</p>
            <p className="mt-1 text-[11px] text-[--color-ink-3]">{error}</p>
          </Panel>
        ) : loading ? (
          <p className="text-[12px] text-[--color-ink-3]">Loading…</p>
        ) : !tasks.length ? (
          <Empty
            title="No work recorded yet"
            reason="These figures come from real tasks. When work exists, open items, blocked work and waiting decisions appear here."
          />
        ) : (
          <>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              {[
                { label: "Open work", value: stats.open, tone: "" },
                { label: "Blocked", value: stats.blocked, tone: "danger" },
                { label: "Awaiting review", value: stats.awaiting, tone: "info" },
                { label: "Decisions pending", value: stats.decisions, tone: "pending" },
              ].map((s) => (
                <Panel key={s.label} className="p-4">
                  <p className="eyebrow">{s.label}</p>
                  <p
                    className={
                      s.tone === "danger"
                        ? "tabular mt-2 text-xl font-semibold text-[--color-danger]"
                        : s.tone === "pending"
                          ? "tabular mt-2 text-xl font-semibold text-[--color-pending]"
                          : s.tone === "info"
                            ? "tabular mt-2 text-xl font-semibold text-[--color-info]"
                            : "tabular mt-2 text-xl font-semibold"
                    }
                  >
                    {s.value}
                  </p>
                </Panel>
              ))}
            </div>

            <Panel className="p-4">
              <div className="flex items-start gap-3">
                <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-md bg-[--color-raised] text-[--color-ink-3]">
                  <Users className="size-3.5" />
                </span>
                <div>
                  <p className="text-[12px] font-medium">
                    Oldest open item: {stats.oldestDays} day
                    {stats.oldestDays === 1 ? "" : "s"}
                  </p>
                  <p className="mt-1 text-[11px] leading-relaxed text-[--color-ink-3]">
                    Flow and aging are reported, not individual throughput.
                    Task counts, token spend and completion rates make poor
                    measures of a person: they reward closing work quickly and
                    penalise picking up anything difficult.
                  </p>
                </div>
              </div>
            </Panel>

            <Panel className="p-4">
              <p className="text-[12px] font-medium">What is not here yet</p>
              <ul className="mt-2 space-y-1.5 text-[11px] leading-relaxed text-[--color-ink-3]">
                <li>
                  · Team and reporting structure — the org graph is not modelled
                  beyond a single organisation id.
                </li>
                <li>
                  · Waiting-time breakdown per stage, which needs event history
                  rather than current status.
                </li>
                <li>
                  · Rollups scoped to a manager&apos;s subtree, which needs the
                  org graph above.
                </li>
              </ul>
            </Panel>
          </>
        )}
      </div>
    </>
  );
}
