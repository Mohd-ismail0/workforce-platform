import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { CheckCircle2, FolderKanban, Plus } from "lucide-react";
import {
  completeMilestone,
  createMilestone,
  createProject,
  createTask,
  listMilestones,
  listProjects,
  listTasks,
  setMilestoneForecast,
  type Milestone,
  type Project,
  type Task,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, Input, Select, Textarea } from "@/components/ui/input";
import { Empty, Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";

/**
 * Projects.
 *
 * Two things live here and they are not the same kind of thing.
 *
 * A project is a container for work, and its task counts are DERIVED from the tasks inside
 * rather than tracked separately, so the two can never disagree.
 *
 * A milestone is a stated outcome with declared acceptance evidence. It is deliberately not a
 * "Done" column and not a summary of its tasks: "everything underneath moved" is not the same
 * as "the outcome was achieved", and a plan that cannot tell those apart stops meaning
 * anything. Completing one requires evidence to be recorded, and its own prerequisites to be
 * met.
 */
export function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [milestones, setMilestones] = useState<Record<string, Milestone[]>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);

  const load = async () => {
    try {
      const [p, t] = await Promise.all([listProjects(), listTasks()]);
      setProjects(p);
      setTasks(t);
      // One request per project. A milestone list is small and this avoids a
      // second aggregate endpoint; if a project count ever made this slow it
      // would become one server-side query.
      const ms = await Promise.all(
        p.map(async (proj) => {
          try {
            return [proj.id, await listMilestones(proj.id)] as const;
          } catch {
            return [proj.id, [] as Milestone[]] as const;
          }
        }),
      );
      setMilestones(Object.fromEntries(ms));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const byProject = useMemo(() => {
    const map = new Map<string, Task[]>();
    for (const t of tasks) {
      const key = t.project_id || "";
      map.set(key, [...(map.get(key) ?? []), t]);
    }
    return map;
  }, [tasks]);

  return (
    <>
      <PageHeader
        eyebrow="Projects"
        title="Projects"
        description="Containers for related work. Progress is counted from the tasks inside, never tracked separately."
        actions={
          <Button size="sm" onClick={() => setCreating((v) => !v)}>
            <Plus className="size-3.5" />
            New project
          </Button>
        }
      />

      <div className="space-y-3 p-6">
        {creating ? (
          <NewProjectForm
            onDone={() => {
              setCreating(false);
              void load();
            }}
            onError={setError}
          />
        ) : null}

        {error ? (
          <Panel className="border-[--color-danger]/40 p-4">
            <p className="text-[12px] font-medium">Could not load projects</p>
            <p className="mt-1 text-[11px] text-[--color-ink-3]">{error}</p>
          </Panel>
        ) : loading ? (
          <p className="text-[12px] text-[--color-ink-3]">Loading…</p>
        ) : !projects.length ? (
          <Empty
            title="No projects yet"
            reason="Create one to group related tasks. Tasks can also exist without a project."
          />
        ) : (
          projects.map((p) => {
            const items = byProject.get(p.id) ?? [];
            const open = items.filter(
              (t) => !["done", "cancelled"].includes(t.status),
            );
            const done = items.length - open.length;
            return (
              <Panel key={p.id} className="p-4">
                <div className="flex items-start gap-3">
                  <span className="grid size-7 shrink-0 place-items-center rounded-md bg-[--color-raised] text-[--color-ink-3]">
                    <FolderKanban className="size-3.5" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-[12px] font-medium">{p.name}</p>
                    {p.description ? (
                      <p className="mt-0.5 text-[11px] text-[--color-ink-3]">
                        {p.description}
                      </p>
                    ) : null}
                    <p className="mt-1.5 flex items-center gap-2 text-[11px] text-[--color-ink-3]">
                      <span className="tabular">{open.length} open</span>
                      <span>·</span>
                      <span className="tabular">{done} done</span>
                    </p>
                    {items.length ? (
                      <div className="mt-2 flex flex-wrap gap-1.5">
                        {items.slice(0, 6).map((t) => (
                          <Link
                            key={t.id}
                            to={`/tasks/${t.id}`}
                            className="rounded border border-[--color-line-strong] px-1.5 py-0.5 text-[10px] hover:border-[--color-ink-3]"
                          >
                            {t.title}
                          </Link>
                        ))}
                        {items.length > 6 ? (
                          <span className="px-1.5 py-0.5 text-[10px] text-[--color-ink-3]">
                            +{items.length - 6} more
                          </span>
                        ) : null}
                      </div>
                    ) : null}
                    <MilestoneList
                      projectId={p.id}
                      milestones={milestones[p.id] ?? []}
                      onChanged={() => void load()}
                      onError={setError}
                    />
                  </div>
                  <Badge tone={open.length ? "neutral" : "accent"}>
                    {open.length ? "active" : "complete"}
                  </Badge>
                </div>
              </Panel>
            );
          })
        )}

        <NewTaskForm
          projects={projects}
          onDone={() => void load()}
          onError={setError}
        />
      </div>
    </>
  );
}

function NewProjectForm({
  onDone,
  onError,
}: {
  onDone: () => void;
  onError: (v: string) => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);

  return (
    <Panel className="p-4">
      <form
        className="grid gap-3"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await createProject({ name, description });
            onDone();
          } catch (err) {
            onError((err as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Name">
            <Input
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field label="Description" hint="optional">
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
        </div>
        <div>
          <Button type="submit" variant="accent" size="sm" disabled={busy}>
            Create project
          </Button>
        </div>
      </form>
    </Panel>
  );
}

/**
 * Creating a task.
 *
 * Deliberately minimal: a title and what "done" means. Owner defaults to the person creating
 * it, which is the accountability rule the kernel enforces — a task always has a human owner
 * even when an agent does the work.
 */
function NewTaskForm({
  projects,
  onDone,
  onError,
}: {
  projects: Project[];
  onDone: () => void;
  onError: (v: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [projectId, setProjectId] = useState("");
  const [busy, setBusy] = useState(false);

  if (!open) {
    return (
      <Button size="sm" onClick={() => setOpen(true)}>
        <Plus className="size-3.5" />
        New task
      </Button>
    );
  }

  return (
    <Panel className="p-4">
      <form
        className="grid gap-3"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await createTask({
              title,
              description,
              project_id: projectId || undefined,
            });
            setTitle("");
            setDescription("");
            setProjectId("");
            setOpen(false);
            onDone();
          } catch (err) {
            onError((err as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="What needs doing?">
          <Input
            required
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Reconcile last week's supplier deliveries"
          />
        </Field>
        <Field
          label="What does done look like?"
          hint="The acceptance criteria. A reviewer uses this to judge the result."
        >
          <Textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>
        <Field label="Project" hint="optional">
          <Select
            value={projectId}
            onChange={(e) => setProjectId(e.target.value)}
          >
            <option value="">No project</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </Field>
        <div className="flex gap-2">
          <Button type="submit" variant="accent" size="sm" disabled={busy}>
            Create task
          </Button>
          <Button
            variant="ghost"
            size="sm"
            type="button"
            onClick={() => setOpen(false)}
          >
            Cancel
          </Button>
        </div>
      </form>
    </Panel>
  );
}

function shortDay(iso: string) {
  if (!iso) return "";
  const t = Date.parse(iso);
  return Number.isFinite(t)
    ? new Date(t).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })
    : iso;
}

/**
 * Milestones for one project.
 *
 * Each one shows its declared acceptance evidence next to its state, because the
 * point of the evidence is to be checked against the claim. Committed and
 * forecast dates are shown together so a slip is visible rather than implied —
 * a plan that hides the difference between a promise and a guess cannot be
 * trusted about either.
 */
function MilestoneList({
  projectId,
  milestones,
  onChanged,
  onError,
}: {
  projectId: string;
  milestones: Milestone[];
  onChanged: () => void;
  onError: (v: string) => void;
}) {
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [evidence, setEvidence] = useState("");
  const [committed, setCommitted] = useState("");
  const [forecast, setForecast] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await createMilestone(projectId, {
        name,
        acceptance_evidence: evidence,
        committed_date: committed || undefined,
        forecast_date: forecast || undefined,
      });
      setName("");
      setEvidence("");
      setCommitted("");
      setForecast("");
      setAdding(false);
      onChanged();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-3 border-t border-[--color-line] pt-2">
      <div className="flex items-baseline gap-2">
        <p className="text-[11px] font-medium">Milestones</p>
        <span className="text-[10px] text-[--color-ink-3]">
          outcomes with acceptance evidence, not a summary of the task list
        </span>
      </div>

      {milestones.length ? (
        <div className="mt-1.5 space-y-1.5">
          {milestones.map((m) => (
            <MilestoneRow key={m.id} m={m} onChanged={onChanged} onError={onError} />
          ))}
        </div>
      ) : (
        <p className="mt-1.5 text-[10px] text-[--color-ink-3]">
          None yet. A milestone states what must be true for the outcome to count
          as achieved.
        </p>
      )}

      {adding ? (
        <div className="mt-2 grid gap-2 rounded-md border border-[--color-line] p-2.5">
          <Field label="Outcome">
            <Input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Supplier replies sent for every open invoice"
            />
          </Field>
          <Field
            label="What must be true for this to count as met?"
            hint="Required. Without it, 'met' can only ever be an assertion."
          >
            <Textarea
              value={evidence}
              onChange={(e) => setEvidence(e.target.value)}
              placeholder="All 42 open invoices have a sent reply recorded, and none bounced."
            />
          </Field>
          <div className="grid gap-2 sm:grid-cols-2">
            <Field label="Committed date" hint="a promise">
              <Input type="date" value={committed} onChange={(e) => setCommitted(e.target.value)} />
            </Field>
            <Field label="Forecast date" hint="a guess, revised freely">
              <Input type="date" value={forecast} onChange={(e) => setForecast(e.target.value)} />
            </Field>
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="accent"
              disabled={busy || !name.trim() || !evidence.trim()}
              onClick={() => void submit()}
            >
              Add milestone
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setAdding(false)}>
              Cancel
            </Button>
          </div>
        </div>
      ) : (
        <Button size="sm" variant="ghost" className="mt-1.5" onClick={() => setAdding(true)}>
          <Plus className="size-3" />
          Add milestone
        </Button>
      )}
    </div>
  );
}

function MilestoneRow({
  m,
  onChanged,
  onError,
}: {
  m: Milestone;
  onChanged: () => void;
  onError: (v: string) => void;
}) {
  const [recording, setRecording] = useState(false);
  const [evidence, setEvidence] = useState("");
  const [busy, setBusy] = useState(false);

  const blocked = m.unmet_prerequisites > 0;
  // A slip is the forecast landing after the promise. Shown, not hidden.
  const slip =
    m.committed_date && m.forecast_date && m.forecast_date > m.committed_date;

  const record = async () => {
    setBusy(true);
    try {
      await completeMilestone(m.id, evidence, m.version);
      setEvidence("");
      setRecording(false);
      onChanged();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const revise = async (value: string) => {
    setBusy(true);
    try {
      await setMilestoneForecast(m.id, value);
      onChanged();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="rounded-md border border-[--color-line] px-2.5 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[11px] font-medium">{m.name}</span>
        <Badge
          tone={m.status === "met" ? "accent" : m.status === "cancelled" ? "neutral" : "pending"}
        >
          {m.status}
        </Badge>
        {slip ? <Badge tone="danger">forecast after commitment</Badge> : null}
        {blocked ? (
          <Badge tone="info">
            {m.unmet_prerequisites} prerequisite
            {m.unmet_prerequisites === 1 ? "" : "s"} unmet
          </Badge>
        ) : null}
      </div>

      <p className="mt-1 text-[10px] text-[--color-ink-3]">
        Acceptance: {m.acceptance_evidence}
      </p>

      <div className="mt-1 flex flex-wrap items-center gap-2 text-[10px] text-[--color-ink-3]">
        {m.committed_date ? <span>committed {shortDay(m.committed_date)}</span> : null}
        {m.forecast_date ? (
          <span className={slip ? "text-[--color-danger]" : ""}>
            forecast {shortDay(m.forecast_date)}
          </span>
        ) : null}
        {m.status === "met" && m.met_at ? <span>met {shortDay(m.met_at)}</span> : null}
      </div>

      {m.status === "met" && m.met_evidence ? (
        <p className="mt-1 flex items-start gap-1 text-[10px] text-[--color-ink-2]">
          <CheckCircle2 className="mt-0.5 size-3 shrink-0 text-[--color-accent]" />
          <span>Evidence recorded: {m.met_evidence}</span>
        </p>
      ) : null}

      {m.status !== "met" && m.status !== "cancelled" ? (
        recording ? (
          <div className="mt-2 grid gap-2">
            <Textarea
              autoFocus
              value={evidence}
              onChange={(e) => setEvidence(e.target.value)}
              placeholder="What evidence shows this outcome was achieved?"
            />
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="accent"
                disabled={busy || !evidence.trim()}
                onClick={() => void record()}
              >
                Record as met
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setRecording(false)}>
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="ghost"
              disabled={busy || blocked}
              onClick={() => setRecording(true)}
            >
              Record as met
            </Button>
            <Input
              type="date"
              className="max-w-[9.5rem]"
              disabled={busy}
              onChange={(e) => e.target.value && void revise(e.target.value)}
              title="Revise the forecast. The commitment is not moved by this."
            />
            {blocked ? (
              <span className="text-[10px] text-[--color-ink-3]">
                cannot be met until its prerequisites are met
              </span>
            ) : null}
          </div>
        )
      ) : null}
    </div>
  );
}
