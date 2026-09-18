import { FileText, Mail, Package } from "lucide-react";
import type { Operation, Record_ } from "@/api";
import { Badge } from "@/components/ui/badge";

/**
 * Domain previews.
 *
 * This is the part that makes the product different from a JSON viewer: a person must see the
 * change in the vocabulary of the system it affects — an envelope with its recipients and
 * attachments, a stock movement with units, a document diff — and approve THAT, not a payload.
 *
 * Two rules hold across every renderer:
 *
 *  1. The host decides what is material and shows it. A preview is allowed to be pretty; it is
 *     not allowed to omit a recipient, a BCC, an attachment or a quantity. If a renderer could
 *     hide a field, "what you see is what you sign" would be false.
 *  2. Missing data is shown as missing. These render against simulator records, and a preview
 *     that quietly invented a plausible subject line would be worse than one that says it does
 *     not have the record yet.
 */

const ICONS: Record<string, typeof Mail> = {
  mail: Mail,
  inventory: Package,
  documents: FileText,
};

export function integrationIcon(integration: string) {
  return ICONS[integration] ?? FileText;
}

/** The per-operation action, in words a person uses rather than the connector's verb. */
const ACTION_LABEL: Record<string, string> = {
  send: "Send",
  adjust: "Adjust",
  update: "Update",
};

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[92px_1fr] items-start gap-3 border-b border-[--color-line] py-2 last:border-b-0">
      <span className="eyebrow pt-0.5">{label}</span>
      <div className="min-w-0 text-[12px]">{children}</div>
    </div>
  );
}

function Recipients({ list, tone }: { list: string[]; tone?: "bcc" }) {
  if (!list.length) {
    return <span className="text-[--color-ink-3]">None</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {list.map((r) => (
        <span
          key={r}
          className={
            tone === "bcc"
              ? "rounded border border-[--color-pending]/40 bg-[--color-pending-soft] px-1.5 py-0.5 font-mono text-[11px] text-[--color-pending]"
              : "rounded border border-[--color-line-strong] px-1.5 py-0.5 font-mono text-[11px]"
          }
        >
          {r}
        </span>
      ))}
    </div>
  );
}

function MailPreview({
  payload,
  target,
}: {
  payload: Record<string, unknown>;
  target?: Record_;
}) {
  const recipients = (payload.recipients as string[]) ?? [];
  const bcc = (payload.bcc as string[]) ?? [];
  const attachments = (payload.attachments as string[]) ?? [];
  const subject = (payload.subject as string) ?? "";
  const body = (payload.body as string) ?? "";

  return (
    <div className="rounded-md border border-[--color-line] bg-[--color-canvas]">
      <div className="px-3 py-2">
        <Row label="To">
          <Recipients list={recipients} />
        </Row>
        {/* BCC is shown as prominently as To, and coloured. Hiding it would be the exact
            failure an approval surface must not have. */}
        <Row label="BCC">
          <Recipients list={bcc} tone="bcc" />
        </Row>
        <Row label="Subject">
          {subject ? (
            subject
          ) : (
            <span className="text-[--color-danger]">subject is empty</span>
          )}
        </Row>
        <Row label="Attachments">
          {attachments.length ? (
            <div className="flex flex-wrap gap-1">
              {attachments.map((a) => (
                <span
                  key={a}
                  className="rounded border border-[--color-line-strong] px-1.5 py-0.5 font-mono text-[11px]"
                >
                  {a}
                </span>
              ))}
            </div>
          ) : (
            <span className="text-[--color-ink-3]">None</span>
          )}
        </Row>
        {target ? (
          <Row label="Replies to">
            <span className="font-mono text-[11px] text-[--color-ink-2]">
              {target.id} · version {target.version}
            </span>
          </Row>
        ) : null}
      </div>
      {body ? (
        <div className="border-t border-[--color-line] px-3 py-2.5">
          <p className="whitespace-pre-wrap text-[12px] leading-relaxed text-[--color-ink-2]">
            {body}
          </p>
        </div>
      ) : null}
    </div>
  );
}

