import { describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./client";

describe("ApiClient", () => {
  it("builds query params for listDocuments", async () => {
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            items: [],
            limit: 50,
            offset: 10,
            total: 0,
          }),
          { status: 200 },
        ),
    );

    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );
    await client.listDocuments({
      status: "indexed",
      limit: 50,
      offset: 10,
      source_type: "upload",
      tags: ["MSA", "Finance"],
    });

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/documents?status=indexed&source_type=upload&tag=MSA&tag=Finance&limit=50&offset=10",
      expect.objectContaining({ method: "GET" }),
    );
  });

  it("sends idempotency key for createDocument", async () => {
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            id: "id",
            filename: "contract.pdf",
            mime_type: "application/pdf",
            status: "ingested",
            created_at: "2025-01-01T00:00:00Z",
            updated_at: "2025-01-01T00:00:00Z",
          }),
          { status: 201 },
        ),
    );

    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );
    await client.createDocument(
      {
        filename: "contract.pdf",
        mime_type: "application/pdf",
        content_base64: "abc",
        tags: ["MSA"],
      },
      { idempotencyKey: "idem-123" },
    );

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/documents",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          "Idempotency-Key": "idem-123",
          "Content-Type": "application/json",
        }),
      }),
    );
    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/documents",
      expect.objectContaining({
        body: expect.stringContaining('"tags":["MSA"]'),
      }),
    );
  });

  it("maps api errors to ApiError", async () => {
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "invalid_argument",
              message: "bad request",
              retriable: false,
            },
          }),
          { status: 400 },
        ),
    );

    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await expect(
      client.startClausePresenceCheck({
        required_clause_text: "Termination for convenience",
      }),
    ).rejects.toEqual(expect.any(ApiError));
  });

  it("falls back gracefully for non-JSON error responses", async () => {
    const fetchFn = vi.fn(
      async () => new Response("<html>gateway down</html>", { status: 502 }),
    );
    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await expect(client.listDocuments()).rejects.toMatchObject({
      name: "ApiError",
      status: 502,
      code: "http_error",
      message: "<html>gateway down</html>",
      retriable: true,
    });
  });

  it("posts LLM review checks to the dedicated guideline endpoint", async () => {
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            check_id: "check-llm-1",
            status: "queued",
            check_type: "llm_review",
          }),
          { status: 202 },
        ),
    );

    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await client.startLLMReviewCheck(
      {
        document_ids: ["doc-1"],
        instructions:
          "Review the whole contract for a termination for convenience right.",
      },
      { idempotencyKey: "idem-llm-1" },
    );

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/guidelines/llm-review",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          "Idempotency-Key": "idem-llm-1",
          "Content-Type": "application/json",
        }),
        body: expect.stringContaining(
          '"instructions":"Review the whole contract for a termination for convenience right."',
        ),
      }),
    );
  });

  it("sends delete request for deleteDocument", async () => {
    const fetchFn = vi.fn(async () => new Response(null, { status: 204 }));
    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await client.deleteDocument("00000000-0000-4000-8000-000000000001");

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/documents/00000000-0000-4000-8000-000000000001",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("sends delete request for deleteCheckRun", async () => {
    const fetchFn = vi.fn(async () => new Response(null, { status: 204 }));
    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await client.deleteCheckRun("00000000-0000-4000-8000-000000000011");

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/guidelines/00000000-0000-4000-8000-000000000011",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("sends bulk delete request for deleteCheckRuns", async () => {
    const fetchFn = vi.fn(async () => new Response(null, { status: 204 }));
    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await client.deleteCheckRuns({
      check_ids: [
        "00000000-0000-4000-8000-000000000011",
        "00000000-0000-4000-8000-000000000012",
      ],
    });

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/guidelines",
      expect.objectContaining({
        method: "DELETE",
        body: JSON.stringify({
          check_ids: [
            "00000000-0000-4000-8000-000000000011",
            "00000000-0000-4000-8000-000000000012",
          ],
        }),
      }),
    );
  });

  it("fetches extracted text for a document", async () => {
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            document_id: "00000000-0000-4000-8000-000000000001",
            filename: "contract.pdf",
            text: "Contract text",
            has_text: true,
          }),
          { status: 200 },
        ),
    );
    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await client.getDocumentText("00000000-0000-4000-8000-000000000001");

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/documents/00000000-0000-4000-8000-000000000001/text",
      expect.objectContaining({ method: "GET" }),
    );
  });

  it("builds document content url for inline previews", () => {
    const client = new ApiClient("http://localhost:8080");

    expect(
      client.getDocumentContentUrl("00000000-0000-4000-8000-000000000001"),
    ).toBe(
      "http://localhost:8080/api/v1/documents/00000000-0000-4000-8000-000000000001/content",
    );
  });

  it("sends patch request for updateContract", async () => {
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            id: "00000000-0000-4000-8000-000000000001",
            name: "Updated Contract",
            tags: ["MSA", "Finance"],
            file_count: 0,
            created_at: "2025-01-01T00:00:00Z",
            updated_at: "2025-01-01T00:00:00Z",
          }),
          { status: 200 },
        ),
    );
    const client = new ApiClient(
      "http://localhost:8080",
      fetchFn as typeof fetch,
    );

    await client.updateContract("00000000-0000-4000-8000-000000000001", {
      name: "Updated Contract",
      tags: ["MSA", "Finance"],
    });

    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/contracts/00000000-0000-4000-8000-000000000001",
      expect.objectContaining({ method: "PATCH" }),
    );
    expect(fetchFn).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/contracts/00000000-0000-4000-8000-000000000001",
      expect.objectContaining({
        body: expect.stringContaining('"name":"Updated Contract"'),
      }),
    );
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
          total: 0,
        }),
        { status: 200 },
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
