import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { getSession, getToken, loginUrl, logout, type Me } from "@/api";

/**
 * Who the browser is, and how they sign in.
 *
 * The credential differs by deployment and the UI must not guess: in OIDC mode the browser
 * holds an HttpOnly cookie it cannot read, so there is no token to look for and a token form
 * would be actively wrong. Only a 401 from /auth/session means "this deployment uses local
 * development tokens".
 */

export type AuthMode = "unknown" | "session" | "local";

type SessionValue = {
  mode: AuthMode;
  me?: Me;
  loading: boolean;
  signedOut: boolean;
  error: string;
  signIn: () => void;
  signOut: () => void;
  submitLocalToken: (token: string) => void;
  localToken: string;
};

const SessionContext = createContext<SessionValue | null>(null);

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [mode, setMode] = useState<AuthMode>("unknown");
  const [me, setMe] = useState<Me>();
  const [loading, setLoading] = useState(true);
  const [signedOut, setSignedOut] = useState(false);
  const [error, setError] = useState("");
  const [localToken, setLocalToken] = useState(getToken());

  useEffect(() => {
    let cancelled = false;
    getSession()
      .then((s) => {
        if (cancelled) return;
        if (s === null) {
          setMode("local");
          return;
        }
        setMode("session");
        if (s.authenticated && s.identity) setMe(s.identity);
        else setSignedOut(true);
      })
      .catch(() => {
        if (cancelled) return;
        // Reachable but failing is NOT local mode. Falling back to a token form here would
        // invite a shared secret into a deployment that has real sign-in.
        setMode("session");
        setError("login_failed");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Local development still requires an explicit token before /me can be answered.
  useEffect(() => {
    if (mode !== "local" || !localToken) return;
    let cancelled = false;
    setLoading(true);
    import("@/api")
      .then(({ api }) => api<Me>("/me"))
      .then((v) => {
        if (!cancelled) setMe(v);
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [mode, localToken]);

  const signOut = useCallback(() => {
    setError("");
    if (mode === "session") {
      // A cookie session must end server-side. Clearing a client variable would leave the
      // cookie in place and the person still signed in.
      void logout()
        .then((url) => {
          window.location.href = url || "/";
        })
        .catch(() => window.location.reload());
      return;
    }
    import("@/api").then(({ setToken }) => {
      setToken("");
      setLocalToken("");
      setMe(undefined);
    });
  }, [mode]);

  const value = useMemo<SessionValue>(
    () => ({
      mode,
      me,
      loading,
      signedOut,
      error,
      localToken,
      signIn: () => {
        window.location.href = loginUrl("/");
      },
      signOut,
      submitLocalToken: (token: string) => {
        void import("@/api").then(({ setToken }) => {
          setToken(token);
          setLocalToken(token);
        });
      },
    }),
    [mode, me, loading, signedOut, error, localToken, signOut],
  );

  return (
    <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
  );
}

export function useSession() {
  const v = useContext(SessionContext);
  if (!v) throw new Error("useSession used outside SessionProvider");
  return v;
}
