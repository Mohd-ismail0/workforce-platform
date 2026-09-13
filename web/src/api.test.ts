import { describe, it, expect } from "vitest";
import { canApprove, digestFor } from "./api";
describe("API policy seams", () => {
  it("only approvers and admins can approve", () => {
    expect(canApprove("requester")).toBe(false);
    expect(canApprove("approver")).toBe(true);
    expect(canApprove("admin")).toBe(true);
  });
  it("keeps proposal digest stable for a frozen revision", () => {
    const p = {
      revision: 2,
      summary: "Ship",
      operations: [
        {
          integration: "mail",
          action: "send",
          target_id: "x",
          expected_version: 1,
          payload: { bcc: [] },
        },
      ],
    };
    expect(digestFor(p)).toBe(
      '2:Ship:[{"integration":"mail","action":"send","target_id":"x","expected_version":1,"payload":{"bcc":[]}}]',
    );
  });
});
