import { BookOpen, Search } from "lucide-react";
import { Empty, Panel } from "@/components/ui/panel";
import { PageHeader } from "@/app/app-shell";

/**
 * Knowledge.
 *
 * This area is a placeholder, and it says so plainly.
 *
 * There is no context backend wired up: no Gontext adapter, no index, no retrieval. Shipping a
 * search box that returned nothing would be worse than shipping no search box, because an
 * empty result reads as "we have no knowledge about this" when the truth is "this is not built".
 * The distinction is the same one the empty-task state makes, and it matters more here.
 */
export function KnowledgePage() {
  return (
    <>
      <PageHeader
        eyebrow="Knowledge"
        title="Knowledge"
        description="Shared organisational knowledge with provenance — what is true, who established it, and what it was derived from."
      />
      <div className="p-6">
        <Panel className="p-6">
          <div className="mx-auto max-w-lg text-center">
            <span className="mx-auto grid size-9 place-items-center rounded-md bg-[--color-raised] text-[--color-ink-3]">
              <BookOpen className="size-4" />
            </span>
            <h2 className="mt-3 text-[13px] font-medium">Not connected yet</h2>
            <p className="mt-2 text-[11px] leading-relaxed text-[--color-ink-3]">
              This area will hold permission-scoped search across the
              organisation&apos;s documents and decisions, with citations back to
              the source. It requires a context backend that is not wired up in
              this build.
            </p>
            <div className="mt-4 flex items-center justify-center gap-2 rounded-md border border-dashed border-[--color-line-strong] px-3 py-2">
              <Search className="size-3.5 text-[--color-ink-3]" />
              <span className="text-[11px] text-[--color-ink-3]">
                No search — an empty result would be misleading
              </span>
            </div>
            <p className="mt-4 text-[10px] leading-relaxed text-[--color-ink-3]">
              Until it exists, evidence for a decision lives on the task itself,
              attached to the proposal revision being reviewed.
            </p>
          </div>
        </Panel>
      </div>
    </>
  );
}
