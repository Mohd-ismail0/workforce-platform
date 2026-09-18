export type Role = "requester" | "approver" | "admin";
export type Me = { id: string; org_id: string; role: Role; name: string };
export type Project = { id: string; name: string; description: string };
export type Task = {
  id: string;
  title: string;
  description: string;
  status: string;
  owner_id: string;
  assignee_id?: string;
  project_id?: string;
  version: number;
  created_at: string;
};
export type Operation = {
  id?: string;
  business_key: string;
  integration: "inventory" | "mail" | "documents";
  action: "adjust" | "send" | "update";
  target_id: string;
  expected_version: number;
  payload: Record<string, unknown>;
};
export type RegistryRelease = {
  id: string;
  family: string;
  kind: string;
  version: string;
  digest: string;
  state: string;
  requested_capabilities: string[];
  granted_capabilities: string[];
  compatibility: unknown;
  simulation: boolean;
  provenance: unknown;
  license: string;
  created_by: string;
  version_num?: number;
};

export const listRegistryReleases = async () => {
  const body = await api<{ items?: RegistryRelease[] } | RegistryRelease[]>(
    "/registry/releases",
  );
  return Array.isArray(body) ? body : body.items || [];
};
export const createRegistryRelease = (body: Record<string, unknown>) =>
  api<RegistryRelease>("/registry/releases", {
    method: "POST",
    body: JSON.stringify(body),
  });
export const transitionRegistryRelease = (
  id: string,
  body: Record<string, unknown>,
) =>
  api<RegistryRelease>(`/registry/releases/${id}/transition`, {
    method: "POST",
    body: JSON.stringify(body),
  });

export type Proposal = {
  id: string;
  task_id: string;
  revision: number;
  digest: string;
  status: string;
  summary: string;
  operations: Operation[];
  created_at: string;
  failure_reason?: string;
};

/** One record a proposal would change, at the version the proposal was built against. */
export type Record_ = {
  id: string;
  integration: string;
  version: number;
  data: Record<string, unknown>;
};

export const listProposals = () => list<Proposal>("/proposals");
export const getProposal = (id: string) => api<Proposal>(`/proposals/${id}`);
export const listRecords = (integration: string) =>
  list<Record_>(`/integrations/${integration}/records`);
export const listTasks = () => list<Task>("/tasks");
export const listProjects = () => list<Project>("/projects");
export const listDecisions = () => list<Decision>("/decisions");
export const getTask = (id: string) => api<Task>(`/tasks/${id}`);
export const listAgents = () => list<Agent>("/agents");
export const createProject = (body: { name: string; description?: string }) =>
  api<Project>("/projects", { method: "POST", body: JSON.stringify(body) });
export const createTask = (body: {
  title: string;
  description?: string;
  project_id?: string;
  assignee_id?: string;
}) => api<Task>("/tasks", { method: "POST", body: JSON.stringify(body) });

/** An agent DEFINITION: a template plus the harness it runs on. Not a running process. */
export type Agent = {
  id: string;
  org_id: string;
  name: string;
  harness: string;
  owner_id: string;
  capabilities: string[] | null;
  created_at: string;
};

/**
 * Decide on a proposal.
 *
 * `kind` is endorse | approve | reject. The distinction is the whole authority model: an
 * endorser confirms "this is the work I asked for", an approver authorises the effect. They are
 * deliberately different people, and the server enforces that — a requester cannot approve
 * their own proposal.
 */
export type Decision = {
  id: string;
  task_id: string;
  proposal_id: string;
  revision: number;
  digest: string;
  kind: "endorse" | "approve";
  title: string;
  status: string;
};

export const decideProposal = (
  proposalId: string,
  kind: "endorse" | "approve" | "reject",
  body: { revision: number; digest: string; reason?: string },
) =>
  api<Proposal>(`/proposals/${proposalId}/${kind}`, {
    method: "POST",
    body: JSON.stringify(body),
  });
export type HandoffOffer = {
  id: string;
  task_id: string;
  recipient_id: string;
  role: string;
  summary: string;
  state: string;
  created_by: string;
  created_at: string;
  /** When an unanswered offer stops being actionable. Empty when none. */
  expires_at: string;
  /** Set once the offer stops being open (accepted/declined/expired/cancelled). */
  resolved_at: string;
  /** The current note: clarification question, decline reason or withdrawal reason. */
  reason: string;
  /** Accountability-transfer depth. */
  hop_depth: number;
};

export type GateKind = "clarification" | "selection" | "missing_information";
export type GateProperty = {
  type: "string" | "integer" | "number" | "boolean" | "array";
  // A gate may ask for a LIST. A real model asked for `recipients` as an array of
  // strings — exactly what the mail connector requires — so the declared item type is
  // carried through rather than collapsed into a bare string.
  items?: { type?: "string" | "integer" | "number" | "boolean" };
};
export type Gate = {
  id: string;
  org_id: string;
  task_id: string;
  run_id: string;
  kind: GateKind;
  prompt: string;
  input_schema: {
    properties?: Record<string, GateProperty>;
    required?: string[];
    [key: string]: unknown;
  };
  revision: number;
  status: string;
  respondent_id: string;
  response?: Record<string, unknown> | null;
  responded_by?: string | null;
  responded_at?: string | null;
  expires_at?: string | null;
  created_by: string;
  created_at: string;
  version: number;
};

