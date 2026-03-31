import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ContractEditPage } from "./ContractEditPage";

const apiMocks = vi.hoisted(() => ({
  getContract: vi.fn(),
  getDocumentText: vi.fn(),
  getCheckRun: vi.fn(),
  getCheckResults: vi.fn(),
  startClausePresenceCheck: vi.fn(),
  updateContract: vi.fn(),
  reorderContractFiles: vi.fn(),
  addContractFile: vi.fn(),
  getDocumentContentUrl: vi.fn()
}));

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>();
  return {
    ...actual,
    apiClient: {
      ...actual.apiClient,
      getContract: apiMocks.getContract,
      getDocumentText: apiMocks.getDocumentText,
      getCheckRun: apiMocks.getCheckRun,
      getCheckResults: apiMocks.getCheckResults,
      startClausePresenceCheck: apiMocks.startClausePresenceCheck,
      updateContract: apiMocks.updateContract,
      reorderContractFiles: apiMocks.reorderContractFiles,
      addContractFile: apiMocks.addContractFile,
      getDocumentContentUrl: apiMocks.getDocumentContentUrl
    }
  };
});

function renderPage() {
  const router = createMemoryRouter(
    [
      { path: "/contracts/:contractId/edit", element: <ContractEditPage /> },
      { path: "/guidelines", element: <div>Guidelines page</div> }
    ],
    { initialEntries: ["/contracts/contract-1/edit"] }
  );

  render(<RouterProvider router={router} />);
}

describe("ContractEditPage", () => {
  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    Object.values(apiMocks).forEach((mockFn) => mockFn.mockReset());
  });

  it("starts queued automatic guideline checks and shows them in a separate panel below files", async () => {
    window.localStorage.setItem(
      "ldi.guidelineRules",
      JSON.stringify([
        {
          id: "rule-auto",
          name: "Auto keyword",
          rule_type: "keyword_match",
          instructions: "Must contain: payment terms",
          auto_run_on_new_contract: true,
          required_terms: ["payment terms"],
          forbidden_terms: [],
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z"
        }
      ])
    );
    window.localStorage.setItem(
      "ldi.pendingAutoGuidelineRuns",
      JSON.stringify([
        {
          contract_id: "contract-1",
          rule_id: "rule-auto",
          created_at: "2026-01-01T00:00:00Z"
        }
      ])
    );

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z"
        }
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z"
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "The agreement includes payment terms.",
      has_text: true
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");

    renderPage();

    const filesHeading = await screen.findByRole("heading", { name: "Files" });
    const guidelineHeading = await screen.findByRole("heading", { name: "Guideline Checks" });

    expect(filesHeading.compareDocumentPosition(guidelineHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByText("Auto keyword")).toBeVisible();
      expect(screen.getByText("Flagged items: 0")).toBeVisible();
    });

    expect(JSON.parse(window.localStorage.getItem("ldi.pendingAutoGuidelineRuns") ?? "[]")).toEqual([]);
  });
});
