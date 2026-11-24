import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { Chat } from "@/components/chat";

vi.mock("@/lib/chat-client", () => ({
  authHeaders: () => ({}),
  chatClient: {},
  conversationId: () => "conversation",
  resetConversation: () => "conversation",
}));
vi.mock("@/components/news-panel", () => ({ NewsPanel: () => null }));
vi.mock("next/image", () => ({ default: () => null }));

describe("chat sidebar", () => {
  const html = renderToStaticMarkup(<Chat />);
  const sidebar = html.match(/<aside\b[\s\S]*?<\/aside>/)?.[0] ?? "";

  it("links to the video portfolio", () => {
    const links = sidebar.match(/<a\b[\s\S]*?<\/a>/g) ?? [];
    const link = links.find((a) => /\shref="\/videos"/.test(a));
    expect(link).toBeDefined();
    expect((link ?? "").replace(/<[^>]*>/g, "").trim()).not.toBe("");
  });

  it("keeps the existing sidebar links", () => {
    expect(sidebar).toContain('href="https://github.com/notbobutah/GoReactChat"');
    expect(sidebar).toContain('href="/Robert-MacKay-Resume.pdf"');
  });
});
