import { describe, expect, it, vi } from "vitest";
import { ApiClient, CheckAcceptedResponse, CheckResultsResponse, CheckRunResponse } from "./client";

describe("check run flow integration", () => {
  it("starts a clause check, polls status, then fetches results", async () => {
    const accepted: CheckAcceptedResponse = {
      check_id: "check-123",
      status: "queued",
      check_type: "llm_review"
    };

    const running: CheckRunResponse = {
      check_id: "check-123",
      status: "running",
      check_type: "llm_review",
      requested_at: "2026-01-01T00:00:00Z"
    };

    const completed: CheckRunResponse = {
      check_id: "check-123",
      status: "completed",
      check_type: "llm_review",
      requested_at: "2026-01-01T00:00:00Z",
      finished_at: "2026-01-01T00:00:03Z"
    };

    const results: CheckResultsResponse = {
      check_id: "check-123",
      status: "completed",
      items: [
        {
          document_id: "doc-1",
          outcome: "missing",
          confidence: 0.92,
          summary: "Termination clause missing",
          evidence: []
        }
      ]
    };

    const fetchFn = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(accepted), { status: 202 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(running), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(completed), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(results), { status: 200 }));

    const client = new ApiClient("http://localhost:8080", fetchFn as typeof fetch);

    const run = await client.startLLMReviewCheck({
      document_ids: ["doc-1"],
      instructions: "Termination for convenience"
    });
    expect(run).toEqual(accepted);

    const firstStatus = await client.getCheckRun(run.check_id);
    expect(firstStatus.status).toBe("running");

    const secondStatus = await client.getCheckRun(run.check_id);
    expect(secondStatus.status).toBe("completed");

    const payload = await client.getCheckResults(run.check_id);
    expect(payload).toEqual(results);
    expect(payload.items[0]?.outcome).toBe("missing");

    expect(fetchFn).toHaveBeenNthCalledWith(
      1,
      "http://localhost:8080/api/v1/guidelines/llm-review",
      expect.objectContaining({ method: "POST" })
    );
    expect(fetchFn).toHaveBeenNthCalledWith(
      2,
      "http://localhost:8080/api/v1/guidelines/check-123",
      expect.objectContaining({ method: "GET" })
    );
    expect(fetchFn).toHaveBeenNthCalledWith(
      3,
      "http://localhost:8080/api/v1/guidelines/check-123",
      expect.objectContaining({ method: "GET" })
    );
    expect(fetchFn).toHaveBeenNthCalledWith(
      4,
      "http://localhost:8080/api/v1/guidelines/check-123/results",
      expect.objectContaining({ method: "GET" })
    );
  });
});
