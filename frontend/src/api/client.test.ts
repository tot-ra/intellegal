import { describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./client";

describe("ApiClient", () => {
  it("builds query params for listDocuments", async () => {
    const fetchFn = vi.fn(async () =>
      new Response(
        JSON.stringify({
          items: [],
          limit: 50,
          offset: 10,
          total: 0
        }),
        { status: 200 }
      )
    );

    const client = new ApiClient("http://localhost:8080", fetchFn as typeof fetch);
    await client.listDocuments({ status: "indexed", limit: 50, offset: 10, source_type: "upload" });

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/documents?status=indexed&source_type=upload&limit=50&offset=10",
      expect.objectContaining({ method: "GET" })
    );
  });

  it("sends idempotency key for createDocument", async () => {
    const fetchFn = vi.fn(async () =>
      new Response(
        JSON.stringify({
          id: "id",
          filename: "contract.pdf",
          mime_type: "application/pdf",
          status: "ingested",
          created_at: "2025-01-01T00:00:00Z",
          updated_at: "2025-01-01T00:00:00Z"
        }),
        { status: 201 }
      )
    );

    const client = new ApiClient("http://localhost:8080", fetchFn as typeof fetch);
    await client.createDocument(
      {
        filename: "contract.pdf",
        mime_type: "application/pdf",
        content_base64: "abc"
      },
      { idempotencyKey: "idem-123" }
    );

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/documents",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          "Idempotency-Key": "idem-123",
          "Content-Type": "application/json"
        })
      })
    );
  });

  it("maps api errors to ApiError", async () => {
    const fetchFn = vi.fn(async () =>
      new Response(
        JSON.stringify({
          error: {
            code: "invalid_argument",
            message: "bad request",
            retriable: false
          }
        }),
        { status: 400 }
      )
    );

    const client = new ApiClient("http://localhost:8080", fetchFn as typeof fetch);

    await expect(
      client.startClausePresenceCheck({ required_clause_text: "Termination for convenience" })
    ).rejects.toEqual(expect.any(ApiError));
  });

  it("uses global fetch path by default to avoid illegal invocation", async () => {
    const originalFetch = globalThis.fetch;
    const fetchFn = vi.fn(async function (this: unknown) {
      expect(this).toBe(globalThis);
      return new Response(
        JSON.stringify({
          items: [],
          limit: 20,
          offset: 0,
          total: 0
        }),
        { status: 200 }
      );
    });

    try {
      globalThis.fetch = fetchFn as unknown as typeof fetch;
      const client = new ApiClient("http://localhost:8080");
      await client.listDocuments();
    } finally {
      globalThis.fetch = originalFetch;
    }

    expect(fetchFn).toHaveBeenCalledOnce();
  });
});
