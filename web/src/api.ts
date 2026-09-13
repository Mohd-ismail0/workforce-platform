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
  const body = await api<{ items?: RegistryRelease[] } | RegistryRelease[]>("/registry/releases");
  return Array.isArray(body) ? body : body.items || [];
};
export const createRegistryRelease = (body: Record<string, unknown>) =>
  api<RegistryRelease>("/registry/releases", { method: "POST", body: JSON.stringify(body) });
export const transitionRegistryRelease = (id: string, body: Record<string, unknown>) =>
  api<RegistryRelease>(`/registry/releases/${id}/transition`, { method: "POST", body: JSON.stringify(body) });

export type Proposal = {
  id: string;
  task_id: string;
  revision: number;
  digest: string;
  status: string;
  summary: string;
  operations: Operation[];
  created_at: string;
};
export type HandoffOffer = {
  id: string;
  task_id: string;
  recipient_id: string;
  role: string;
  summary: string;
  state: string;
  created_by: string;
  created_at: string;
};

export const listHandoffs = () => list<HandoffOffer>("/handoffs");

const tokenKey = "workforce.local.token";
export const getToken = () => sessionStorage.getItem(tokenKey) || "";
export const setToken = (token: string) =>
  token
    ? sessionStorage.setItem(tokenKey, token)
    : sessionStorage.removeItem(tokenKey);
export async function api<T>(path: string, init: RequestInit = {}) {
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(getToken() ? { Authorization: `Bearer ${getToken()}` } : {}),
      ...(init.headers || {}),
    },
  });
  const body = await response.json().catch(() => null);
  if (!response.ok)
    throw new Error(
      body?.error?.message || `Request failed (${response.status})`,
    );
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
