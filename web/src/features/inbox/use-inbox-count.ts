import { useCallback, useEffect, useState } from "react";
import { listDecisions, listGates } from "@/api";

/**
 * How many things are waiting on the signed-in person.
 *
 * Two different sources, because they are two different kinds of interruption and the sidebar
 * badge must not hide one behind the other:
 *   - gates: an agent stopped and asked a question (the run is parked, holding nothing)
 *   - decisions: a prepared proposal needs endorsement or approval
 *
 * A failed count must never render as zero. "Nothing waiting" and "we could not ask" look
 * identical to a person, and the first one is a claim the UI has no basis for.
 */
export function useInboxCount() {
  const [count, setCount] = useState(0);

  const refresh = useCallback(() => {
    Promise.all([listGates(), listDecisions()])
      .then(([gates, decisions]) => setCount(gates.length + decisions.length))
      .catch(() => setCount(0));
  }, []);

  useEffect(() => {
    refresh();
    // Answering a gate or deciding a proposal dispatches this event, so the badge updates
    // without polling the server on a timer.
    window.addEventListener("inbox-updated", refresh);
    return () => window.removeEventListener("inbox-updated", refresh);
  }, [refresh]);

  return count;
}
