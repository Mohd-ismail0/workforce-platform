import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
// These technical screens moved out of the old monolithic main.tsx into the admin console.
// The imports follow the code rather than the other way round.
import {
  Gates,
  HandoffForms,
  Login,
  Registry,
  Runs,
  TaskDetail,
} from "@/features/admin/console";
import { setToken } from "@/api";
import React from "react";

const task = {
  id: "task-1",
  title: "Task",
  description: "",
  status: "ready",
  owner_id: "o",
  version: 1,
  created_at: "",
};

describe("handoff UI", () => {
  beforeEach(() => {
    cleanup();
    vi.restoreAllMocks();
    setToken("");
  });
  it("posts offer fields, displays returned ID, and accepts known ID", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockImplementation(async (input, init) => {
        const url = String(input);
        if (url.endsWith("/handoffs"))
          return new Response(JSON.stringify({ id: "offer-7" }), {
            status: 200,
          });
        return new Response(JSON.stringify({ id: "offer-7" }), { status: 200 });
      });
    render(
      <HandoffForms task={task} setError={vi.fn()} onAccepted={vi.fn()} />,
    );
    fireEvent.change(screen.getByLabelText("Recipient ID"), {
      target: { value: "person-2" },
    });
    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: "assignee" },
    });
    fireEvent.change(screen.getByLabelText("Summary"), {
      target: { value: "Please take this" },
    });
    fireEvent.submit(
      screen.getByRole("form", { name: "Create handoff offer" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain("offer-7"),
    );
    expect(fetchMock.mock.calls[0][1]?.body).toBe(
      JSON.stringify({
        recipient_id: "person-2",
        role: "assignee",
        summary: "Please take this",
      }),
    );
    expect(
      screen.queryByRole("form", { name: "Accept handoff offer" }),
    ).toBeNull();
  });
  it("shows real API errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: { message: "not allowed" } }), {
        status: 403,
      }),
    );
    const setError = vi.fn();
    render(
      <HandoffForms task={task} setError={setError} onAccepted={vi.fn()} />,
    );
    fireEvent.change(screen.getByLabelText("Recipient ID"), {
      target: { value: "x" },
    });
    fireEvent.change(screen.getByLabelText("Summary"), {
      target: { value: "s" },
    });
    fireEvent.submit(
      screen.getByRole("form", { name: "Create handoff offer" }),
    );
    await waitFor(() => expect(setError).toHaveBeenCalledWith("not allowed"));
  });
});

describe("registry and proposal controls", () => {
  beforeEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });
  const release = {
    id: "rel-1",
    family: "payments",
    kind: "connector",
    version: "1.2.3",
    digest: "sha256:x",
    state: "quarantined",
    requested_capabilities: ["read"],
    granted_capabilities: [],
    compatibility: {},
    simulation: true,
    provenance: "test",
    license: "MIT",
    created_by: "person-1",
  };
  it("posts business_key in a proposal", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockImplementation(async (input) => {
        if (String(input).includes("/records"))
          return new Response(
            JSON.stringify({ items: [{ id: "target-1", version: 4 }] }),
            { status: 200 },
          );
        return new Response(JSON.stringify({}), { status: 200 });
      });
    render(
      <TaskDetail
        task={task}
        close={vi.fn()}
        refresh={vi.fn()}
        setError={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText("Build proposal"));
    fireEvent.change(
      screen.getAllByLabelText("Summary")[
        screen.getAllByLabelText("Summary").length - 1
      ],
      { target: { value: "Do it" } },
    );
    fireEvent.change(
      screen.getByRole("textbox", { name: "Business operation key" }),
      { target: { value: "invoice-42" } },
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Target ID" }), {
      target: { value: "target-1" },
    });
    fireEvent.click(screen.getByText("Capture target version"));
    await waitFor(() =>
      expect(
        screen
          .getAllByRole("status")
          .some((x) => x.textContent?.includes("Captured version: v4")),
      ).toBe(true),
    );
    fireEvent.click(screen.getByText("Create proposal"));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(([, init]) =>
          String(init?.body).includes("business_key"),
        ),
      ).toBe(true),
    );
  });
  it("renders mocked registry releases", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(JSON.stringify({ items: [release] }), { status: 200 }),
    );
    render(
      <Registry
        me={{ id: "p", org_id: "o", role: "requester", name: "Requester" }}
        setError={vi.fn()}
      />,
    );
    await waitFor(() => expect(screen.getByText("payments")).toBeTruthy());
    expect(screen.getByText("SIMULATION")).toBeTruthy();
  });
  it("only shows lifecycle transition buttons to admins", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(JSON.stringify({ items: [release] }), { status: 200 }),
    );
    const requester = {
      id: "p",
      org_id: "o",
      role: "requester" as const,
      name: "Requester",
    };
    render(<Registry me={requester} setError={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("payments")).toBeTruthy());
    expect(screen.queryByRole("button", { name: "active" })).toBeNull();
    cleanup();
    render(
      <Registry me={{ ...requester, role: "admin" }} setError={vi.fn()} />,
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "active" })).toBeTruthy(),
    );
  });
});

