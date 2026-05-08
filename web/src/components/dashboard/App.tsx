import { useEffect, useState } from "preact/hooks";
import { Router, Route, Redirect } from "wouter-preact";
import { me, addToast } from "./state.js";
import { handleCallback, signIn, signOut } from "@/lib/oauth.js";
import { api, ApiError, type MeResponse } from "@/lib/api.js";
import { pollForActivation } from "@/lib/stripe-return.js";
import Modal from "./Modal.js";
import Toast from "./Toast.js";
import AccountSection from "./AccountSection.js";
import KeysSection, { loadKeys } from "./KeysSection.js";
import UsageSection, { loadUsage } from "./UsageSection.js";
import BillingSection from "./BillingSection.js";

type AppState = "loading" | "gate" | "ready";

export default function App() {
  const [state, setState] = useState<AppState>("loading");
  const [gateMsg, setGateMsg] = useState("");

  useEffect(() => {
    async function init() {
      const url = new URL(location.href);

      if (url.searchParams.get("verified") === "1") {
        history.replaceState(null, "", location.origin + "/app/");
        addToast("Email verified successfully.", "success");
      }

      const checkoutResult = url.searchParams.get("checkout");
      if (checkoutResult) {
        history.replaceState(null, "", location.origin + "/app/");
        if (checkoutResult === "success") {
          await pollForActivation(addToast);
        } else if (checkoutResult === "cancelled") {
          addToast("Checkout cancelled.", "info");
        }
      }

      try {
        await handleCallback();
      } catch (e: unknown) {
        const err = e as Error;
        setGateMsg(err?.message ?? "Authentication error");
        setState("gate");
        return;
      }

      let meData: MeResponse | null = null;
      try {
        meData = await api<MeResponse>("/api/dashboard/me");
      } catch (e: unknown) {
        if (e instanceof ApiError && e.status === 401) {
          setState("gate");
          return;
        }
        // transient error (5xx / network) — show toast and retry once after 2s
        addToast("Network error loading profile, retrying…", "error");
        await new Promise((res) => setTimeout(res, 2000));
        try {
          meData = await api<MeResponse>("/api/dashboard/me");
        } catch {
          setState("gate");
          return;
        }
      }
      if (!meData) {
        setState("gate");
        return;
      }
      me.value = meData;
      setState("ready");
      await Promise.all([loadKeys(), loadUsage()]);
    }
    init();
  }, []);

  if (state === "loading") {
    return (
      <>
        <Modal />
        <Toast />
        <nav>
          <span class="logo">msg2agent</span>
        </nav>
        <main>
          <div
            class="skeleton skeleton-sm-narrow"
            style="margin: 2rem auto; width: 200px; height: 1.5rem;"
          />
        </main>
      </>
    );
  }

  if (state === "gate") {
    return (
      <>
        <Modal />
        <Toast />
        <nav>
          <a href="/" class="logo">
            msg2agent
          </a>
        </nav>
        <div class="auth-gate-container">
          <h1 class="auth-gate-title">Sign in to msg2agent</h1>
          <p class="auth-gate-subtitle">
            Use your API key to access the dashboard.
          </p>
          {gateMsg && <p class="error-text">{gateMsg}</p>}
          <button
            class="btn-primary"
            onClick={() => signIn().catch((e) => setGateMsg(e.message))}
          >
            Sign in with msg2agent
          </button>
        </div>
      </>
    );
  }

  const meVal = me.value!;

  async function resendVerification() {
    try {
      const r = await fetch("/api/email/resend", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: meVal.email }),
      });
      if (r.status === 429) {
        addToast(
          "Verification email already sent recently. Check your inbox.",
          "info",
        );
      } else if (r.ok) {
        addToast("Verification email sent — check your inbox.", "success");
        sessionStorage.setItem("m2a_verify_dismissed", "1");
      } else {
        addToast("Failed to resend verification email.", "error");
      }
    } catch {
      addToast("Failed to resend verification email.", "error");
    }
  }

  const verifyDismissed =
    sessionStorage.getItem("m2a_verify_dismissed") === "1";
  const showVerifyBanner = !meVal.email_verified && !verifyDismissed;

  return (
    <>
      <Modal />
      <Toast />
      <nav>
        <a href="/" class="logo">
          msg2agent
        </a>
        <span id="nav-plan">{meVal.plan}</span>
        <button id="btn-signout" onClick={signOut}>
          Sign out
        </button>
      </nav>
      {showVerifyBanner && (
        <div class="verify-banner">
          <span>
            Please verify your email address to keep your account active.
          </span>
          <button class="btn-ghost btn-sm" onClick={resendVerification}>
            Resend email
          </button>
          <button
            class="btn-ghost btn-sm"
            onClick={() => {
              sessionStorage.setItem("m2a_verify_dismissed", "1");
              setState("ready");
            }}
            aria-label="Dismiss"
          >
            ✕
          </button>
        </div>
      )}
      <main>
        <Router base="/app">
          <Route path="/" component={() => <Redirect to="/account" />} />
          <Route path="/account" component={AccountSection} />
          <Route path="/keys" component={KeysSection} />
          <Route path="/usage" component={UsageSection} />
          <Route path="/billing" component={BillingSection} />
        </Router>
      </main>
    </>
  );
}
