import { useEffect, useMemo, useState } from "react";
import { Users } from "lucide-react";
import {
  listDecisions,
  listPeople,
  listPositions,
  listRelationships,
  listTasks,
  type Decision,
  type Person,
  type Position,
  type Relationship,
  type Task,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Empty, Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";

/** A short, unambiguous date. Structure periods are days, not timestamps. */
function day(iso: string) {
  if (!iso) return "";
  const t = Date.parse(iso);
  return Number.isFinite(t)
    ? new Date(t).toLocaleDateString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
      })
    : iso;
}

/** "since Mar 3, 2026" or "Mar 3 – Apr 9, 2026" — the visible proof it is a period. */
function period(from: string, to: string) {
  if (!from) return "";
  return to ? `${day(from)} – ${day(to)}` : `since ${day(from)}`;
}

/**
 * Team operations.
 *
 * Deliberately aggregate where it concerns people's throughput. This is the
 * surface where a "productivity dashboard" would be the obvious thing to build
 * and the wrong one: individual scoring from task counts or completion rates is
 * gamed within a sprint and punishes people for taking hard work. So the metrics
 * report the shape of the workload — how much is open, how much is stuck, how old
 * the oldest item is — and not who is winning.
 *
 * The organization STRUCTURE is shown, and that is not the same thing. Who
 * reports to whom, who is in which team and who is covering for whom are facts
 * about the org, not judgements about a person, and they are what makes a
 * handoff or an escalation comprehensible. Structure grants nothing: reporting to
 * someone does not let you approve their work, and the API refuses to treat it
 * that way.
 */
