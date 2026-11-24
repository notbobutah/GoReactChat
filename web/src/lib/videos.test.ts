import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const ID = "0b9d3c1e-5a52-4b7e-9a41-2f6f6f1d7c01";

const video = {
  id: ID,
  title: "Harbour lights",
  description: "A night crossing.",
  url: `https://expona-images.us-lax-1.linodeobjects.com/videos/${ID}/cinematic-graded.mp4`,
  sizeBytes: 12345678,
  lastModified: "2026-08-22T10:00:00Z",
};

const fetchMock = vi.fn<typeof fetch>();

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

// The base URL may be read when the module loads, so every test imports a fresh
// copy under the environment it needs.
async function load(baseUrl?: string) {
  vi.stubEnv("NEXT_PUBLIC_LUMI_URL", baseUrl);
  vi.resetModules();
  return import("@/lib/videos");
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("loadVideos", () => {
  it("requests the same-origin relative /api/videos URL with the signal", async () => {
    fetchMock.mockResolvedValue(json({ videos: [video] }));
    const { loadVideos } = await load();
    const { signal } = new AbortController();

    await loadVideos(signal);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/videos");
    expect(init?.signal).toBe(signal);
  });

  it("uses NEXT_PUBLIC_LUMI_URL as the base when it is set", async () => {
    fetchMock.mockResolvedValue(json({ videos: [video] }));
    const { loadVideos } = await load("http://localhost:8080");

    await loadVideos(new AbortController().signal);

    expect(fetchMock.mock.calls[0][0]).toBe("http://localhost:8080/api/videos");
  });

  it("returns ready with the videos in the order the API sent them", async () => {
    const older = { ...video, id: "1c8e4d2f-6b63-4c8f-8b52-3a7a7a2e8d02" };
    fetchMock.mockResolvedValue(json({ videos: [video, older] }));
    const { loadVideos } = await load();

    expect(await loadVideos(new AbortController().signal)).toEqual({
      status: "ready",
      videos: [video, older],
    });
  });

  it("returns empty for an empty list", async () => {
    fetchMock.mockResolvedValue(json({ videos: [] }));
    const { loadVideos } = await load();

    expect(await loadVideos(new AbortController().signal)).toMatchObject({ status: "empty" });
  });

  it("returns error for a 503", async () => {
    fetchMock.mockResolvedValue(json({ error: "video portfolio unavailable" }, 503));
    const { loadVideos } = await load();

    expect(await loadVideos(new AbortController().signal)).toMatchObject({ status: "error" });
  });

  it("returns error for a network failure", async () => {
    fetchMock.mockRejectedValue(new TypeError("fetch failed"));
    const { loadVideos } = await load();

    expect(await loadVideos(new AbortController().signal)).toMatchObject({ status: "error" });
  });

  it("returns error when a 200 body is not JSON", async () => {
    fetchMock.mockResolvedValue(new Response("<html></html>", { status: 200 }));
    const { loadVideos } = await load();

    expect(await loadVideos(new AbortController().signal)).toMatchObject({ status: "error" });
  });
});

describe("downloadUrl", () => {
  it("is the relative API download path", async () => {
    const { downloadUrl } = await load();
    expect(downloadUrl(ID)).toBe(`/api/videos/${ID}/download`);
  });

  it("uses NEXT_PUBLIC_LUMI_URL as the base when it is set", async () => {
    const { downloadUrl } = await load("http://localhost:8080");
    expect(downloadUrl(ID)).toBe(`http://localhost:8080/api/videos/${ID}/download`);
  });
});

describe("formatDate", () => {
  it("formats as day, short month and year", async () => {
    const { formatDate } = await load();
    expect(formatDate("2026-08-22T10:00:00Z")).toBe("22 Aug 2026");
  });

  it.each(["America/Los_Angeles", "Pacific/Auckland"])("uses the UTC day in %s", async (tz) => {
    vi.stubEnv("TZ", tz);
    const { formatDate } = await load();
    expect(formatDate("2026-08-22T00:30:00Z")).toBe("22 Aug 2026");
    expect(formatDate("2026-08-22T23:30:00Z")).toBe("22 Aug 2026");
  });
});
