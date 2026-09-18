import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { FolderKanban, Plus } from "lucide-react";
import {
  createProject,
  createTask,
  listProjects,
  listTasks,
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
 * A project is a container for work, not a second status system: progress here is derived from
 * the tasks inside it rather than tracked separately, so the two can never disagree.
 */
export function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);

  const load = () =>
    Promise.all([listProjects(), listTasks()])
      .then(([p, t]) => {
        setProjects(p);
        setTasks(t);
        setError("");
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));

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