export const listGates = () => list<Gate>("/gates");
export const getGate = (id: string) => api<Gate>(`/gates/${id}`);
export const respondToGate = (
  id: string,
  body: { revision: number; response: Record<string, unknown> },
) =>
  api<Gate>(`/gates/${id}/respond`, {
    method: "POST",
    body: JSON.stringify(body),
  });

export type AgentRun = {
  id: string;
  org_id: string;
  task_id: string;
  agent_id: string;
  harness: string;
  harness_release_id: string;
  runner_id: string;
  intent: string;
  status: "queued" | "running" | "waiting" | "succeeded" | "failed" | "cancelled";
  proposal_id?: string | null;
  result_summary?: string | null;
  failure_reason?: string | null;
  notes: string[];
  created_by: string;
  version: number;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
};

export const listHandoffs = () => list<HandoffOffer>("/handoffs");
export const listRuns = () => list<AgentRun>("/runs");
export const getRun = (id: string) => api<AgentRun>(`/runs/${id}`);
export const startRun = (
  taskId: string,
  body: { agent_id: string; intent: string },
) =>
  api<AgentRun>(`/tasks/${taskId}/runs`, {
    method: "POST",
    body: JSON.stringify(body),
  });

const tokenKey = "workforce.local.token";
export const getToken = () => sessionStorage.getItem(tokenKey) || "";
export const setToken = (token: string) =>
  token
    ? sessionStorage.setItem(tokenKey, token)
    : sessionStorage.removeItem(tokenKey);

// ---------------------------------------------------------------------------
// Browser session (BFF) support.
//
// In OIDC mode the browser holds NO token: the credential is an HttpOnly cookie the page
// cannot read, and every request is authorised server-side from the database. Nothing here
// ever stores an access token, refresh token or client secret -- doing so would move the
// credential back into reach of any script on the page.
//
// The CSRF token is the one value that must be readable by the client, because it has to be
// echoed back in a header; that is what proves a state-changing request was deliberate rather
// than something the browser attached a cookie to on its own.
// ---------------------------------------------------------------------------

export type SessionState = {
  authenticated: boolean;
  identity?: Me;
  csrf_token?: string;
  expires_at?: string;
};

let csrfToken = "";
// browserSessionMode records that the deployment uses the BFF cookie flow. It is deliberately
// sticky for the life of the page: once the server has answered /auth/session, the local
// development token must not be raced against the cookie (see api()).
let browserSessionMode = false;

/** Ask the BFF who we are. Returns null ONLY when the browser flow is not configured. */
export async function getSession(): Promise<SessionState | null> {
  const response = await fetch("/auth/session", {
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  // 401 is a definite statement: the endpoint refused because the browser flow is not
  // configured (local mode). That is different from "configured but nobody is signed in",
  // which is 200 with authenticated:false.
  if (response.status === 401) {
    browserSessionMode = false;
    return null;
  }
  // Any other failure is NOT "not configured" and must not be reported as one. Treating an
  // unreachable API as local mode would show a development token form in a real deployment,
  // inviting a shared secret as a credential.
  if (!response.ok) {
    throw new Error(`session endpoint failed (${response.status})`);
  }
  const body = (await response.json().catch(() => null)) as SessionState | null;
  if (!body) throw new Error("session endpoint returned a non-JSON response");

  browserSessionMode = true;
  csrfToken = body.csrf_token || "";
  if (body.authenticated) {
    // Clear any stale development token. The server checks a bearer token BEFORE the session
    // cookie, so a leftover dev token would make every request fail verification even though
    // a valid session cookie was attached.
    setToken("");
  }
  return body;
}

/** Absolute URL that starts the interactive login. A full navigation, not a fetch. */
export const loginUrl = (returnTo = "/") =>
  `/auth/login?return_to=${encodeURIComponent(returnTo)}`;

export async function logout(): Promise<string> {
  // Local state is cleared even when the server call fails. Leaving the CSRF token and any
  // development token in place after a failed logout would let the next request look
  // authenticated on this page while the server still holds a live session.
  let url = "";
  try {
    const response = await fetch("/auth/logout", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...(csrfToken ? { "X-CSRF-Token": csrfToken } : {}),
      },
      body: JSON.stringify({}),
    });
    const body = await response.json().catch(() => null);
    url = body?.logout_url || "";
  } finally {
    csrfToken = "";
    setToken("");
  }
  return url;
}

