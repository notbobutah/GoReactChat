import { createClient, type Client } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { ChatService } from "@/gen/lumi/chat/v1/chat_pb";

/**
 * The Go service is the only backend. Unlike the Next app in lumi-neo's
 * ecosystem, there is no TypeScript API layer here — the browser calls the Go
 * service directly over the Connect protocol, which gives us real server
 * streaming over fetch with no proxy in front.
 */
const baseUrl = process.env.NEXT_PUBLIC_LUMI_URL ?? "http://localhost:8080";

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

/** A conversation id per browser tab, so a reload resumes the same thread. */
export function conversationId(): string {
  const key = "lumi.conversationId";
  const existing = window.sessionStorage.getItem(key);
  if (existing) return existing;
  const id = crypto.randomUUID();
  window.sessionStorage.setItem(key, id);
  return id;
}
