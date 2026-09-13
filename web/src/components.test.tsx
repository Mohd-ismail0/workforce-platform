import { describe, expect, it, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { HandoffForms, Login } from "./main";
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