function InventoryPreview({
  payload,
  target,
  expectedVersion,
}: {
  payload: Record<string, unknown>;
  target?: Record_;
  expectedVersion?: number;
}) {
  const delta = typeof payload.delta === "number" ? payload.delta : undefined;
  const current =
    target && typeof target.data?.quantity === "number"
      ? (target.data.quantity as number)
      : undefined;
  const after =
    current !== undefined && delta !== undefined ? current + delta : undefined;

  /**
   * A stale record must NOT be presented as the proposal's "before".
   *
   * The proposal was prepared against `expectedVersion`. If the record has moved on since,
   * then the number in front of the approver is a different state than the change was designed
   * for — showing it inside a before/after diff would assert the platform applied a change to
   * state it never saw. That is precisely the "what you see is what you sign" failure, so the
   * diff is withheld and the staleness is stated instead.
   */
  const stale =
    target !== undefined &&
    expectedVersion !== undefined &&
    target.version !== expectedVersion;

  return (
    <div className="rounded-md border border-[--color-line] bg-[--color-canvas] px-3 py-2">
      <Row label="Record">
        <span className="font-mono text-[11px]">
          {target?.id ?? "—"}
          {target ? ` · version ${target.version}` : ""}
        </span>
      </Row>

      {stale ? (
        <>
          <Row label="State">
            <span className="flex items-center gap-2">
              <Badge tone="pending">changed since prepared</Badge>
              <span className="text-[11px] text-[--color-pending]">
                prepared against v{expectedVersion}, now v{target?.version}
              </span>
            </span>
          </Row>
          <Row label="Change">
            <span className="text-[11px] text-[--color-ink-2]">
              {delta === undefined ? (
                <span className="text-[--color-danger]">no quantity in payload</span>
              ) : (
                <>
                  Would apply <span className="font-mono">{delta > 0 ? "+" : ""}{delta}</span> to
                  whatever the value is at execution time.
                </>
              )}
            </span>
          </Row>
          <Row label="Current">
            <span className="font-mono text-[--color-ink-2]">
              {current === undefined ? "unknown" : current}
            </span>
          </Row>
          <p className="mt-2 text-[10px] leading-relaxed text-[--color-pending]">
            No before/after is shown because the record no longer matches the
            revision this was prepared against. Execution is version-checked and
            would be refused; prepare a fresh revision to see a true diff.
          </p>
        </>
      ) : (
        <>
          <Row label="Change">
            {delta === undefined ? (
              <span className="text-[--color-danger]">no quantity in payload</span>
            ) : (
              <span className="font-mono">
                {delta > 0 ? "+" : ""}
                {delta}
              </span>
            )}
          </Row>
          <Row label="Quantity">
            {current === undefined || after === undefined ? (
              <span className="text-[--color-ink-3]">
                current value not available for this record
              </span>
            ) : (
              <span className="flex items-center gap-2 font-mono">
                <span className="text-[--color-ink-3]">{current}</span>
                <span className="text-[--color-ink-3]">→</span>
                <span
                  className={
                    after < 0 ? "font-semibold text-[--color-danger]" : "font-semibold"
                  }
                >
                  {after}
                </span>
                {after < 0 ? <Badge tone="danger">would go negative</Badge> : null}
              </span>
            )}
          </Row>
        </>
      )}
    </div>
  );
}

function DocumentsPreview({ payload }: { payload: Record<string, unknown> }) {
  const title = (payload.title as string) ?? "";
  const content = (payload.content as string) ?? "";
  return (
    <div className="rounded-md border border-[--color-line] bg-[--color-canvas]">
      <div className="px-3 py-2">
        <Row label="Title">
          {title || <span className="text-[--color-danger]">title is empty</span>}
        </Row>
      </div>
      {content ? (
        <div className="border-t border-[--color-line] px-3 py-2.5">
          <p className="whitespace-pre-wrap text-[12px] leading-relaxed text-[--color-ink-2]">
            {content}
          </p>
        </div>
      ) : null}
    </div>
  );
}

/** Render one operation as the change it would make. */
export function OperationPreview({
  op,
  target,
}: {
  op: Operation;
  target?: Record_;
}) {
  if (op.integration === "mail") {
    return <MailPreview payload={op.payload} target={target} />;
  }
  if (op.integration === "inventory") {
    return (
      <InventoryPreview
        payload={op.payload}
        target={target}
        expectedVersion={op.expected_version}
      />
    );
  }
  if (op.integration === "documents") {
    return <DocumentsPreview payload={op.payload} />;
  }
  return (
    <div className="rounded-md border border-[--color-line] bg-[--color-canvas] p-3">
      <p className="text-[11px] text-[--color-ink-3]">
        No preview renderer exists for <code>{op.integration}</code>. The raw operation is shown
        in the technical panel rather than guessed at here.
      </p>
    </div>
  );
}

export function OperationHeading({ op }: { op: Operation }) {
  const Icon = integrationIcon(op.integration);
  return (
    <div className="flex items-center gap-2">
      <span className="grid size-6 place-items-center rounded bg-[--color-accent-soft] text-[--color-accent]">
        <Icon className="size-3.5" />
      </span>
      <span className="text-[12px] font-medium">
        {ACTION_LABEL[op.action] ?? op.action} · {op.integration}
      </span>
      <span className="ml-auto font-mono text-[10px] text-[--color-ink-3]">
        {op.target_id}
      </span>
    </div>
  );
}