export function TeamPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [people, setPeople] = useState<Person[]>([]);
  const [positions, setPositions] = useState<Position[]>([]);
  const [relationships, setRelationships] = useState<Relationship[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      listTasks(),
      listDecisions(),
      listPeople(),
      listPositions(),
      listRelationships(),
    ])
      .then(([t, d, ppl, pos, rel]) => {
        if (cancelled) return;
        setTasks(t);
        setDecisions(d);
        setPeople(ppl);
        setPositions(pos);
        setRelationships(rel);
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
    for (const p of positions) m.set(p.id, p.name);
    return (id: string) => m.get(id) || id;
  }, [people, positions]);

  const reporting = relationships.filter((r) => r.kind === "reports_to");
  const memberships = relationships.filter((r) => r.kind === "member_of");
  const covers = relationships.filter((r) => r.kind === "covers_for");
  // A position nobody occupies is a vacancy — a state worth showing, not an
  // absence to hide.
  const filled = new Set(memberships.map((m) => m.object_id));
  const vacancies = positions.filter((p) => !filled.has(p.id));

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
        description="Structure, flow and aging across the organisation. Throughput is reported in aggregate — this surface does not rank individuals."
      />

      <div className="space-y-4 p-6">
        {error ? (
          <Panel className="border-[--color-danger]/40 p-4">
            <p className="text-[12px] font-medium">Could not load team metrics</p>
            <p className="mt-1 text-[11px] text-[--color-ink-3]">{error}</p>
          </Panel>
        ) : loading ? (
          <p className="text-[12px] text-[--color-ink-3]">Loading…</p>
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
                    Flow and aging are reported, not individual throughput. Task
                    counts, token spend and completion rates make poor measures of
                    a person: they reward closing work quickly and penalise
                    picking up anything difficult.
                  </p>
                </div>
              </div>
            </Panel>

            <Panel className="p-4">
              <p className="eyebrow">Organisation structure</p>
              <p className="mt-1 text-[11px] leading-relaxed text-[--color-ink-3]">
                Reporting, team membership and cover are effective-dated periods.
                Reporting to someone is not authority over them — who approves
                what comes from a person&apos;s role, never from the shape of this
                graph.
              </p>

              {!reporting.length && !memberships.length && !covers.length ? (
                <p className="mt-3 text-[11px] text-[--color-ink-3]">
                  No reporting lines, team memberships or cover recorded yet.
                </p>
              ) : (
                <div className="mt-3 space-y-4">
                  {reporting.length ? (
                    <div>
                      <p className="text-[11px] font-medium">Reporting</p>
                      <ul className="mt-1.5 space-y-1">
                        {reporting.map((r) => (
                          <li
                            key={r.id}
                            className="flex flex-wrap items-baseline gap-x-2 text-[11px]"
                          >
                            <span>{nameOf(r.subject_id)}</span>
                            <span className="text-[--color-ink-3]">reports to</span>
                            <span className="font-medium">{nameOf(r.object_id)}</span>
                            <span className="text-[10px] text-[--color-ink-3]">
                              {period(r.valid_from, r.valid_to)}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}

                  {memberships.length ? (
                    <div>
                      <p className="text-[11px] font-medium">Team membership</p>
                      <ul className="mt-1.5 space-y-1">
                        {memberships.map((r) => (
                          <li
                            key={r.id}
                            className="flex flex-wrap items-baseline gap-x-2 text-[11px]"
                          >
                            <span>{nameOf(r.subject_id)}</span>
                            <span className="text-[--color-ink-3]">in</span>
                            <span className="font-medium">{nameOf(r.object_id)}</span>
                            <span className="text-[10px] text-[--color-ink-3]">
                              {period(r.valid_from, r.valid_to)}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}

                  {covers.length ? (
                    <div>
                      <p className="text-[11px] font-medium">Cover</p>
                      <ul className="mt-1.5 space-y-1">
                        {covers.map((r) => (
                          <li
                            key={r.id}
                            className="flex flex-wrap items-baseline gap-x-2 text-[11px]"
                          >
                            <span>{nameOf(r.subject_id)}</span>
                            <span className="text-[--color-ink-3]">covers for</span>
                            <span className="font-medium">{nameOf(r.object_id)}</span>
                            <span className="text-[10px] text-[--color-ink-3]">
                              {period(r.valid_from, r.valid_to)}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}

                  {vacancies.length ? (
                    <div>
                      <p className="text-[11px] font-medium">Unstaffed positions</p>
                      <ul className="mt-1.5 flex flex-wrap gap-2">
                        {vacancies.map((p) => (
                          <li key={p.id}>
                            <Badge tone="pending">{p.name} · vacant</Badge>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}

                  {positions.length ? (
                    <div>
                      <p className="text-[11px] font-medium">Positions</p>
                      <ul className="mt-1.5 space-y-1">
                        {positions.map((p) => (
                          <li key={p.id} className="text-[11px]">
                            {p.parent_id ? (
                              <>
                                <span className="text-[--color-ink-3]">
                                  {nameOf(p.parent_id)} /{" "}
                                </span>
                                <span>{p.name}</span>
                              </>
                            ) : (
                              <span>{p.name}</span>
                            )}
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}
                </div>
              )}

              <p className="mt-3 text-[10px] text-[--color-ink-3]">
                Structure is stored as periods, so a past arrangement stays
                answerable rather than being overwritten by the current one.
              </p>
            </Panel>

            <Panel className="p-4">
              <p className="text-[12px] font-medium">What is not here yet</p>
              <ul className="mt-2 space-y-1.5 text-[11px] leading-relaxed text-[--color-ink-3]">
                <li>
                  · Editing the structure from this page. The model and its API
                  exist; there is no form here yet.
                </li>
                <li>
                  · Viewing the structure as it stood on a past date, which the
                  API already supports (`as_of`) but this page does not expose.
                </li>
                <li>
                  · Waiting-time breakdown per stage, which needs event history
                  rather than current status.
                </li>
                <li>
                  · Rollups scoped to a manager&apos;s subtree.
                </li>
              </ul>
            </Panel>
          </>
        )}
      </div>
    </>
  );
}