describe("agent runs", () => {
  beforeEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });
  const run = {
    id: "run-1",
    org_id: "o",
    task_id: "task-1",
    agent_id: "agent-1",
    harness: "sim",
    harness_release_id: "rel",
    runner_id: "simulator",
    intent: "Prepare proposal",
    status: "queued",
    proposal_id: null,
    result_summary: null,
    failure_reason: null,
    notes: [],
    created_by: "p",
    version: 1,
    created_at: "",
  };
  it("posts agent_id and intent and renders returned status", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) =>
      String(input).endsWith("/gates")
        ? new Response(JSON.stringify({ items: [] }), { status: 200 })
        : new Response(JSON.stringify({ ...run, status: "running" }), { status: 201 }),
    );
    render(
      <TaskDetail
        task={task}
        close={vi.fn()}
        refresh={vi.fn()}
        setError={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByLabelText("Agent ID"), {
      target: { value: "agent-1" },
    });
    fireEvent.change(screen.getByLabelText("Intent"), {
      target: { value: "Prepare proposal" },
    });
    fireEvent.submit(screen.getByRole("form", { name: "Start run" }));
    await waitFor(() =>
      expect(
        screen
          .getAllByRole("status")
          .some((x) => x.textContent?.includes("running")),
      ).toBe(true),
    );
    expect(
      fetchMock.mock.calls.find(([, init]) => init?.method === "POST")?.[1]?.body,
    ).toBe(
      JSON.stringify({ agent_id: "agent-1", intent: "Prepare proposal" }),
    );
  });
  it("renders the backend 409 error verbatim", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).endsWith("/gates") ? new Response(JSON.stringify({ items: [] }), { status: 200 }) : new Response(JSON.stringify({ error: { message: "No active harness release exists in the registry" } }), { status: 409 }));
    render(
      <TaskDetail
        task={task}
        close={vi.fn()}
        refresh={vi.fn()}
        setError={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByLabelText("Agent ID"), {
      target: { value: "agent-1" },
    });
    fireEvent.change(screen.getByLabelText("Intent"), {
      target: { value: "x" },
    });
    fireEvent.submit(screen.getByRole("form", { name: "Start run" }));
    await waitFor(() =>
      expect(
        screen
          .getAllByRole("status")
          .some((x) =>
            x.textContent?.includes(
              "No active harness release exists in the registry",
            ),
          ),
      ).toBe(true),
    );
  });
  it("renders a failed run failure reason", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [
            {
              ...run,
              status: "failed",
              failure_reason: "Harness release blocked",
            },
          ],
        }),
        { status: 200 },
      ),
    );
    render(<Runs setError={vi.fn()} />);
    await waitFor(() =>
      expect(screen.getByText("Harness release blocked")).toBeTruthy(),
    );
  });
});
describe("gates", () => {
  const gate = {
    id: "gate-1", org_id: "o", task_id: "task-1", run_id: "run-1",
    kind: "clarification", prompt: "Which values?", input_schema: {
      properties: { answer: { type: "string" }, count: { type: "integer" }, ok: { type: "boolean" } },
      required: ["answer", "count"],
    }, revision: 3, status: "pending", respondent_id: "p", created_by: "a", created_at: "2026-01-01T00:00:00Z", version: 1,
  };
  beforeEach(() => { cleanup(); vi.restoreAllMocks(); });
  it("renders prompt and schema fields", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ items: [gate] }), { status: 200 }));
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("Which values?")).toBeTruthy());
    expect(screen.getByLabelText("answer *")).toBeTruthy();
    expect(screen.getByLabelText("count *")).toBeTruthy();
    expect(screen.getByLabelText("ok")).toBeTruthy();
  });
  it("posts the current revision and serialized response", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => String(input).endsWith("/gates") ? new Response(JSON.stringify({ items: [gate] }), { status: 200 }) : new Response(JSON.stringify({ ...gate, response: { answer: "yes", count: 2, ok: true } }), { status: 200 }));
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => screen.getByText("Which values?"));
    fireEvent.change(screen.getByLabelText("answer *"), { target: { value: "yes" } });
    fireEvent.change(screen.getByLabelText("count *"), { target: { value: "2" } });
    fireEvent.click(screen.getByLabelText("ok"));
    fireEvent.submit(screen.getByRole("form", { name: "Answer gate gate-1" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => String(init?.body) === JSON.stringify({ revision: 3, response: { answer: "yes", count: 2, ok: true } }))).toBe(true));
  });
  it("refreshes and shows a 409 message", async () => {
    let gets = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => { if (String(input).endsWith("/gates")) { gets++; return new Response(JSON.stringify({ items: [gate] }), { status: 200 }); } return new Response(JSON.stringify({ error: { message: "Gate changed elsewhere" } }), { status: 409 }); });
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => screen.getByText("Which values?"));
    fireEvent.change(screen.getByLabelText("answer *"), { target: { value: "yes" } });
    fireEvent.change(screen.getByLabelText("count *"), { target: { value: "2" } });
    fireEvent.submit(screen.getByRole("form", { name: "Answer gate gate-1" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("Gate changed elsewhere"));
    expect(gets).toBe(2);
  });
  it("rejects non-integer values without posting", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ items: [gate] }), { status: 200 }));
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => screen.getByText("Which values?"));
    fireEvent.change(screen.getByLabelText("answer *"), { target: { value: "yes" } });
    fireEvent.change(screen.getByLabelText("count *"), { target: { value: "1.5" } });
    fireEvent.submit(screen.getByRole("form", { name: "Answer gate gate-1" }));
    expect(screen.getByRole("status").textContent).toContain("whole number");
    expect(fetchMock.mock.calls.every(([, init]) => !init?.method || init.method !== "POST")).toBe(true);
  });

  // An array field must be answerable by a PERSON. The backend accepts scalar arrays
  // because a real model asked for `recipients` as an array — exactly what the mail
  // connector requires — but the form previously turned that answer into a bare string
  // the server refuses with 422. A script passing did not mean a human could answer.
  const arrayGate = {
    id: "gate-arr", org_id: "o", task_id: "task-9", run_id: "run-9",
    kind: "missing_information", prompt: "Who should this go to?",
    input_schema: {
      properties: { recipients: { type: "array", items: { type: "string" } } },
      required: ["recipients"],
    },
    revision: 1, status: "pending", respondent_id: "p", created_by: "a",
    created_at: "2026-01-01T00:00:00Z", version: 1,
  };

  it("posts a LIST answer for a declared array field", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      String(input).endsWith("/gates")
        ? new Response(JSON.stringify({ items: [arrayGate] }), { status: 200 })
        : new Response(JSON.stringify({ ...arrayGate, response: { recipients: ["a@x.test", "b@x.test"] } }), { status: 200 }));
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => screen.getByText("Who should this go to?"));
    fireEvent.change(screen.getByLabelText("recipients *"), { target: { value: "a@x.test, b@x.test" } });
    fireEvent.submit(screen.getByRole("form", { name: "Answer gate gate-arr" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) =>
      String(init?.body) === JSON.stringify({ revision: 1, response: { recipients: ["a@x.test", "b@x.test"] } }))).toBe(true));
  });

  // Numeric and boolean item types must round-trip as numbers/booleans, not strings:
  // the server validates per declared type, so a stringified list would be refused.
  it("posts typed entries for integer and boolean array fields", async () => {
    const typedGate = {
      ...arrayGate,
      id: "gate-typed",
      prompt: "Which values?",
      input_schema: {
        properties: {
          counts: { type: "array", items: { type: "integer" } },
          flags: { type: "array", items: { type: "boolean" } },
        },
        required: ["counts", "flags"],
      },
    };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      String(input).endsWith("/gates")
        ? new Response(JSON.stringify({ items: [typedGate] }), { status: 200 })
        : new Response(JSON.stringify(typedGate), { status: 200 }));
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => screen.getByText("Which values?"));
    fireEvent.change(screen.getByLabelText("counts *"), { target: { value: "1, 2, 3" } });
    fireEvent.change(screen.getByLabelText("flags *"), { target: { value: "true, false" } });
    fireEvent.submit(screen.getByRole("form", { name: "Answer gate gate-typed" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) =>
      String(init?.body) === JSON.stringify({
        revision: 1, response: { counts: [1, 2, 3], flags: [true, false] },
      }))).toBe(true));
  });

  // A large integer must be refused, not silently stored as a different number.
  it("refuses an integer beyond the safe range without posting", async () => {
    const bigGate = {
      ...arrayGate,
      id: "gate-big",
      input_schema: {
        properties: { ids: { type: "array", items: { type: "integer" } } },
        required: ["ids"],
      },
    };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ items: [bigGate] }), { status: 200 }));
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => screen.getByText("Who should this go to?"));
    fireEvent.change(screen.getByLabelText("ids *"), { target: { value: "9007199254740993" } });
    fireEvent.submit(screen.getByRole("form", { name: "Answer gate gate-big" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("too large"));
    expect(fetchMock.mock.calls.every(([, init]) => !init?.method || init.method !== "POST")).toBe(true);
  });

  it("refuses an unsupported array item type without posting", async () => {
    const unsupported = {
      ...arrayGate,
      id: "gate-bad",
      input_schema: { properties: { things: { type: "array", items: { type: "object" } } }, required: ["things"] },
    };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ items: [unsupported] }), { status: 200 }));
    render(<Gates setError={vi.fn()} />);
    await waitFor(() => screen.getByText("Who should this go to?"));
    fireEvent.change(screen.getByLabelText("things *"), { target: { value: "something" } });
    fireEvent.submit(screen.getByRole("form", { name: "Answer gate gate-bad" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("cannot build"));
    expect(fetchMock.mock.calls.every(([, init]) => !init?.method || init.method !== "POST")).toBe(true);
  });
});


describe("login", () => {
  it("does not call network while typing", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    render(<Login token="" setTok={vi.fn()} error="" />);
    fireEvent.change(screen.getByPlaceholderText("Bearer token"), {
      target: { value: "abc" },
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
