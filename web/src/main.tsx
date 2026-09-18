import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  acceptHandoff,
  api,
  canApprove,
  createHandoff,
  getSession,
  getToken,
  list,
  listHandoffs,
  listRegistryReleases,
  listGates,
  loginUrl,
  logout,
  respondToGate,
  Gate,
  listRuns,
  startRun,
  createRegistryRelease,
  transitionRegistryRelease,
  Me,
  Operation,
  operationPayload,
  Project,
  Proposal,
  setToken,
  Task,
} from "./api";
import "./styles.css";

const nav = [
  "Overview",
  "Projects & tasks",
  "My decisions",
  "Integrations",
  "Agents",
  "Activity & receipts",
  "Handoffs",
  "Needs input",
  "Runs",
  "Registry",
];
const statuses = [
  "draft",
  "ready",
  "in_progress",
  "in_review",
  "blocked",
  "done",
  "cancelled",
];
const empty = (
  <div className="empty-panel">
    <h3>No records returned</h3>
    <p className="muted">
      The backend returned an empty list. Nothing is invented in this view.
    </p>
  </div>
);

function App() {
  const [me, setMe] = useState<Me>();
  const [page, setPage] = useState("Overview");
  const [error, setError] = useState("");
  const [token, setTok] = useState(getToken());
  const [loading, setLoading] = useState(true);
  // authMode is unknown until the backend has been asked. "session" means the BFF browser flow
  // is configured (OIDC); "local" means /auth/session refused because it is not configured.
  // The distinction matters: showing a token form in a deployment that uses real sign-in would
  // invite a shared secret as a credential.
  const [authMode, setAuthMode] = useState<"unknown" | "session" | "local">("unknown");
  const [signedIn, setSignedIn] = useState(false);
  const [authError, setAuthError] = useState("");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get("auth_error");
    if (code) {
      setAuthError(code);
      // Strip it so a reload does not keep re-showing a failure the user already read.
      window.history.replaceState({}, "", window.location.pathname);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setMe(undefined);
    // Ask the BFF FIRST. In OIDC mode the credential is an HttpOnly cookie the page cannot
    // read, so there is no token to look for and the token form must never be the default.
    getSession()
      .then((s) => {
        if (cancelled) return;
        if (s === null) {
          setAuthMode("local");
          return;
        }
        setAuthMode("session");
        if (s.authenticated && s.identity) {
          setSignedIn(true);
          setMe(s.identity);
          return;
        }
        setSignedIn(false);
      })
      .catch(() => {
        if (!cancelled) setAuthMode("local");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Local development mode still requires an explicit token before /me can be called.
  useEffect(() => {
    if (authMode !== "local" || !token) return;
    let cancelled = false;
    setLoading(true);
    api<Me>("/me")
      .then((v) => {
        if (!cancelled) setMe(v);
      })
      .catch((e) => {
        if (!cancelled) setError(e.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [authMode, token]);

  if (loading)
    return <div className="center">Connecting to the control plane…</div>;

  if (!me) {
    if (authMode === "session") {
      return (
        <SignIn
          signedIn={signedIn}
          error={authError}
          onSignIn={() => {
            // A full navigation, not a fetch: the provider must set its own cookies.
            window.location.href = loginUrl("/");
          }}
        />
      );
    }
    return <Login token={token} setTok={setTok} error={error} />;
  }

  return (
    <Shell
      me={me}
      page={page}
      setPage={setPage}
      error={error}
      setError={setError}
      logout={() => {
        setError("");
        if (authMode === "session") {
          // For a cookie session, logging out must end it server-side. Clearing a client
          // variable would leave the cookie in place and the user still signed in.
          void logout().then((url) => {
            window.location.href = url || "/";
          });
          return;
        }
        setToken("");
        setTok("");
        setMe(undefined);
      }}
    />
  );
}
export function HandoffForms({
  task,
  setError,
  onAccepted,
}: {
  task: Task;
  setError: (v: string) => void;
  onAccepted: () => void;
}) {
  const [recipient, setRecipient] = useState("");
  const [role, setRole] = useState<"owner" | "assignee">("owner");
  const [summary, setSummary] = useState("");
  const [offerId, setOfferId] = useState("");
  const [accepted, setAccepted] = useState(false);
  const offer = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const created = await createHandoff(task.id, {
        recipient_id: recipient,
        role,
        summary,
      });
      setOfferId(created.id);
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const accept = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await acceptHandoff(offerId);
      setAccepted(true);
      onAccepted();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <section className="composer">
      <h3>Handoff</h3>
      <form onSubmit={offer} aria-label="Create handoff offer">
        <label>
          Recipient ID
          <input
            required
            value={recipient}
            onChange={(e) => setRecipient(e.target.value)}
          />
        </label>
        <label>
          Role
          <select
            value={role}
            onChange={(e) => setRole(e.target.value as "owner" | "assignee")}
          >
            <option value="owner">owner</option>
            <option value="assignee">assignee</option>
          </select>
        </label>
        <label>
          Summary
          <textarea
            required
            value={summary}
            onChange={(e) => setSummary(e.target.value)}
          />
        </label>
        <button className="primary">Offer handoff</button>
      </form>
      <p role="status">
        {offerId ? (
          <>
            Offer created: <code>{offerId}</code>. Incoming offers appear in
            Handoffs, where only the intended recipient can accept.
          </>
        ) : (
          "Incoming offers appear in Handoffs, where only the intended recipient can accept."
        )}
      </p>
    </section>
  );
}

export function SignIn({
  signedIn,
  error,
  onSignIn,
}: {
  signedIn: boolean;
  error: string;
  onSignIn: () => void;
}) {
  // Coarse, fixed wording per reason. The code never identifies which account was involved,
  // so it cannot be used to probe whether a given subject is provisioned.
  const messages: Record<string, string> = {
    unlinked:
      "You signed in successfully, but no platform identity is linked to this account yet. An administrator has to provision access — it is not granted automatically on first sign-in.",
    login_failed:
      "The sign-in could not be completed. Try again; if it keeps failing an administrator should check the identity provider configuration.",
    login_expired: "That sign-in attempt expired before it finished. Start again.",
    provider_refused: "The identity provider refused the sign-in request.",
    session_unusable: "Your session is no longer valid. Sign in again to continue.",
  };
  return (
    <main className="login">
      <div className="login-card">
        <div className="brand-mark">W</div>
        <p className="eyebrow">WORKFORCE / SIGN IN</p>
        <h1>Sign in to the control room</h1>
        <p className="muted">
          Sign-in is handled by your organization&apos;s identity provider. This application
          never sees or stores your password.
        </p>
        {error && (
          <div className="alert error">{messages[error] || "Sign-in failed."}</div>
        )}
        {signedIn && !error && (
          <div className="alert error">
            Your session ended. Sign in again to continue.
          </div>
        )}
        <button className="primary" type="button" onClick={onSignIn}>
          Sign in with SSO
        </button>
        <small>
          Access is not granted by signing in. An administrator provisions each person
          explicitly, and actions are recorded against that identity.
        </small>
      </div>
    </main>
  );
}

export function Login({
  token,
  setTok,
  error,
}: {
  token: string;
  setTok: (v: string) => void;
  error: string;
}) {
  return (
    <main className="login">
      <form
        className="login-card"
        onSubmit={(e) => {
          e.preventDefault();
          setToken(token);
          setTok(token);
        }}
      >
        <div className="brand-mark">W</div>
        <p className="eyebrow">WORKFORCE / LOCAL DEV</p>
        <h1>Sign in to the control room</h1>
        <p className="muted">
          Enter a server-authenticated opaque session key.
        </p>
        <label>
          Opaque session key
          <input
            type="password"
            value={token}
            onChange={(e) => setTok(e.target.value)}
            placeholder="Bearer token"
          />
        </label>
        {error && <div className="alert error">{error}</div>}
        <button className="primary" type="submit">
          Connect
        </button>
        <small>
          Development identities only — this form is never shown when real sign-in is
          configured. Tokens are stored in sessionStorage only.
        </small>
      </form>
    </main>
  );
}
function Shell({
  me,
  page,
  setPage,
  error,
  setError,
  logout,
}: {
  me: Me;
  page: string;
  setPage: (v: string) => void;
  error: string;
  setError: (v: string) => void;
  logout: () => void;
}) {
  const [gateCount, setGateCount] = useState(0);
  const refreshGateCount = () =>
    listGates()
      .then((gates) => setGateCount(gates.length))
      .catch(() => undefined);
  useEffect(() => {
    refreshGateCount();
    window.addEventListener("gates-updated", refreshGateCount);
    return () => window.removeEventListener("gates-updated", refreshGateCount);
  }, []);
  return (
    <div className="app">
      <aside>
        <div className="logo">
          <span className="brand-mark small">W</span>
          <span>workforce</span>
        </div>
        <div className="env">
          <b>LOCAL DEV</b>
          <span>simulator-gated</span>
        </div>
        <nav>
          {nav.map((n, i) => (
            <button
              className={page === n ? "active" : ""}
              key={n}
              onClick={() => setPage(n)}
            >
              <span className="nav-icon">
                {["⌂", "▦", "✓", "◈", "◎", "≋", "⇄", "?", "▶", "◇"][i]}
              </span>
              {n}
              {n === "Needs input" && gateCount > 0 && (
                <span className="count">{gateCount}</span>
              )}
            </button>
          ))}
        </nav>
        <div className="side-foot">
          <div className="avatar">{me.name?.[0] || "U"}</div>
          <div>
            <strong>{me.name}</strong>
            <span>{me.role}</span>
          </div>
          <button className="icon-button" onClick={logout}>
            ↪
          </button>
        </div>
      </aside>
      <main className="content">
        <header>
          <div>
            <p className="eyebrow">CONTROL ROOM / {page.toUpperCase()}</p>
            <h1>{page}</h1>
          </div>
          <span className="role-pill">{me.role} access</span>
        </header>
        {error && (
          <div className="alert error top-alert">
            {error}
            <button onClick={() => setError("")}>Dismiss</button>
          </div>
        )}
        <Page page={page} me={me} setError={setError} />
      </main>
    </div>
  );
}
function Page({
  page,
  me,
  setError,
}: {
  page: string;
  me: Me;
  setError: (v: string) => void;
}) {
  if (page === "Projects & tasks") return <Projects setError={setError} />;
  if (page === "My decisions") return <Decisions me={me} setError={setError} />;
  if (page === "Integrations") return <Integrations setError={setError} />;
  if (page === "Agents") return <Agents setError={setError} />;
  if (page === "Activity & receipts") return <Activity setError={setError} />;
  if (page === "Handoffs") return <Handoffs me={me} setError={setError} />;
  if (page === "Needs input") return <Gates setError={setError} />;
  if (page === "Runs") return <Runs setError={setError} />;
  if (page === "Registry") return <Registry me={me} setError={setError} />;
  return <Overview me={me} setError={setError} />;
}
export function Gates({ setError }: { setError: (v: string) => void }) {
  const [items, setItems] = useState<Gate[]>([]);
  const [loading, setLoading] = useState(true);
  const load = () => {
    setLoading(true);
    listGates()
      .then((gates) =>
        setItems(
          [...gates].sort(
            (a, b) => Date.parse(b.created_at) - Date.parse(a.created_at),
          ),
        ),
      )
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);
  return (
    <section aria-label="Needs input">
      <div className="alert">
        Answering a question resumes the agent. The agent's output is still only
        a proposal that needs endorsement and approval before anything is
        executed.
      </div>
      <div className="toolbar">
        <div>
          <h2>Needs input</h2>
          <p className="muted">Questions waiting for your response.</p>
        </div>
        <button className="button" onClick={load}>Refresh</button>
      </div>
      {loading ? <p className="muted">Loading gates…</p> : !items.length ? (
        <div className="empty-panel"><h3>Nothing is waiting on you</h3></div>
      ) : (
        <div className="handoff-list">
          {items.map((gate) => (
            <GateCard key={gate.id} gate={gate} load={load} setError={setError} />
          ))}
        </div>
      )}
    </section>
  );
}
// maxListItems mirrors the server's bound (maxGateArrayItems): every input entry point
// is bounded, and a gate answer is one. Rejecting here gives the person a clear message
// instead of a 422 after they have typed everything.
const maxListItems = 64;

// parseListField turns one text field into a typed list answer.
//
// A gate may declare `array` with scalar `items` because a real model asked for
// `recipients` as an array — precisely what the mail connector requires. The server
// accepts that shape, but the UI rendered every non-boolean as a plain input, turning
// the answer into a bare string that the server refuses with 422. This keeps the declared
// type intact and reports an unsupported item type rather than guessing.
function parseListField(
  raw: string,
  itemType?: string,
): { ok: true; value: unknown[] } | { ok: false; error: string } {
  const parts = raw.split(",").map((p) => p.trim()).filter((p) => p !== "");
  if (parts.length > maxListItems) {
    return { ok: false, error: `at most ${maxListItems} entries` };
  }
  switch (itemType) {
    case "string":
      return { ok: true, value: parts };
    case "integer": {
      const nums: number[] = [];
      for (const p of parts) {
        const n = Number(p);
        if (!Number.isInteger(n)) return { ok: false, error: `"${p}" must be a whole number` };
        // Beyond 2^53-1 a JS number silently becomes a DIFFERENT integer, and the server
        // would accept that wrong value. Refuse rather than record something not typed.
        if (!Number.isSafeInteger(n)) {
          return { ok: false, error: `"${p}" is too large to enter safely` };
        }
        nums.push(n);
      }
      return { ok: true, value: nums };
    }
    case "number": {
      const nums: number[] = [];
      for (const p of parts) {
        const n = Number(p);
        if (!Number.isFinite(n)) return { ok: false, error: `"${p}" must be a number` };
        nums.push(n);
      }
      return { ok: true, value: nums };
    }
    case "boolean": {
      // Kept for parity with the server, which accepts boolean items. Accepting only
      // the exact words (not "yes"/"1") avoids quietly recording a truthy typo as true.
      const bools: boolean[] = [];
      for (const p of parts) {
        const v = p.toLowerCase();
        if (v !== "true" && v !== "false") {
          return { ok: false, error: `"${p}" must be true or false` };
        }
        bools.push(v === "true");
      }
      return { ok: true, value: bools };
    }
    default:
      // Refuse visibly rather than sending an answer the server cannot record.
      return { ok: false, error: "this question asks for a list type this form cannot build" };
  }
}

function GateCard({ gate, load, setError }: { gate: Gate; load: () => void; setError: (v: string) => void }) {
  const properties = gate.input_schema.properties || {};
  const required = new Set(gate.input_schema.required || []);
  const [values, setValues] = useState<Record<string, string | boolean>>({});
  const [status, setStatus] = useState("");
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const response: Record<string, unknown> = {};
    for (const [name, schema] of Object.entries(properties)) {
      const value = values[name];

      // A LIST answer. The server accepts `array` fields with scalar items because a
      // real model asked for `recipients` as an array — exactly what the mail connector
      // needs. Rendering every non-boolean as a text input silently turned that answer
      // into a bare string, which the server rejects with 422: the question was
      // answerable by a script but not by a person.
      if (schema.type === "array") {
        const parsed = parseListField(String(value ?? ""), schema.items?.type);
        if (!parsed.ok) {
          setStatus(`${name}: ${parsed.error}`);
          return;
        }
        if (required.has(name) && parsed.value.length === 0) {
          setStatus(`${name} needs at least one entry.`);
          return;
        }
        if (parsed.value.length > 0) response[name] = parsed.value;
        continue;
      }

      if (required.has(name) && (schema.type === "boolean" ? value === undefined : String(value ?? "").trim() === "")) {
        setStatus(`${name} is required.`);
        return;
      }
      if (value === undefined || (schema.type === "string" && value === "" && !required.has(name))) continue;
      if (schema.type === "integer") {
        const number = Number(value);
        if (!Number.isInteger(number)) { setStatus(`${name} must be a whole number.`); return; }
        response[name] = number;
      } else if (schema.type === "number") response[name] = Number(value);
      else response[name] = value;
    }
    try {
      const resolved = await respondToGate(gate.id, { revision: gate.revision, response });
      setStatus(`Answer recorded for agent run ${resolved.run_id}. It is queued to resume — watch Runs.`);
      window.dispatchEvent(new Event("gates-updated"));
      load();
    } catch (error) {
      const e = error as Error & { status?: number };
      if (e.status === 409) { setStatus(e.message); load(); }
      else if (e.status === 422) setStatus(e.message);
      else if (e.status === 403) setStatus("You are not the respondent for this gate.");
      else setError(e.message);
    }
  };
  return (
    <article className="panel handoff-card">
      <span className="tag">{gate.kind === "missing_information" ? "Missing information" : gate.kind[0].toUpperCase() + gate.kind.slice(1)}</span>
      <h3>{gate.prompt}</h3>
      <p className="muted">Task <code>{gate.task_id}</code> · Run <code>{gate.run_id}</code></p>
      <small className="muted">Created {gate.created_at}</small>
      <form className="composer" onSubmit={submit} aria-label={`Answer gate ${gate.id}`}>
        {Object.entries(properties).map(([name, schema]) => (
          <label key={name}>
            {name}{required.has(name) ? " *" : ""}
            {schema.type === "boolean" ? (
              <input type="checkbox" checked={Boolean(values[name])} onChange={(e) => setValues({ ...values, [name]: e.target.checked })} />
            ) : schema.type === "array" ? (
              // A LIST answer is entered as comma-separated text and converted on submit.
              // The previous fallback rendered it as a number input, so a list of email
              // addresses could not be typed at all — the question was answerable by a
              // script but not by a person.
              <input
                type="text"
                placeholder="comma-separated"
                value={String(values[name] ?? "")}
                onChange={(e) => setValues({ ...values, [name]: e.target.value })}
              />
            ) : (
              <input type={schema.type === "string" ? "text" : "number"} step={schema.type === "integer" ? 1 : undefined} value={String(values[name] ?? "")} onChange={(e) => setValues({ ...values, [name]: e.target.value })} />
            )}
          </label>
        ))}
        <button className="primary">Submit answer</button>
      </form>
      {status && <p role="status">{status} {status.includes("queued to resume") && <a href="#runs">See Runs</a>}</p>}
    </article>
  );
}

export function Runs({ setError }: { setError: (v: string) => void }) {
  const [items, setItems] = useState<import("./api").AgentRun[]>([]);
  const [loading, setLoading] = useState(true);
  const load = () => {
    setLoading(true);
    listRuns()
      .then(setItems)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  };
  useEffect(() => {
    load();
  }, []);
  const active = items.some(
    (run) => run.status === "queued" || run.status === "running",
  );
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(load, 2000);
    return () => window.clearInterval(timer);
  }, [active]);
  return (
    <section aria-label="Agent runs">
      <div className="alert error">
        <strong>SIMULATOR runner only:</strong> The only available runner is an
        in-process SIMULATOR that prepares a proposal. It does not execute a
        real AI coding harness, does not run external commands, and its output
        still requires endorsement, approval and the normal governed execution
        path.
      </div>
      <div className="toolbar">
        <div>
          <h2>Agent runs</h2>
          <p className="muted">
            Queued runs are not shown as working; only running runs are active.
          </p>
        </div>
        <button className="button" onClick={load}>
          Refresh
        </button>
      </div>
      {loading ? (
        <p className="muted">Loading runs…</p>
      ) : !items.length ? (
        empty
      ) : (
        <div className="table">
          {items.map((run) => (
            <article className="table-row" key={run.id}>
              <div>
                <strong>{run.harness}</strong>
                <span className="muted">Runner: {run.runner_id}</span>
              </div>
              <span className="status-chip">
                {run.status === "waiting" ? "Waiting for input" : run.status}
              </span>
              <div>
                <span className="label">INTENT</span>
                <p>{run.intent}</p>
                <span className="muted">
                  Task <code>{run.task_id}</code>
                </span>
              </div>
              <div>
                {run.status === "waiting" ? (
                  <span>Waiting for input — no worker is held. <a href="#needs-input">Needs input</a></span>
                ) : run.status === "failed" ||
                  run.status === "cancelled" ||
                  run.failure_reason ? (
                  <strong>{run.failure_reason || "Run blocked"}</strong>
                ) : (
                  run.result_summary || "No result yet"
                )}
              </div>
              {run.proposal_id && (
                <a href={`#proposal-${run.proposal_id}`}>
                  Proposal {run.proposal_id}
                </a>
              )}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

export function Handoffs({
  me,
  setError,
}: {
  me: Me;
  setError: (v: string) => void;
}) {
  const [items, setItems] = useState<import("./api").HandoffOffer[]>([]);
  const [loading, setLoading] = useState(true);
  const load = () => {
    setLoading(true);
    listHandoffs()
      .then(setItems)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);
  const accept = async (id: string) => {
    try {
      await acceptHandoff(id);
      load();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <section aria-label="Handoff inbox">
      <div className="toolbar">
        <div>
          <h2>Handoffs</h2>
          <p className="muted">
            Incoming offers for you and outgoing offers you created.
          </p>
        </div>
        <button className="button" onClick={load}>
          Refresh
        </button>
      </div>
      {loading ? (
        <p className="muted">Loading handoffs…</p>
      ) : !items.length ? (
        empty
      ) : (
        <div className="handoff-list">
          {items.map((h) => {
            const incoming = h.recipient_id === me.id;
            return (
              <article className="panel handoff-card" key={h.id}>
                <div className="handoff-head">
                  <span className="tag">
                    {incoming ? "INCOMING" : "OUTGOING"}
                  </span>
                  <span className="status-chip">{h.state}</span>
                </div>
                <h3>{h.summary}</h3>
                <p className="muted">
                  Task <code>{h.task_id}</code> · role: {h.role}
                </p>
                <small className="muted">Created {h.created_at}</small>
                {incoming && h.state === "offered" && (
                  <button className="primary" onClick={() => accept(h.id)}>
                    Accept handoff
                  </button>
                )}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}
function Overview({ me, setError }: { me: Me; setError: (v: string) => void }) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [decisions, setDecisions] = useState<any[]>([]);
  useEffect(() => {
    Promise.all([list<Task>("/tasks"), list<any>("/decisions")])
      .then(([t, d]) => {
        setTasks(t);
        setDecisions(d);
      })
      .catch((e) => setError(e.message));
  }, []);
  return (
    <>
      <section className="welcome">
        <div>
          <p className="eyebrow">GOOD MORNING, {me.name?.toUpperCase()}</p>
          <h2>Make work legible.</h2>
          <p className="muted">
            A permission-aware queue for tasks, proposals, and human decisions.
          </p>
        </div>
        <span className="status-dot">● API connected</span>
      </section>
      <div className="grid stats">
        <article>
          <span className="label">OPEN TASKS</span>
          <strong>
            {
              tasks.filter((t) => !["done", "cancelled"].includes(t.status))
                .length
            }
          </strong>
        </article>
        <article>
          <span className="label">AWAITING YOUR DECISION</span>
          <strong>{decisions.length}</strong>
        </article>
        <article>
          <span className="label">WORKSPACE</span>
          <strong className="mono">{me.org_id.slice(0, 10)}</strong>
        </article>
      </div>
      {tasks.length ? (
        <section className="panel">
          <h3>Recent work</h3>
          {tasks.slice(0, 5).map((t) => (
            <p key={t.id}>
              <strong>{t.title}</strong>{" "}
              <span className="muted">{t.status}</span>
            </p>
          ))}
        </section>
      ) : (
        empty
      )}
    </>
  );
}
function Projects({ setError }: { setError: (v: string) => void }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [showProject, setShowProject] = useState(false);
  const [showTask, setShowTask] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [title, setTitle] = useState("");
  const [selectedProject, setSelectedProject] = useState("");
  const [selected, setSelected] = useState<Task>();
  const refresh = () =>
    Promise.all([list<Project>("/projects"), list<Task>("/tasks")])
      .then(([p, t]) => {
        setProjects(p);
        setTasks(t);
      })
      .catch((e) => setError(e.message));
  useEffect(() => {
    refresh();
  }, []);
  const createProject = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api("/projects", {
        method: "POST",
        body: JSON.stringify({ name, description }),
      });
      setName("");
      setDescription("");
      setShowProject(false);
      refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const createTask = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await api("/tasks", {
        method: "POST",
        body: JSON.stringify({
          title,
          description: "",
          project_id: selectedProject || undefined,
        }),
      });
      setTitle("");
      setSelectedProject("");
      setShowTask(false);
      refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <>
      <div className="toolbar">
        <div>
          <h2>Projects and tasks</h2>
          <p className="muted">
            Create work, inspect immutable task versions, and build proposals.
          </p>
        </div>
        <button className="button" onClick={() => setShowProject(true)}>
          ＋ New project
        </button>
        <button className="primary" onClick={() => setShowTask(true)}>
          ＋ New task
        </button>
      </div>
      {showProject && (
        <form className="composer" onSubmit={createProject}>
          <label>
            Project name
            <input
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label>
            Description
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
          <button className="primary">Create project</button>
        </form>
      )}
      {showTask && (
        <form className="composer" onSubmit={createTask}>
          <label>
            Task title
            <input
              required
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </label>
          <label>
            Project (optional)
            <select
              value={selectedProject}
              onChange={(e) => setSelectedProject(e.target.value)}
            >
              <option value="">No project</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          <button className="primary">Create task</button>
        </form>
      )}
      <div className="project-list">
        {projects.map((p) => (
          <div className="project-row" key={p.id}>
            <div className="project-glyph">{p.name?.[0] || "P"}</div>
            <div>
              <strong>{p.name}</strong>
              <p className="muted">{p.description || "No description"}</p>
            </div>
            <span className="count">
              {tasks.filter((t) => t.project_id === p.id).length} tasks
            </span>
          </div>
        ))}
        {!projects.length && empty}
      </div>
      <h3 className="subhead">Task board</h3>
      <div className="board">
        {statuses.map((status) => (
          <div className="lane" key={status}>
            <div className="lane-head">
              <span>{status.replace("_", " ")}</span>
              <b>{tasks.filter((t) => t.status === status).length}</b>
            </div>
            {tasks
              .filter((t) => t.status === status)
              .map((t) => (
                <button
                  className="task-card"
                  key={t.id}
                  onClick={() => setSelected(t)}
                >
                  <strong>{t.title}</strong>
                  <span className="muted">v{t.version}</span>
                </button>
              ))}
          </div>
        ))}
      </div>
      {selected && (
        <TaskDetail
          task={selected}
          close={() => setSelected(undefined)}
          refresh={refresh}
          setError={setError}
        />
      )}
    </>
  );
}
export function TaskDetail({
  task,
  close,
  refresh,
  setError,
}: {
  task: Task;
  close: () => void;
  refresh: () => void;
  setError: (v: string) => void;
}) {
  const [parent, setParent] = useState("");
  const [proposal, setProposal] = useState(false);
  const [summary, setSummary] = useState("");
  const [businessKey, setBusinessKey] = useState("");
  const [integration, setIntegration] =
    useState<Operation["integration"]>("inventory");
  const [action, setAction] = useState<Operation["action"]>("adjust");
  const [target, setTarget] = useState("");
  const [targetVersion, setTargetVersion] = useState<number>();
  const [targetVersionError, setTargetVersionError] = useState("");
  const [agentId, setAgentId] = useState("");
  const [runIntent, setRunIntent] = useState("");
  const [runStatus, setRunStatus] = useState("");
  const [waitingGate, setWaitingGate] = useState<Gate>();
  useEffect(() => {
    listGates()
      .then((gates) => setWaitingGate(gates.find((gate) => gate.task_id === task.id)))
      .catch(() => undefined);
  }, [task.id]);
  const [values, setValues] = useState<Record<string, string>>({
    delta: "0",
    recipients: "",
    subject: "",
    body: "",
    bcc: "",
    attachments: "",
    title: "",
    content: "",
  });
  const update = (key: string, value: string) =>
    setValues((v) => ({ ...v, [key]: value }));
  const cancel = async () => {
    try {
      await api(`/tasks/${task.id}/cancel`, {
        method: "POST",
        body: JSON.stringify({ expected_version: task.version }),
      });
      refresh();
      close();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const dependency = async () => {
    try {
      await api(`/tasks/${task.id}/dependencies`, {
        method: "POST",
        body: JSON.stringify({ parent_id: parent }),
      });
      setParent("");
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const start = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const run = await startRun(task.id, {
        agent_id: agentId,
        intent: runIntent,
      });
      setRunStatus(
        `Run ${run.id} started with status: ${run.status}. See the Runs page.`,
      );
    } catch (e) {
      setRunStatus((e as Error).message);
    }
  };
  const submitProposal = async (e: React.FormEvent) => {
    e.preventDefault();
    if (targetVersion === undefined) {
      setError(
        targetVersionError ||
          "Capture the target's current version before creating a proposal.",
      );
      return;
    }
    try {
      await api(`/tasks/${task.id}/proposals`, {
        method: "POST",
        body: JSON.stringify({
          summary,
          operations: [
            {
              business_key: businessKey,
              integration,
              action,
              target_id: target,
              expected_version: targetVersion,
              payload: operationPayload(integration, values),
            },
          ],
        }),
      });
      setProposal(false);
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <div className="modal">
      <div className="panel">
        <button className="icon-button" onClick={close}>
          ×
        </button>
        <p className="eyebrow">TASK DETAIL</p>
        <h2>{task.title}</h2>
        <p>{task.description || "No description"}</p>
        <p className="muted">
          Status: {task.status} · Frozen version: {task.version}
        </p>
        {waitingGate && (
          <div className="alert">
            <strong>Waiting for your input:</strong> {waitingGate.prompt}{" "}
            <a href="#needs-input">Answer on Needs input</a>
          </div>
        )}
        <div className="toolbar">
          <button
            className="button"
            onClick={cancel}
            disabled={task.status === "cancelled"}
          >
            Cancel task
          </button>
          <button className="primary" onClick={() => setProposal(true)}>
            Build proposal
          </button>
        </div>
        <label>
          Parent task ID
          <input
            value={parent}
            onChange={(e) => setParent(e.target.value)}
            placeholder="Known opaque task ID"
          />
        </label>
        <button className="button" onClick={dependency} disabled={!parent}>
          Add dependency
        </button>
        <HandoffForms task={task} setError={setError} onAccepted={refresh} />
        <form className="composer" onSubmit={start} aria-label="Start run">
          <h3>Start run</h3>
          <label>
            Agent ID
            <input
              required
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
            />
          </label>
          <label>
            Intent
            <textarea
              required
              value={runIntent}
              onChange={(e) => setRunIntent(e.target.value)}
            />
          </label>
          <button className="primary">Start run</button>
          <p role="status">{runStatus}</p>
        </form>
        {proposal && (
          <form className="composer" onSubmit={submitProposal}>
            <label>
              Summary
              <input
                required
                value={summary}
                onChange={(e) => setSummary(e.target.value)}
              />
            </label>
            <label>
              Business operation key
              <input
                aria-label="Business operation key"
                required
                value={businessKey}
                onChange={(e) => setBusinessKey(e.target.value)}
              />
              <small className="muted">
                The same key prevents duplicate official effects across retries
                and re-runs.
              </small>
            </label>
            <label>
              Integration
              <select
                value={integration}
                onChange={(e) => {
                  const i = e.target.value as Operation["integration"];
                  setIntegration(i);
                  setAction(
                    i === "inventory"
                      ? "adjust"
                      : i === "mail"
                        ? "send"
                        : "update",
                  );
                }}
              >
                {["inventory", "mail", "documents"].map((i) => (
                  <option key={i}>{i}</option>
                ))}
              </select>
            </label>
            <label>
              Action
              <input readOnly value={action} />
            </label>
            <label>
              Target ID
              <input
                aria-label="Target ID"
                required
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              />
            </label>
            <button
              type="button"
              className="button"
              disabled={!target}
              onClick={async () => {
                setTargetVersionError("");
                try {
                  const rows = await list<any>(
                    `/integrations/${integration}/records`,
                  );
                  const record = rows.find(
                    (r) => String(r.id ?? r.record_id) === target,
                  );
                  if (!record || typeof record.version !== "number")
                    throw new Error(
                      "Target record not found or has no version.",
                    );
                  setTargetVersion(record.version);
                } catch (e) {
                  setTargetVersion(undefined);
                  setTargetVersionError((e as Error).message);
                }
              }}
            >
              Capture target version
            </button>
            <p className="muted" role="status">
              {targetVersion !== undefined
                ? `Captured version: v${targetVersion}`
                : targetVersionError ||
                  "No target version captured; new records are not auto-approved."}
            </p>
            {integration === "inventory" && (
              <label>
                Delta
                <input
                  type="number"
                  required
                  value={values.delta}
                  onChange={(e) => update("delta", e.target.value)}
                />
              </label>
            )}
            {integration === "mail" && (
              <>
                <label>
                  Recipients (comma separated)
                  <input
                    required
                    value={values.recipients}
                    onChange={(e) => update("recipients", e.target.value)}
                  />
                </label>
                <label>
                  Subject
                  <input
                    required
                    value={values.subject}
                    onChange={(e) => update("subject", e.target.value)}
                  />
                </label>
                <label>
                  Body
                  <textarea
                    required
                    value={values.body}
                    onChange={(e) => update("body", e.target.value)}
                  />
                </label>
                <label>
                  Bcc (comma separated)
                  <input
                    required
                    value={values.bcc}
                    onChange={(e) => update("bcc", e.target.value)}
                  />
                </label>
                <label>
                  Attachments (simulator refs, comma separated)
                  <input
                    required
                    value={values.attachments}
                    onChange={(e) => update("attachments", e.target.value)}
                  />
                </label>
              </>
            )}
            {integration === "documents" && (
              <>
                <label>
                  Title
                  <input
                    value={values.title}
                    onChange={(e) => update("title", e.target.value)}
                  />
                </label>
                <label>
                  Content
                  <textarea
                    value={values.content}
                    onChange={(e) => update("content", e.target.value)}
                  />
                </label>
              </>
            )}
            <button className="primary">Create proposal</button>
          </form>
        )}
      </div>
    </div>
  );
}
function Decisions({
  me,
  setError,
}: {
  me: Me;
  setError: (v: string) => void;
}) {
  const [items, setItems] = useState<any[]>([]);
  const [selected, setSelected] = useState<any>();
  const load = () =>
    list<any>("/decisions")
      .then((x) => {
        setItems(x);
        setSelected(undefined);
      })
      .catch((e) => setError(e.message));
  useEffect(() => {
    load();
  }, []);
  return (
    <>
      <div className="toolbar">
        <div>
          <h2>My decisions</h2>
          <p className="muted">Role-scoped review of frozen revisions.</p>
        </div>
        <button className="button" onClick={load}>
          Refresh
        </button>
      </div>
      <div className="decision-layout">
        <div className="decision-list">
          {items.map((i) => (
            <button
              className={
                selected?.id === i.id ? "decision selected" : "decision"
              }
              key={i.id}
              onClick={() => setSelected(i)}
            >
              <span className="decision-kind">{i.kind}</span>
              <strong>{i.title}</strong>
              <span className="muted">
                Revision {i.revision} · {i.status}
              </span>
            </button>
          ))}
          {!items.length && empty}
        </div>
        {selected ? (
          <Review
            key={selected.id}
            item={selected}
            me={me}
            setError={setError}
            onDecision={load}
          />
        ) : (
          <div className="review">
            <p className="muted">
              Select a decision to inspect its frozen revision.
            </p>
          </div>
        )}
      </div>
    </>
  );
}
function Review({
  item,
  me,
  setError,
  onDecision,
}: {
  item: any;
  me: Me;
  setError: (v: string) => void;
  onDecision: () => void;
}) {
  const [prop, setProp] = useState<Proposal>();
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setProp(undefined);
    api<Proposal>(`/proposals/${item.proposal_id}`)
      .then(setProp)
      .catch((e) => setError(e.message));
  }, [item, setError]);
  if (!prop)
    return (
      <div className="review">
        <p className="muted">Loading frozen revision…</p>
      </div>
    );
  const frozen = prop.revision !== item.revision || prop.digest !== item.digest;
  const act = async (kind: "endorse" | "approve" | "reject") => {
    setBusy(true);
    try {
      await api(`/proposals/${prop.id}/${kind}`, {
        method: "POST",
        body: JSON.stringify({
          revision: item.revision,
          digest: item.digest,
          ...(kind === "reject" ? { reason: "Rejected in control room" } : {}),
        }),
      });
      onDecision();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="review">
      <div className="review-banner">
        {frozen
          ? "This proposal changed; refresh required."
          : "Revision frozen for review."}
      </div>
      <div className="review-head">
        <div>
          <span className="eyebrow">PROPOSAL {prop.id}</span>
          <h2>{prop.summary}</h2>
        </div>
        <div className="revision">
          REV {prop.revision}
          <br />
          <span className="mono">{prop.digest.slice(0, 16)}</span>
        </div>
      </div>
      <p className="muted">
        {prop.operations.length} typed operation
        {prop.operations.length !== 1 ? "s" : ""} · No provider writes occur
        without approval.
      </p>
      <div className="operations">
        {prop.operations.map((op, i) => (
          <OperationPreview key={op.id || i} op={op} />
        ))}
      </div>
      <div className="review-actions">
        <button
          className="button"
          disabled={
            busy ||
            frozen ||
            item.revision !== prop.revision ||
            item.digest !== prop.digest
          }
          onClick={() => act("reject")}
        >
          Reject
        </button>
        <button
          className="button"
          disabled={busy || frozen}
          onClick={() => act("endorse")}
        >
          Endorse
        </button>
        <button
          className="primary"
          disabled={busy || frozen || !canApprove(me.role)}
          onClick={() => act("approve")}
        >
          {canApprove(me.role) ? "Approve" : "Approve · approver only"}
        </button>
      </div>
    </div>
  );
}
function OperationPreview({ op }: { op: Operation }) {
  return (
    <div className="op">
      <div className="op-title">
        <span className="integration-dot">
          {op.integration[0].toUpperCase()}
        </span>
        <strong>
          {op.integration} / {op.action}
        </strong>
        <span className="tag">SIMULATOR</span>
      </div>
      <div className="op-target">
        <span className="label">TARGET</span>
        <code>{op.target_id}</code>
        <span className="label">EXPECTED VERSION</span>
        <code>v{op.expected_version}</code>
        <span className="label">BUSINESS KEY</span>
        <code>{op.business_key}</code>
      </div>
      <dl className="typed-fields">
        {Object.entries(op.payload).map(([k, v]) => (
          <div key={k}>
            <dt>{k}</dt>
            <dd>{Array.isArray(v) ? v.join(", ") || "(empty)" : String(v)}</dd>
          </div>
        ))}
      </dl>
      <pre>{JSON.stringify(op.payload, null, 2)}</pre>
    </div>
  );
}
function Integrations({ setError }: { setError: (v: string) => void }) {
  const [items, setItems] = useState<any[]>([]);
  const [records, setRecords] = useState<Record<string, any[]>>({});
  useEffect(() => {
    list<any>("/integrations")
      .then(setItems)
      .catch((e) => setError(e.message));
  }, []);
  const read = async (id: string) => {
    try {
      const rows = await list<any>(`/integrations/${id}/records`);
      setRecords((r) => ({ ...r, [id]: rows }));
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <>
      <div className="toolbar">
        <div>
          <h2>Integrations</h2>
          <p className="muted">
            Read-only simulator records. Proposal actions are gated.
          </p>
        </div>
      </div>
      <div className="integration-grid">
        {items.map((x) => (
          <article className="integration" key={x.id}>
            <div className="integration-top">
              <span className="big-icon">
                {x.name?.[0] || x.id?.[0] || "I"}
              </span>
              <span className="tag">
                {x.simulation ? "SIMULATOR" : "UNAVAILABLE"}
              </span>
            </div>
            <h3>{x.name || x.id}</h3>
            <p className="muted">{x.kind || "Integration"}</p>
            <div className="chips">
              {(x.actions || []).map((a: string) => (
                <span key={a}>{a}</span>
              ))}
            </div>
            <button className="button" onClick={() => read(x.id)}>
              Read records
            </button>
            {records[x.id] && (
              <pre className="records">
                {JSON.stringify(records[x.id], null, 2)}
              </pre>
            )}
          </article>
        ))}
        {!items.length && empty}
      </div>
    </>
  );
}
function Agents({ setError }: { setError: (v: string) => void }) {
  const [items, setItems] = useState<any[]>([]);
  const [show, setShow] = useState(false);
  const [name, setName] = useState("");
  const [harness, setHarness] = useState("");
  const [owner, setOwner] = useState("");
  const [capabilities, setCapabilities] = useState("");
  useEffect(() => {
    list<any>("/agents")
      .then(setItems)
      .catch((e) => setError(e.message));
  }, []);
  const register = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const agent = await api<any>("/agents", {
        method: "POST",
        body: JSON.stringify({
          name,
          harness,
          owner_id: owner,
          capabilities: capabilities
            .split(",")
            .map((x) => x.trim())
            .filter(Boolean),
        }),
      });
      setItems((a) => [...a, agent]);
      setShow(false);
      setName("");
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <>
      <div className="toolbar">
        <div>
          <h2>Agent registry</h2>
          <p className="muted">
            Registration metadata only; unavailable harnesses are not runnable.
          </p>
        </div>
        <button className="primary" onClick={() => setShow(true)}>
          ＋ Register agent
        </button>
      </div>
      {show && (
        <form className="composer" onSubmit={register}>
          <label>
            Name
            <input
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label>
            Harness
            <input
              required
              value={harness}
              onChange={(e) => setHarness(e.target.value)}
              placeholder="Harness identifier"
            />
          </label>
          <label>
            Owner person ID
            <input
              required
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
            />
          </label>
          <label>
            Capabilities
            <input
              value={capabilities}
              onChange={(e) => setCapabilities(e.target.value)}
            />
          </label>
          <button className="primary">Register</button>
        </form>
      )}
      <div className="table">
        {items.map((a) => (
          <div className="table-row" key={a.id}>
            <div>
              <strong>{a.name}</strong>
              <span className="muted">
                {a.harness || "harness unavailable"}
              </span>
            </div>
            <span className="status-chip">{a.status || "unavailable"}</span>
            <div className="chips">
              {(a.capabilities || []).map((x: string) => (
                <span key={x}>{x}</span>
              ))}
            </div>
          </div>
        ))}
        {!items.length && empty}
      </div>
    </>
  );
}
export function Registry({
  me,
  setError,
}: {
  me: Me;
  setError: (v: string) => void;
}) {
  const [releases, setReleases] = useState<import("./api").RegistryRelease[]>(
    [],
  );
  const [reason, setReason] = useState("");
  const [form, setForm] = useState({
    family: "",
    kind: "",
    version: "",
    digest: "",
    requested_capabilities: "",
    simulation: false,
    provenance: "",
    license: "",
  });
  const load = () =>
    listRegistryReleases()
      .then(setReleases)
      .catch((e) => setError(e.message));
  useEffect(() => {
    load();
  }, []);
  const transition = async (id: string, state: string) => {
    try {
      await transitionRegistryRelease(id, { state, reason });
      setReason("");
      load();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const created = await createRegistryRelease({
        ...form,
        requested_capabilities: form.requested_capabilities
          .split(",")
          .map((x) => x.trim())
          .filter(Boolean),
        state: "quarantined",
      });
      setReleases((items) => [...items, created]);
    } catch (e) {
      setError((e as Error).message);
    }
  };
  const targets = [
    "verified",
    "approved",
    "installed",
    "active",
    "draining",
    "disabled",
    "revoked",
  ];
  return (
    <section aria-label="Registry">
      <div className="alert">
        The registry stores metadata and lifecycle only; it does not upload
        executables or execute plugins.
      </div>
      <div className="toolbar">
        <div>
          <h2>Registry</h2>
          <p className="muted">Release metadata and lifecycle state.</p>
        </div>
        <button className="button" onClick={load}>
          Refresh
        </button>
      </div>
      {me.role === "admin" && (
        <form
          className="composer"
          onSubmit={create}
          aria-label="Create registry release"
        >
          {(
            [
              "family",
              "kind",
              "version",
              "digest",
              "requested_capabilities",
              "provenance",
              "license",
            ] as const
          ).map((key) => (
            <label key={key}>
              {key.replace("_", " ")}
              <input
                required={key !== "provenance" && key !== "license"}
                value={form[key]}
                onChange={(e) => setForm({ ...form, [key]: e.target.value })}
              />
            </label>
          ))}
          <label>
            <input
              type="checkbox"
              checked={form.simulation}
              onChange={(e) =>
                setForm({ ...form, simulation: e.target.checked })
              }
            />{" "}
            Simulation
          </label>
          <button className="primary">Create quarantined release</button>
        </form>
      )}
      <label>
        Transition reason
        <input value={reason} onChange={(e) => setReason(e.target.value)} />
      </label>
      <div className="table">
        {releases.map((r) => (
          <article className="table-row" key={r.id}>
            <div>
              <strong>{r.family}</strong>
              <span className="muted">
                {r.kind} · {r.version}
              </span>
            </div>
            <span className="status-chip">{r.state}</span>
            {r.simulation && <span className="tag">SIMULATION</span>}
            <div className="chips">
              {r.requested_capabilities.map((c) => (
                <span key={c}>{c}</span>
              ))}
            </div>
            {me.role === "admin" && (
              <div className="toolbar">
                {targets.map((target) => (
                  <button
                    type="button"
                    className="button"
                    key={target}
                    onClick={() => transition(r.id, target)}
                  >
                    {target}
                  </button>
                ))}
              </div>
            )}
          </article>
        ))}
        {!releases.length && empty}
      </div>
    </section>
  );
}
function Activity({ setError }: { setError: (v: string) => void }) {
  const [events, setEvents] = useState<any[]>([]);
  const [receipts, setReceipts] = useState<any[]>([]);
  useEffect(() => {
    Promise.all([list<any>("/events"), list<any>("/receipts")])
      .then(([e, r]) => {
        setEvents(e);
        setReceipts(r);
      })
      .catch((e) => setError(e.message));
  }, []);
  return (
    <>
      <div className="toolbar">
        <h2>Activity and receipts</h2>
      </div>
      <div className="activity-grid">
        <article className="panel">
          <h3>Events</h3>
          {events.map((e, i) => (
            <div className="event" key={e.id || i}>
              <strong>{e.event_type || "Event"}</strong>
              <p className="muted">
                {e.created_at || "Recorded event"} ·{" "}
                {e.payload ? JSON.stringify(e.payload) : ""}
              </p>
            </div>
          ))}
          {!events.length && <p className="muted">No events returned.</p>}
        </article>
        <article className="panel">
          <h3>Receipts</h3>
          {receipts.map((r, i) => (
            <div className="receipt" key={r.id || i}>
              <strong>
                {r.kind || "Receipt"} · {r.outcome_code || "unknown outcome"}
              </strong>
              <p className="muted mono">
                {r.result ? JSON.stringify(r.result) : "No result"} ·{" "}
                {r.effect_id || r.id || "No effect"}
              </p>
            </div>
          ))}
          {!receipts.length && <p className="muted">No receipts returned.</p>}
        </article>
      </div>
    </>
  );
}
if (typeof document !== "undefined" && document.getElementById("root")) {
  createRoot(document.getElementById("root")!).render(<App />);
}