export async function api<T>(path: string, init: RequestInit = {}) {
  // A bearer token is used ONLY when one was explicitly supplied for local development.
  //
  // This matters more than it looks. The server checks a bearer token BEFORE the session
  // cookie, so a stale development token left in sessionStorage would be sent on every
  // request, fail verification against the issuer, and return 401 — even though a perfectly
  // good session cookie was attached. The browser would be signed in and still unable to call
  // anything. So once the browser flow is in use, the local token is cleared rather than
  // raced against the cookie.
  const localToken = browserSessionMode ? "" : getToken();
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...(localToken ? { Authorization: `Bearer ${localToken}` } : {}),
      // Sent only when a session cookie is in play; an explicit bearer token needs no CSRF
      // proof because the browser never attaches one by itself.
      ...(!localToken && csrfToken ? { "X-CSRF-Token": csrfToken } : {}),
      ...(init.headers || {}),
    },
  });
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    const error = new Error(
      body?.error?.message || `Request failed (${response.status})`,
    ) as Error & { status?: number };
    error.status = response.status;
    throw error;
  }
  return body as T;
}
export const list = async <T>(path: string) =>
  (await api<{ items?: T[] }>(path)).items || [];
export const createHandoff = (
  taskId: string,
  payload: { recipient_id: string; role: string; summary: string },
) =>
  api<HandoffOffer>(`/tasks/${taskId}/handoffs`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
export const acceptHandoff = (handoffId: string) =>
  api<HandoffOffer>(`/handoffs/${handoffId}/accept`, {
    method: "POST",
    body: JSON.stringify({}),
  });
/** The recipient refuses the work, saying why. Not the same as cancelling. */
export const declineHandoff = (handoffId: string, reason: string) =>
  api<{ status: string }>(`/handoffs/${handoffId}/decline`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
/** The recipient asks a question instead of guessing; the offer stays open. */
export const clarifyHandoff = (handoffId: string, question: string) =>
  api<{ status: string }>(`/handoffs/${handoffId}/clarify`, {
    method: "POST",
    body: JSON.stringify({ reason: question }),
  });
/** The creator withdraws their own offer. */
export const cancelHandoff = (handoffId: string, reason: string) =>
  api<{ status: string }>(`/handoffs/${handoffId}/cancel`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });

/** A directory entry: enough to name someone in a structure view. */
export type Person = {
  id: string;
  name: string;
  role: Role;
  active: boolean;
};
/** A node in the organization structure. A holderless position is a vacancy. */
export type Position = {
  id: string;
  name: string;
  parent_id: string;
  version: number;
  created_at: string;
};
/**
 * An effective-dated fact about a person. `valid_from`/`valid_to` are a
 * half-open period; `valid_to` is empty while it is still in effect.
 */
export type Relationship = {
  id: string;
  subject_id: string;
  kind: "reports_to" | "member_of" | "covers_for";
  object_id: string;
  valid_from: string;
  valid_to: string;
  created_by: string;
  created_at: string;
};

export const listPeople = () => list<Person>("/people");
/**
 * One piece of work as it relates to the signed-in person. `relevance` is WHY it
 * is on the board — accountable, executing, or an offer not yet answered — which
 * is what makes the list an account of obligations rather than a pile of tasks.
 */
export type WorkBoardItem = {
  task_id: string;
  title: string;
  status: string;
  version: number;
  created_at: string;
  relevance: "accountable" | "executing" | "handoff_offered";
  blocker_task_id: string;
  blocker_title: string;
  blocker_owner_id: string;
};
/** The caller's own board, projected server-side from the authenticated identity. */
export const listWorkBoard = () => list<WorkBoardItem>("/work/board");
export const listPositions = () => list<Position>("/positions");
export const createPosition = (body: { name: string; parent_id?: string }) =>
  api<Position>("/positions", { method: "POST", body: JSON.stringify(body) });
/** Structure is a question with a time in it; omit `asOf` for "now". */
export const listRelationships = (asOf?: string) =>
  list<Relationship>(
    asOf ? `/relationships?as_of=${encodeURIComponent(asOf)}` : "/relationships",
  );
export const createRelationship = (body: {
  subject_id: string;
  kind: string;
  object_id: string;
  from?: string;
  to?: string;
}) =>
  api<Relationship>("/relationships", { method: "POST", body: JSON.stringify(body) });
export const endRelationship = (id: string, at?: string) =>
  api<{ status: string }>(`/relationships/${id}/end`, {
    method: "POST",
    body: JSON.stringify(at ? { at } : {}),
  });
export const canApprove = (role?: Role) =>
  role === "approver" || role === "admin";
export function digestFor(proposal: {
  revision: number;
  summary: string;
  operations: Array<{
    integration: string;
    action: string;
    target_id: string;
    expected_version: number;
    payload: Record<string, unknown>;
  }>;
}) {
  return `${proposal.revision}:${proposal.summary}:${JSON.stringify(proposal.operations)}`;
}
export function operationPayload(
  integration: Operation["integration"],
  values: Record<string, string>,
): Record<string, unknown> {
  if (integration === "inventory") return { delta: Number(values.delta) };
  if (integration === "mail")
    return {
      recipients: values.recipients
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean),
      subject: values.subject,
      body: values.body,
      bcc: values.bcc
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean),
      attachments: values.attachments
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean),
    };
  return Object.fromEntries(
    Object.entries({ title: values.title, content: values.content }).filter(
      ([, value]) => value,
    ),
  );
}
