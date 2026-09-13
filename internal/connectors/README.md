# Connector operation shapes

These connectors are explicitly **simulators**. They perform no network calls or external writes. `Prepare` returns a new record and leaves its input untouched.

Every operation is JSON shaped as:

```json
{"id":"op-1","integration":"inventory","action":"adjust","target_id":"sku-1","expected_version":2,"payload":{"delta":-3}}
```

Supported operations:

- `inventory/adjust`: payload exactly `{ "delta": integer }`; resulting `quantity` must remain a finite, bounded, non-negative integer.
- `mail/send`: payload requires `recipients`, `subject`, `body`, `bcc`, and `attachments`. `bcc` and `attachments` must be explicit arrays (empty arrays are safe); attachment values are simulator references only.
- `documents/update`: payload contains only `title` and/or `content`; at least one is required.

Unknown integrations/actions, hidden payload properties, malformed JSON, missing required fields, target mismatch, and version mismatch are rejected with exported sentinel errors.
