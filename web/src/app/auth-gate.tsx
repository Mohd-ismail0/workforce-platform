import { useState } from "react";
import { AlertTriangle, KeyRound, LogIn } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useSession } from "@/app/session";

/**
 * Sign-in.
 *
 * Two presentations, and which one appears is decided by the SERVER, not by the UI: if
 * /auth/session refuses with 401 the deployment runs on local development tokens, and anything
 * else means real sign-in is configured. A UI that guessed would offer a token box in a
 * deployment that has SSO, which is how a shared secret becomes a credential.
 */

const REASONS: Record<string, string> = {
  unlinked:
    "You signed in, but no platform identity is linked to this account yet. Access is provisioned by an administrator — it is not granted automatically on first sign-in.",
  login_failed:
    "The sign-in could not be completed. Try again; if it keeps failing, an administrator should check the identity provider configuration.",
  login_expired: "That sign-in attempt expired before it finished. Start again.",
  provider_refused: "The identity provider refused the sign-in request.",
  session_unusable: "Your session is no longer valid. Sign in again to continue.",
};

function Frame({
  children,
  eyebrow,
}: {
  children: React.ReactNode;
  eyebrow: string;
}) {
  return (
    <div className="grid min-h-dvh place-items-center bg-[--color-canvas] px-4">
      <div className="w-full max-w-sm rounded-[--radius-panel] border border-[--color-line] bg-[--color-surface] p-7">
        <div className="mb-5 flex items-center gap-2.5">
          <span className="grid size-7 place-items-center rounded-md bg-[--color-accent] text-[13px] font-bold text-[--color-accent-ink]">
            W
          </span>
          <span className="text-[13px] font-semibold tracking-tight">
            workforce
          </span>
        </div>
        <p className="eyebrow">{eyebrow}</p>
        {children}
      </div>
    </div>
  );
}

export function AuthGate() {
  const { mode, signedIn: _signedIn, signIn, error } = useSessionState();

  if (mode === "local") return <LocalTokenForm />;
  return (
    <Frame eyebrow="Sign in">
      <h1 className="mt-1 text-base font-semibold">Sign in to the workspace</h1>
      <p className="mt-2 text-[11px] leading-relaxed text-[--color-ink-3]">
        Sign-in is handled by your organisation&apos;s identity provider. This
        application never sees or stores your password.
      </p>
      {error ? (
        <div className="mt-4 flex items-start gap-2 rounded-md border border-[--color-danger]/40 bg-[--color-danger-soft] p-2.5">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-[--color-danger]" />
          <p className="text-[11px] text-[--color-danger]">
            {REASONS[error] ?? "Sign-in failed."}
          </p>
        </div>
      ) : null}
      <Button variant="accent" className="mt-5 w-full" onClick={signIn}>
        <LogIn className="size-3.5" />
        Sign in with SSO
      </Button>
      <p className="mt-4 text-[10px] leading-relaxed text-[--color-ink-3]">
        Signing in proves who you are. It does not grant access — work and
        approvals are granted per person by an administrator.
      </p>
    </Frame>
  );
}

function LocalTokenForm() {
  const { submitLocalToken, error, localToken } = useSessionState();
  const [value, setValue] = useState(localToken);

  return (
    <Frame eyebrow="Local development">
      <h1 className="mt-1 text-base font-semibold">Connect with a dev token</h1>
      <p className="mt-2 text-[11px] leading-relaxed text-[--color-ink-3]">
        This deployment has no identity provider configured. Enter the token
        from your local auth file.
      </p>
      <form
        className="mt-4 grid gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          submitLocalToken(value);
        }}
      >
        <label className="grid gap-1.5">
          <span className="text-[11px] font-medium text-[--color-ink-2]">
            Development token
          </span>
          <input
            type="password"
            autoComplete="off"
            className="w-full rounded-md border border-[--color-line-strong] bg-[--color-canvas] px-2.5 py-1.5 text-xs"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="Bearer token"
          />
        </label>
        {error ? (
          <p className="text-[11px] text-[--color-danger]">{error}</p>
        ) : null}
        <Button type="submit" variant="accent" className="mt-1 w-full">
          <KeyRound className="size-3.5" />
          Connect
        </Button>
      </form>
      <p className="mt-4 text-[10px] leading-relaxed text-[--color-ink-3]">
        Development identities only. This form is never shown when real sign-in
        is configured.
      </p>
    </Frame>
  );
}

// Small indirection so both screens read the same fields from one place.
function useSessionState() {
  const s = useSession();
  return { ...s, signedIn: !!s.me };
}
