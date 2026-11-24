import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";

import VideosPage, { metadata } from "@/app/videos/page";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("/videos page", () => {
  it("is titled Expona portfolio", () => {
    expect(metadata.title).toBe("Expona portfolio");
    const html = renderToStaticMarkup(<VideosPage />);
    expect(html).toMatch(/<h1\b[^>]*>Expona portfolio<\/h1>/);
  });

  it("starts in the loading state without fetching on the server", () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const html = renderToStaticMarkup(<VideosPage />);

    expect(html.replace(/<[^>]*>/g, " ")).toMatch(/loading/i);
    expect(html).not.toContain("<video");
    expect(html).not.toContain('role="alert"');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
