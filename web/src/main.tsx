import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router-dom";

import "@/index.css";
// The legacy console keeps its original styles, scoped under `.legacy` so its global element
// rules (`nav button`, `aside`, `h1`) cannot restyle this shell. See styles-legacy.css.
import "@/styles-legacy.css";

import { SessionProvider, useSession } from "@/app/session";
import { AppShell } from "@/app/app-shell";
import { AuthGate } from "@/app/auth-gate";
import { MyWorkPage } from "@/features/work/my-work-page";
import { InboxPage } from "@/features/inbox/inbox-page";
import { TaskPage } from "@/features/task/task-page";
import { ProjectsPage } from "@/features/projects/projects-page";
import { AgentsPage } from "@/features/agents/agents-page";
import { KnowledgePage } from "@/features/knowledge/knowledge-page";
import { TeamPage } from "@/features/team/team-page";
import { AdminPage } from "@/features/admin/admin-page";

/**
 * Authentication gate.
 *
 * Rendered before any route, so an unauthenticated visitor never sees the shell with empty
 * panels — which reads as "there is no work here" rather than "you are not signed in".
 */
function Gate({ children }: { children: React.ReactNode }) {
  const { me, loading } = useSession();
  if (loading) {
    return (
      <div className="grid min-h-dvh place-items-center bg-[--color-canvas]">
        <p className="text-[12px] text-[--color-ink-3]">
          Connecting to the control plane…
        </p>
      </div>
    );
  }
  if (!me) return <AuthGate />;
  return <>{children}</>;
}

export function App() {
  return (
    <SessionProvider>
      <Gate>
        <Routes>
          <Route element={<AppShell />}>
            <Route index element={<MyWorkPage />} />
            <Route path="inbox" element={<InboxPage />} />
            <Route path="tasks/:id" element={<TaskPage />} />
            <Route path="projects" element={<ProjectsPage />} />
            <Route path="agents" element={<AgentsPage />} />
            <Route path="knowledge" element={<KnowledgePage />} />
            <Route path="team" element={<TeamPage />} />
            <Route path="admin" element={<AdminPage />} />
          </Route>
        </Routes>
      </Gate>
    </SessionProvider>
  );
}

const host = document.getElementById("root");
if (host) {
  createRoot(host).render(
    <StrictMode>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </StrictMode>,
  );
}
