import { createClient, type Client } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { ChatService } from "@/gen/lumi/chat/v1/chat_pb";

/**
 * The Go service is the only backend. Unlike the Next app in lumi-neo's
 * ecosystem, there is no TypeScript API layer here — the browser calls the Go
 * service directly over the Connect protocol, which gives us real server
 * streaming over fetch with no proxy in front.
 */
// Same-origin by default. In the cluster one ingress host serves both the page
// and the Connect endpoints, so a relative base needs no CORS preflight and —
// more importantly — keeps the built image portable: NEXT_PUBLIC_* is inlined at
// build time, so baking a hostname here would mean one image per environment.
// `next dev` picks up the absolute local URL from web/.env.development, where
// the two processes really do live on different ports.
const baseUrl = process.env.NEXT_PUBLIC_LUMI_URL ?? "";

const transport = createConnectTransport({
  baseUrl,
  // The Connect protocol (not gRPC-Web) — server streaming works over plain
  // fetch, so no HTTP/2 requirement in the browser.
  useBinaryFormat: false,
});

export const chatClient: Client<typeof ChatService> = createClient(ChatService, transport);

/**
 * Dev credentials. AUTH_MODE=dev on the server treats the bearer token as the
 * user id, so `user:workspace` is enough to get a scoped session. This is a
 * local-development affordance only — the real panel token replaces it when
 * the production verifier lands, and nothing else in this file changes.
 */
export function authHeaders(): HeadersInit {
  const token = process.env.NEXT_PUBLIC_DEV_TOKEN ?? "dev-user:local-workspace";
  return { Authorization: `Bearer ${token}` };
}

const CONVERSATION_KEY = "lumi.conversationId";

/** A conversation id per browser tab, so a reload resumes the same thread. */
export function conversationId(): string {
  const existing = window.sessionStorage.getItem(CONVERSATION_KEY);
  if (existing) return existing;
  const id = crypto.randomUUID();
  window.sessionStorage.setItem(CONVERSATION_KEY, id);
  return id;
}

/**
 * Mint a fresh conversation id and return it.
 *
 * Nothing is created server-side here, deliberately. The service creates a
 * conversation row on the first message of a turn, so an id that is never used
 * costs nothing — whereas asking the server for an empty conversation up front
 * would leave a row behind every time somebody clicked this and then left.
 * The new thread begins when the agent actually runs.
 */
export function resetConversation(): string {
  const id = crypto.randomUUID();
  window.sessionStorage.setItem(CONVERSATION_KEY, id);
  return id;
}
