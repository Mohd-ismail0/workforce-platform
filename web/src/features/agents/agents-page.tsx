import { useEffect, useState } from "react";
import { Bot, Cpu, PauseCircle, ShieldAlert, Zap } from "lucide-react";
import {
  createAgentFromTemplate,
  getAgentBoard,
  getAgentConfiguration,
  listAgents,
  listAgentTemplates,
  publishAgentTemplate,
  type Agent,
  type AgentBoard,
  type AgentConfiguration,
  type AgentTemplate,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
  const [configs, setConfigs] = useState<Record<string, AgentConfiguration>>({});
  const [templates, setTemplates] = useState<AgentTemplate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = () => {
    let cancelled = false;
    Promise.all([listAgents(), listAgentTemplates().catch(() => [] as AgentTemplate[])])
      .then(async ([a, tpls]) => {
        if (cancelled) return;
        setAgents(a);
        setTemplates(tpls);
        // One request per agent. Fine at the scale of a team's agent roster;
        // if that ever stops being true this becomes a single summary endpoint.
        const entries = await Promise.all(
          a.map(async (ag) => {
            try {
              const [board, config] = await Promise.all([
                getAgentBoard(ag.id),
                getAgentConfiguration(ag.id),
              ]);
              return [ag.id, board, config] as const;
            } catch {
              return null;
            }
          }),
        );
        if (cancelled) return;
        const m: Record<string, AgentBoard> = {};
        const c: Record<string, AgentConfiguration> = {};
        for (const e of entries) {
          if (e) {
            m[e[0]] = e[1];
            c[e[0]] = e[2];
          }
        }
        setBoards(m);
        setConfigs(c);
      })
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  };

  useEffect(load, []);

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

        <TemplatePicker
          templates={templates}
          onChanged={() => void load()}
          onError={setError}
        />

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
                      {a.template_version ? (
                        <p className="mt-0.5 text-[10px] text-[--color-ink-3]">
                          From a template, pinned to version{" "}
                          <span className="font-mono">{a.template_version}</span>{" "}
                          — a newer template version does not change this agent.
                        </p>
                      ) : null}

                      {(() => {
                        const cfg = configs[a.id];
                        const declared = cfg?.declared ?? a.capabilities ?? [];
                        const denied = new Set(cfg?.denied ?? []);
                        if (!declared.length) {
                          return (
                            <p className="mt-2 text-[10px] text-[--color-ink-3]">
                              No capabilities declared — it can prepare, and
                              nothing more.
                            </p>
                          );
                        }
                        return (
                          <div className="mt-2">
                            <div className="flex flex-wrap gap-1">
                              {declared.map((c) => (
                                <span
                                  key={c}
                                  className={
                                    denied.has(c)
                                      ? "rounded border border-[--color-danger]/50 px-1.5 py-0.5 text-[10px] text-[--color-danger]"
                                      : "rounded border border-[--color-line-strong] px-1.5 py-0.5 text-[10px] text-[--color-ink-2]"
                                  }
                                >
                                  {c}
                                </span>
                              ))}
                            </div>
                            {denied.size ? (
                              <p className="mt-1 text-[10px] text-[--color-danger]">
                                Not granted by the {cfg?.harness ?? a.harness}{" "}
                                release — a run would be refused at admission.
                              </p>
                            ) : (
                              <p className="mt-1 text-[10px] text-[--color-ink-3]">
                                Within what the harness release permits
                                {cfg?.available.length
                                  ? ` · ${cfg.available.length} further capability${
                                      cfg.available.length === 1 ? "" : "ies"
                                    } not used`
                                  : ""}
                                .
                              </p>
                            )}
                          </div>
                        );
                      })()}
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

/**
 * Start an agent from a template.
 *
 * A template is the difference between "configure an agent" and "pick one and
 * turn parts of it off". Only PUBLISHED templates can be adopted: publishing is
 * the act that makes a template safe to hand to someone, so a half-written one
 * is never offered. Customization narrows the template's scope; it can never
 * widen it, and the server refuses a widened request rather than trimming it.
 */
function TemplatePicker({
  templates,
  onChanged,
  onError,
}: {
  templates: AgentTemplate[];
  onChanged: () => void;
  onError: (v: string) => void;
}) {
  const [adopting, setAdopting] = useState<string>("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  const adopt = async () => {
    setBusy(true);
    try {
      await createAgentFromTemplate({ name, template_id: adopting });
      setName("");
      setAdopting("");
      onChanged();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const publish = async (tpl: AgentTemplate) => {
    setBusy(true);
    try {
      await publishAgentTemplate(tpl.id, tpl.revision);
      onChanged();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (!templates.length) {
    return (
      <Panel className="p-4">
        <p className="text-[12px] font-medium">No templates yet</p>
        <p className="mt-1 text-[11px] leading-relaxed text-[--color-ink-3]">
          A template names a harness and the scope of work an agent from it may
          do, so a colleague can start from something reviewed instead of
          configuring an agent from nothing. Templates are data: they cannot run
          anything and cannot widen what an operator has granted.
        </p>
      </Panel>
    );
  }

  return (
    <Panel className="p-4">
      <p className="text-[12px] font-medium">Start from a template</p>
      <p className="mt-1 text-[11px] text-[--color-ink-3]">
        Adopting a template pins the agent to that version. Customization can
        narrow its scope, never widen it.
      </p>
      <div className="mt-3 space-y-2">
        {templates.map((tpl) => (
          <div
            key={tpl.id}
            className="flex flex-wrap items-center gap-2 rounded-md border border-[--color-line] px-2.5 py-2"
          >
            <span className="text-[11px] font-medium">{tpl.name}</span>
            <span className="font-mono text-[10px] text-[--color-ink-3]">
              {tpl.version} · {tpl.harness}
            </span>
            <Badge tone={tpl.status === "published" ? "accent" : "neutral"}>
              {tpl.status}
            </Badge>
            {tpl.capabilities.length ? (
              <span className="text-[10px] text-[--color-ink-3]">
                scope: {tpl.capabilities.join(", ")}
              </span>
            ) : (
              <span className="text-[10px] text-[--color-ink-3]">
                no capabilities — it may prepare, nothing more
              </span>
            )}

            <span className="ms-auto flex items-center gap-2">
              {tpl.status === "draft" ? (
                <>
                  <span className="text-[10px] text-[--color-ink-3]">
                    not selectable until published
                  </span>
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => void publish(tpl)}>
                    Publish
                  </Button>
                </>
              ) : tpl.status === "published" ? (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={busy}
                  onClick={() => setAdopting(adopting === tpl.id ? "" : tpl.id)}
                >
                  Adopt
                </Button>
              ) : null}
            </span>

            {adopting === tpl.id ? (
              <div className="mt-1 flex w-full flex-wrap items-center gap-2">
                <Input
                  autoFocus
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Name this agent"
                  className="max-w-xs"
                />
                <Button
                  size="sm"
                  variant="accent"
                  disabled={busy || !name.trim()}
                  onClick={() => void adopt()}
                >
                  Create agent
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setAdopting("")}>
                  Cancel
                </Button>
              </div>
            ) : null}
          </div>
        ))}
      </div>
    </Panel>
  );
}
