import { useEffect, useState } from "react";
import { Bot, Cpu, ShieldAlert } from "lucide-react";
import { listAgents, listRuns, type Agent } from "@/api";
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
 * The simulator notice is not decoration. Runs currently prepare proposals through an
 * in-process simulator; no model is called and no external command is launched. A screen that
 * showed agent cards without that would imply capability the deployment does not have.
 */
export function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [runCounts, setRunCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    Promise.all([listAgents(), listRuns()])
      .then(([a, runs]) => {
        if (cancelled) return;
        setAgents(a);
        const counts: Record<string, number> = {};
        for (const r of runs) counts[r.agent_id] = (counts[r.agent_id] ?? 0) + 1;
        setRunCounts(counts);
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
            {agents.map((a) => (
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
                    <p className="mt-1 text-[11px] text-[--color-ink-3]">
                      Owner <span className="font-mono">{a.owner_id}</span> ·{" "}
                      {runCounts[a.id] ?? 0} run
                      {(runCounts[a.id] ?? 0) === 1 ? "" : "s"}
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
                  <Badge tone="neutral">definition</Badge>
                </div>
              </Panel>
            ))}
          </div>
        )}
      </div>
    </>
  );
}
