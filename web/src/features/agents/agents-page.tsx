import { useEffect, useState } from "react";
import { Bot, Cpu, PauseCircle, ShieldAlert, Zap } from "lucide-react";
import { getAgentBoard, listAgents, type Agent, type AgentBoard } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Empty, Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";

/**
 * Agents.
 *
 * An agent here is a DEFINITION — a template (harness, capabilities, owner) that runs are
 * created from. It is not a long-lived process, and the copy says so, because the alternative
 * reading ("my agent is a thing that is always awake") would be wrong in a way that matters:
 * work is dispatched per task and the compute is released when a task is not runnable.
 *
 * Each card shows what the agent is ACTUALLY doing, and the distinction that matters is
 * between reasoning and waiting. A parked run is waiting on a person: it is not thinking, it
 * holds no worker, and showing it as activity would report an idle agent as busy. So "waiting
 * on you" is rendered as its own state, deliberately quieter than working.
 *
 * The simulator notice is not decoration. The built-in runner prepares proposals
 * deterministically and calls no model; a screen that showed agent cards without saying so
 * would imply capability the deployment may not have.
 */
export function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [boards, setBoards] = useState<Record<string, AgentBoard>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    listAgents()
      .then(async (a) => {
        if (cancelled) return;
        setAgents(a);
        // One request per agent. Fine at the scale of a team's agent roster;
        // if that ever stops being true this becomes a single summary endpoint.
        const entries = await Promise.all(
          a.map(async (ag) => {
            try {
              return [ag.id, await getAgentBoard(ag.id)] as const;
            } catch {
              return null;
            }
          }),
        );
        if (cancelled) return;
        const m: Record<string, AgentBoard> = {};
        for (const e of entries) if (e) m[e[0]] = e[1];
        setBoards(m);
      })
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <>
      <PageHeader
        eyebrow="Agents"
        title="Agent definitions"
        description="Templates work can be delegated to. A definition is not a running process: each task creates a run, and compute is released while a task waits."
      />

      <div className="space-y-4 p-6">
        <div className="flex items-start gap-2.5 rounded-[--radius-panel] border border-[--color-pending]/30 bg-[--color-pending-soft] px-4 py-3">
          <ShieldAlert className="mt-0.5 size-3.5 shrink-0 text-[--color-pending]" />
          <p className="text-[11px] leading-relaxed text-[--color-pending]">
            <strong className="font-semibold">Runner is not verified here.</strong>{" "}
            Business effects are simulated: a run prepares a proposal and no
            email, stock or document is really changed. Whether model calls leave
            this machine depends on whether an operator has configured a
            real harness — the built-in runner is deterministic and calls no
            model, but a configured one routes through the gateway.
          </p>
        </div>

        {error ? (
          <Panel className="border-[--color-danger]/40 p-4">
            <p className="text-[12px] font-medium">Could not load agents</p>
            <p className="mt-1 text-[11px] text-[--color-ink-3]">{error}</p>
          </Panel>
        ) : loading ? (
          <p className="text-[12px] text-[--color-ink-3]">Loading…</p>
        ) : !agents.length ? (
          <Empty
            title="No agent definitions yet"
            reason="An agent definition names a harness and the capabilities work may use. Once one exists, a task can be delegated to it and its runs appear on the task."
          />
        ) : (
          <div className="grid gap-3 md:grid-cols-2">
            {agents.map((a) => {
              const b = boards[a.id];
              const parked = b?.parked.length ?? 0;
              const running = b?.running.length ?? 0;
              const queued = b?.queued.length ?? 0;
              return (
                <Panel key={a.id} className="p-4">
                  <div className="flex items-start gap-3">
                    <span className="grid size-7 shrink-0 place-items-center rounded-md bg-[--color-accent-soft] text-[--color-accent]">
                      <Bot className="size-3.5" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="text-[12px] font-medium">{a.name}</p>
                      <p className="mt-1 flex items-center gap-1.5 text-[11px] text-[--color-ink-3]">
                        <Cpu className="size-3" />
                        <span className="font-mono">{a.harness}</span>
                      </p>

                      {/* The state that matters, stated plainly. */}
                      <p className="mt-2 flex items-center gap-1.5 text-[11px]">
                        {running > 0 ? (
                          <>
                            <Zap className="size-3 text-[--color-accent]" />
                            <span className="font-medium">Working now</span>
                            <span className="text-[--color-ink-3]">
                              · {running} attempt{running === 1 ? "" : "s"}
                            </span>
                          </>
                        ) : parked > 0 ? (
                          <>
                            <PauseCircle className="size-3 text-[--color-pending]" />
                            <span className="font-medium text-[--color-pending]">
                              Waiting on a person
                            </span>
                            <span className="text-[--color-ink-3]">
                              · not reasoning
                            </span>
                          </>
                        ) : queued > 0 ? (
                          <>
                            <Zap className="size-3 text-[--color-ink-3]" />
                            <span className="text-[--color-ink-3]">
                              Admitted, not started · {queued}
                            </span>
                          </>
                        ) : (
                          <>
                            <PauseCircle className="size-3 text-[--color-ink-3]" />
                            <span className="text-[--color-ink-3]">Idle</span>
                          </>
                        )}
                      </p>

                      {parked > 0 ? (
                        <p className="mt-1 text-[10px] text-[--color-ink-3]">
                          {parked} attempt{parked === 1 ? "" : "s"} released their
                          worker and are waiting for an answer. A parked agent is
                          not consuming compute.
                        </p>
                      ) : null}

                      <p className="mt-1 text-[11px] text-[--color-ink-3]">
                        Owner <span className="font-mono">{a.owner_id}</span>
                        {b?.tasks.length ? ` · ${b.tasks.length} open task${b.tasks.length === 1 ? "" : "s"}` : ""}
                      </p>

                      {a.capabilities?.length ? (
                        <div className="mt-2 flex flex-wrap gap-1">
                          {a.capabilities.map((c) => (
                            <span
                              key={c}
                              className="rounded border border-[--color-line-strong] px-1.5 py-0.5 text-[10px] text-[--color-ink-2]"
                            >
                              {c}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <p className="mt-2 text-[10px] text-[--color-ink-3]">
                          No capabilities declared — it can prepare, and nothing
                          more.
                        </p>
                      )}
                    </div>
                    {running > 0 ? (
                      <Badge tone="accent">working</Badge>
                    ) : parked > 0 ? (
                      <Badge tone="pending">waiting</Badge>
                    ) : (
                      <Badge tone="neutral">definition</Badge>
                    )}
                  </div>
                  <p className="mt-2 border-t border-[--color-line] pt-2 text-[10px] text-[--color-ink-3]">
                    An agent can prepare work, never approve it. Approval requires
                    a person; an agent identity is not a human approver.
                  </p>
                </Panel>
              );
            })}
          </div>
        )}
      </div>
    </>
  );
}
