import { useState } from "react";
import type { Gate } from "@/api";
import { Button } from "@/components/ui/button";

/**
 * Answer form for a parked agent question.
 *
 * The declared schema decides the control. This matters more than it sounds: an earlier
 * revision rendered every non-boolean field as a plain text input, so a question that asked for
 * `recipients` as an ARRAY — exactly what the mail connector needs — produced a bare string the
 * server refused with 422. The question was answerable by a script and not by a person.
 *
 * So: type-driven controls, and a declared type this form cannot build is refused visibly
 * rather than sent and rejected.
 */

const MAX_LIST_ITEMS = 64;

export type ParsedList =
  | { ok: true; value: unknown[] }
  | { ok: false; error: string };

export function parseList(raw: string, itemType?: string): ParsedList {
  const parts = raw
    .split(",")
    .map((p) => p.trim())
    .filter((p) => p !== "");
  if (parts.length > MAX_LIST_ITEMS) {
    return { ok: false, error: `at most ${MAX_LIST_ITEMS} entries` };
  }
  switch (itemType) {
    case "string":
      return { ok: true, value: parts };
    case "integer": {
      const out: number[] = [];
      for (const p of parts) {
        const n = Number(p);
        if (!Number.isInteger(n)) {
          return { ok: false, error: `"${p}" must be a whole number` };
        }
        // Past 2^53-1 a JS number silently becomes a DIFFERENT integer and the server would
        // faithfully record a value nobody typed. Refuse instead.
        if (!Number.isSafeInteger(n)) {
          return { ok: false, error: `"${p}" is too large to enter safely` };
        }
        out.push(n);
      }
      return { ok: true, value: out };
    }
    case "number": {
      const out: number[] = [];
      for (const p of parts) {
        const n = Number(p);
        if (!Number.isFinite(n)) {
          return { ok: false, error: `"${p}" must be a number` };
        }
        out.push(n);
      }
      return { ok: true, value: out };
    }
    case "boolean": {
      const out: boolean[] = [];
      for (const p of parts) {
        const v = p.toLowerCase();
        // Only the exact words. Accepting "yes"/"1" would silently record a typo as true.
        if (v !== "true" && v !== "false") {
          return { ok: false, error: `"${p}" must be true or false` };
        }
        out.push(v === "true");
      }
      return { ok: true, value: out };
    }
    default:
      return {
        ok: false,
        error: "this question asks for a list type this form cannot build",
      };
  }
}

export function GateForm({
  gate,
  busy,
  onSubmit,
  onError,
}: {
  gate: Gate;
  busy: boolean;
  onSubmit: (response: Record<string, unknown>) => void;
  onError: (message: string) => void;
}) {
  const properties = gate.input_schema.properties ?? {};
  const required = new Set(gate.input_schema.required ?? []);
  const [values, setValues] = useState<Record<string, string | boolean>>({});
  const [fieldError, setFieldError] = useState("");

  const set = (name: string, value: string | boolean) =>
    setValues((v) => ({ ...v, [name]: value }));

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    const response: Record<string, unknown> = {};
    for (const [name, schema] of Object.entries(properties)) {
      const value = values[name];

      if (schema.type === "array") {
        const parsed = parseList(String(value ?? ""), schema.items?.type);
        if (!parsed.ok) {
          setFieldError(`${name}: ${parsed.error}`);
          return;
        }
        if (required.has(name) && parsed.value.length === 0) {
          setFieldError(`${name} needs at least one entry.`);
          return;
        }
        if (parsed.value.length > 0) response[name] = parsed.value;
        continue;
      }

      const empty =
        schema.type === "boolean"
          ? value === undefined
          : String(value ?? "").trim() === "";
      if (required.has(name) && empty) {
        setFieldError(`${name} is required.`);
        return;
      }
      if (empty) continue;

      if (schema.type === "integer") {
        const n = Number(value);
        if (!Number.isInteger(n)) {
          setFieldError(`${name} must be a whole number.`);
          return;
        }
        response[name] = n;
      } else if (schema.type === "number") {
        response[name] = Number(value);
      } else if (schema.type === "boolean") {
        response[name] = Boolean(value);
      } else {
        response[name] = value;
      }
    }
    setFieldError("");
    onError("");
    onSubmit(response);
  };

  const entries = Object.entries(properties);
  if (!entries.length) {
    // A question with no declared fields cannot be answered meaningfully; saying so is better
    // than rendering an empty form whose submit button does nothing.
    return (
      <p className="text-[11px] text-[--color-danger]">
        This question declared no fields, so it has no answer the platform can record.
      </p>
    );
  }

  return (
    <form onSubmit={submit} className="space-y-3">
      {entries.map(([name, schema]) => {
        const label = name.replace(/_/g, " ");
        return (
          <label key={name} className="grid gap-1.5">
            <span className="text-[11px] font-medium capitalize text-[--color-ink-2]">
              {label}
              {required.has(name) ? (
                <span className="ml-1 text-[--color-danger]">*</span>
              ) : null}
            </span>

            {schema.type === "boolean" ? (
              <span className="flex items-center gap-2">
                <input
                  type="checkbox"
                  className="size-3.5 accent-[--color-accent]"
                  checked={Boolean(values[name])}
                  onChange={(e) => set(name, e.target.checked)}
                />
                <span className="text-[11px] text-[--color-ink-3]">
                  tick for yes
                </span>
              </span>
            ) : schema.type === "array" ? (
              <>
                <input
                  type="text"
                  placeholder="comma-separated"
                  className="w-full rounded-md border border-[--color-line-strong] bg-[--color-canvas] px-2.5 py-1.5 text-xs"
                  value={String(values[name] ?? "")}
                  onChange={(e) => set(name, e.target.value)}
                />
                <span className="text-[10px] text-[--color-ink-3]">
                  a list of {schema.items?.type ?? "values"}
                </span>
              </>
            ) : (
              <input
                type={schema.type === "string" ? "text" : "number"}
                step={schema.type === "integer" ? 1 : undefined}
                className="w-full rounded-md border border-[--color-line-strong] bg-[--color-canvas] px-2.5 py-1.5 text-xs"
                value={String(values[name] ?? "")}
                onChange={(e) => set(name, e.target.value)}
              />
            )}
          </label>
        );
      })}

      {fieldError ? (
        <p className="text-[11px] text-[--color-danger]">{fieldError}</p>
      ) : null}

      <Button type="submit" variant="accent" size="sm" disabled={busy}>
        {busy ? "Sending…" : "Send answer and resume"}
      </Button>
    </form>
  );
}
