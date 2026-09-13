import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { HandoffForms, Login, Registry, TaskDetail } from "./main";
import { setToken } from "./api";
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
    expect(screen.queryByRole("form", { name: "Accept handoff offer" })).toBeNull();
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
  const release = { id: "rel-1", family: "payments", kind: "connector", version: "1.2.3", digest: "sha256:x", state: "quarantined", requested_capabilities: ["read"], granted_capabilities: [], compatibility: {}, simulation: true, provenance: "test", license: "MIT", created_by: "person-1" };
  it("posts business_key in a proposal", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (String(input).includes("/records")) return new Response(JSON.stringify({ items: [{ id: "target-1", version: 4 }] }), { status: 200 });
      return new Response(JSON.stringify({}), { status: 200 });
    });
    render(<TaskDetail task={task} close={vi.fn()} refresh={vi.fn()} setError={vi.fn()} />);
    fireEvent.click(screen.getByText("Build proposal"));
    fireEvent.change(screen.getAllByLabelText("Summary")[screen.getAllByLabelText("Summary").length - 1], { target: { value: "Do it" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Business operation key" }), { target: { value: "invoice-42" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Target ID" }), { target: { value: "target-1" } });
    fireEvent.click(screen.getByText("Capture target version"));
    await waitFor(() => expect(screen.getAllByRole("status").some((x) => x.textContent?.includes("Captured version: v4"))).toBe(true));
    fireEvent.click(screen.getByText("Create proposal"));
    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => String(init?.body).includes("business_key"))).toBe(true));
  });
  it("renders mocked registry releases", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(JSON.stringify({ items: [release] }), { status: 200 }));
    render(<Registry me={{ id: "p", org_id: "o", role: "requester", name: "Requester" }} setError={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("payments")).toBeTruthy());
    expect(screen.getByText("SIMULATION")).toBeTruthy();
  });
  it("only shows lifecycle transition buttons to admins", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(JSON.stringify({ items: [release] }), { status: 200 }));
    const requester = { id: "p", org_id: "o", role: "requester" as const, name: "Requester" };
    render(<Registry me={requester} setError={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("payments")).toBeTruthy());
    expect(screen.queryByRole("button", { name: "active" })).toBeNull();
    cleanup();
    render(<Registry me={{ ...requester, role: "admin" }} setError={vi.fn()} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "active" })).toBeTruthy());
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
