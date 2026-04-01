import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { Navigate, RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>();
  return {
    ...actual,
    apiClient: {
      listContracts: vi.fn().mockResolvedValue({
        items: [
          {
            id: "contract-1",
            name: "Alpha",
            language: "eng",
            file_count: 1,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
          {
            id: "contract-2",
            name: "Beta",
            language: "eng",
            file_count: 1,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
        limit: 200,
        offset: 0,
        total: 2,
      }),
      listDocuments: vi.fn().mockResolvedValue({
        items: [
          {
            id: "doc-1",
            contract_id: "contract-1",
            filename: "alpha.pdf",
            mime_type: "application/pdf",
            status: "indexed",
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
          {
            id: "doc-2",
            contract_id: "contract-2",
            filename: "beta.pdf",
            mime_type: "application/pdf",
            status: "indexed",
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
        limit: 200,
        offset: 0,
        total: 2,
      }),
      getCheckRun: vi.fn().mockResolvedValue({
        check_id: "00000000-0000-4000-8000-000000000000",
        status: "completed",
        check_type: "clause_presence",
        requested_at: "2026-01-01T00:00:00Z",
      }),
      getCheckResults: vi.fn().mockResolvedValue({
        check_id: "00000000-0000-4000-8000-000000000000",
        status: "completed",
        items: [],
      }),
      getDocument: vi.fn().mockResolvedValue({
        id: "00000000-0000-4000-8000-000000000000",
        filename: "contract.pdf",
        mime_type: "application/pdf",
        status: "indexed",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      }),
      getDocumentText: vi.fn().mockResolvedValue({
        document_id: "00000000-0000-4000-8000-000000000000",
        filename: "contract.pdf",
        text: "test",
        has_text: true,
      }),
      searchContractSections: vi.fn().mockResolvedValue({ items: [] }),
      createDocument: vi.fn(),
      deleteDocument: vi.fn(),
      startClausePresenceCheck: vi.fn(),
      startLLMReviewCheck: vi.fn(),
      deleteCheckRun: vi.fn(),
      deleteCheckRuns: vi.fn(),
    },
  };
});
import { AppShell } from "./AppShell";
import { ApiError, apiClient } from "../api/client";
import { AuditPage } from "../pages/AuditPage";
import { BatchImportContractsPage } from "../pages/BatchImportContractsPage";
import { ContractsPage } from "../pages/ContractsPage";
import { CompareContractsPage } from "../pages/CompareContractsPage";
import { DashboardPage } from "../pages/DashboardPage";
import { GuidelineCreatePage } from "../pages/GuidelineCreatePage";
import { GuidelineRunPage } from "../pages/GuidelineRunPage";
import { GuidelinesPage } from "../pages/GuidelinesPage";
import { NewContractPage } from "../pages/NewContractPage";
import { NotFoundPage } from "../pages/NotFoundPage";
import { ResultsPage } from "../pages/ResultsPage";
import { SearchPage } from "../pages/SearchPage";

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      {
        path: "/",
        element: <AppShell />,
        children: [
          { index: true, element: <DashboardPage /> },
          { path: "search", element: <SearchPage /> },
          { path: "contracts", element: <ContractsPage /> },
          { path: "contracts/import", element: <BatchImportContractsPage /> },
          { path: "contracts/new", element: <NewContractPage /> },
          { path: "contracts/compare", element: <CompareContractsPage /> },
          { path: "guidelines", element: <GuidelinesPage /> },
          { path: "guidelines/new", element: <GuidelineCreatePage /> },
          { path: "guidelines/run", element: <GuidelineRunPage /> },
          { path: "checks", element: <Navigate to="/guidelines" replace /> },
          { path: "results", element: <ResultsPage /> },
          { path: "audit", element: <AuditPage /> },
        ],
      },
      {
        path: "*",
        element: <NotFoundPage />,
      },
    ],
    { initialEntries: [path] },
  );

  render(<RouterProvider router={router} />);
}

describe("router", () => {
  it("renders dashboard content and primary navigation", () => {
    renderAt("/");

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Legal Document Intelligence",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("heading", { level: 2, name: "Dashboard" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Contracts" })).toHaveAttribute(
      "href",
      "/contracts",
    );
    expect(screen.getByRole("link", { name: "Guidelines" })).toHaveAttribute(
      "href",
      "/guidelines",
    );
    expect(
      screen.queryByRole("link", { name: "Results" }),
    ).not.toBeInTheDocument();
  });

  it("renders guidelines route from memory router navigation", () => {
    renderAt("/guidelines");

    expect(
      screen.getByRole("heading", { level: 2, name: "Guidelines" }),
    ).toBeVisible();
    expect(screen.getByText("Rules")).toBeVisible();
    expect(screen.getByText("Guideline Checks")).toBeVisible();
    expect(screen.getByRole("link", { name: "New Rule" })).toHaveAttribute(
      "href",
      "/guidelines/new",
    );
    expect(screen.getByRole("link", { name: "Run Guideline" })).toHaveAttribute(
      "href",
      "/guidelines/run",
    );
  });

  it("keeps contract comparison in the bulk action bar instead of row actions", async () => {
    renderAt("/contracts");

    expect(
      await screen.findByRole("button", { name: "Compare Selected" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Compare" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: "Delete actions" }),
    ).toBeVisible();
  });

  it("shows guideline check emoji and created date in the run list", () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "check-1",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Payment terms",
        },
      ]),
    );

    renderAt("/guidelines");

    expect(screen.getByText("✅")).toBeVisible();
    expect(screen.getByText("Payment terms")).toBeVisible();
    expect(screen.getByText(/Created 02\.01\.2026/)).toBeVisible();
  });

  it("shows rule type icons and badges in the guidelines rule list", () => {
    window.localStorage.setItem(
      "ldi.guidelineRules",
      JSON.stringify([
        {
          id: "rule-1",
          name: "Payment words",
          rule_type: "keyword_match",
          instructions: "Must contain: payment terms",
          required_terms: ["payment terms"],
          forbidden_terms: [],
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
        {
          id: "rule-2",
          name: "Estonian entity review",
          rule_type: "llm_review",
          instructions:
            "Review whether the company is clearly identified as an Estonian legal entity.",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
        {
          id: "rule-3",
          name: "Termination review",
          rule_type: "gemini_review",
          instructions:
            "Review whether the contract includes a termination for convenience right.",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );

    renderAt("/guidelines");

    expect(screen.getByText("🔎")).toBeVisible();
    expect(screen.getByText("📄")).toBeVisible();
    expect(screen.getByText("🧠")).toBeVisible();
    expect(screen.getByText("Strict keyword check")).toBeVisible();
    expect(screen.getByText("Lexical clause check")).toBeVisible();
    expect(screen.getByText("Gemini contract review")).toBeVisible();
  });

  it("shows checked contract names as clickable links in guideline results", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "check-1",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Payment terms",
        },
      ]),
    );

    vi.mocked(apiClient.getCheckRun).mockResolvedValueOnce({
      check_id: "check-1",
      status: "completed",
      check_type: "clause_presence",
      requested_at: "2026-01-02T03:04:05Z",
    });
    vi.mocked(apiClient.getCheckResults).mockResolvedValueOnce({
      check_id: "check-1",
      status: "completed",
      items: [
        {
          document_id: "doc-1",
          outcome: "missing",
          confidence: 0.87,
          summary: "Missing payment clause.",
        },
      ],
    });

    renderAt("/guidelines?checkId=check-1");

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Alpha" })).toHaveAttribute(
        "href",
        "/contracts/contract-1/edit",
      );
    });
  });

  it("applies outcome row highlighting in guideline results", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "check-1",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Payment terms",
        },
      ]),
    );

    vi.mocked(apiClient.getCheckRun).mockResolvedValueOnce({
      check_id: "check-1",
      status: "completed",
      check_type: "clause_presence",
      requested_at: "2026-01-02T03:04:05Z",
    });
    vi.mocked(apiClient.getCheckResults).mockResolvedValueOnce({
      check_id: "check-1",
      status: "completed",
      items: [
        {
          document_id: "doc-1",
          outcome: "match",
          confidence: 1,
          summary: "Clause found.",
        },
        {
          document_id: "doc-2",
          outcome: "missing",
          confidence: 0.42,
          summary: "Missing payment clause.",
        },
      ],
    });

    renderAt("/guidelines?checkId=check-1");

    await waitFor(() => {
      expect(screen.getByText("Clause found.").closest("tr")).toHaveClass(
        "guideline-result-row-match",
      );
      expect(
        screen.getByText("Missing payment clause.").closest("tr"),
      ).toHaveClass("guideline-result-row-missing");
    });
  });

  it("deletes a guideline rule from the list view", () => {
    window.localStorage.setItem(
      "ldi.guidelineRules",
      JSON.stringify([
        {
          id: "rule-1",
          name: "Payment terms",
          rule_type: "llm_review",
          instructions: "Confirm the contract clearly defines payment terms.",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );

    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    renderAt("/guidelines");

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    expect(screen.queryByText("Payment terms")).not.toBeInTheDocument();
    expect(
      JSON.parse(window.localStorage.getItem("ldi.guidelineRules") ?? "[]"),
    ).toEqual([]);

    confirmSpy.mockRestore();
  });

  it("deletes a single guideline check from the guidelines view", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "check-1",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Payment terms",
        },
      ]),
    );

    vi.mocked(apiClient.deleteCheckRun).mockResolvedValueOnce(undefined);
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    renderAt("/guidelines");

    fireEvent.click(
      screen.getByRole("button", { name: "Delete Payment terms" }),
    );

    await waitFor(() => {
      expect(apiClient.deleteCheckRun).toHaveBeenCalledWith("check-1");
    });
    await waitFor(() => {
      expect(screen.queryByText("Payment terms")).not.toBeInTheDocument();
    });

    expect(
      JSON.parse(window.localStorage.getItem("ldi.checkRuns") ?? "[]"),
    ).toEqual([]);
    confirmSpy.mockRestore();
  });

  it("removes a stale guideline check locally when delete returns not found", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "check-1",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Payment terms",
        },
      ]),
    );

    vi.mocked(apiClient.deleteCheckRun).mockRejectedValueOnce(
      new ApiError(404, {
        error: {
          code: "not_found",
          message: "check not found",
          retriable: false,
        },
      }),
    );
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    renderAt("/guidelines");

    fireEvent.click(
      screen.getByRole("button", { name: "Delete Payment terms" }),
    );

    await waitFor(() => {
      expect(screen.queryByText("Payment terms")).not.toBeInTheDocument();
    });

    expect(
      JSON.parse(window.localStorage.getItem("ldi.checkRuns") ?? "[]"),
    ).toEqual([]);
    confirmSpy.mockRestore();
  });

  it("bulk deletes selected guideline checks from the guidelines view", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "check-1",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Payment terms",
        },
        {
          check_id: "check-2",
          check_type: "llm_review",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-01T03:04:05Z",
          rule_name: "Risk review",
        },
      ]),
    );

    vi.mocked(apiClient.deleteCheckRuns).mockResolvedValueOnce(undefined);
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    renderAt("/guidelines");

    fireEvent.click(screen.getByLabelText("Select Payment terms"));
    fireEvent.click(screen.getByLabelText("Select Risk review"));
    fireEvent.click(
      screen.getByRole("button", { name: "Delete Selected (2)" }),
    );

    await waitFor(() => {
      expect(apiClient.deleteCheckRuns).toHaveBeenCalledWith({
        check_ids: ["check-1", "check-2"],
      });
    });
    await waitFor(() => {
      expect(screen.queryByText("Payment terms")).not.toBeInTheDocument();
      expect(screen.queryByText("Risk review")).not.toBeInTheDocument();
    });

    expect(
      JSON.parse(window.localStorage.getItem("ldi.checkRuns") ?? "[]"),
    ).toEqual([]);
    confirmSpy.mockRestore();
  });

  it("keeps guideline check order stable when selecting a run", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "check-1",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Payment terms",
        },
        {
          check_id: "check-2",
          check_type: "llm_review",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T03:04:05Z",
          rule_name: "Risk review",
        },
      ]),
    );

    vi.mocked(apiClient.getCheckRun).mockImplementationOnce(
      async (checkId: string) => ({
        check_id: checkId,
        status: "completed",
        check_type: checkId === "check-1" ? "clause_presence" : "llm_review",
        requested_at: "2026-01-02T03:04:05Z",
      }),
    );
    vi.mocked(apiClient.getCheckResults).mockResolvedValueOnce({
      check_id: "check-1",
      status: "completed",
      items: [],
    });

    renderAt("/guidelines");

    const getRunButtons = () =>
      Array.from(
        document.querySelectorAll<HTMLButtonElement>("button.run-item"),
      );
    const getRunLabels = () =>
      getRunButtons().map((button) => button.textContent?.trim() ?? "");

    expect(getRunLabels()).toEqual([
      expect.stringContaining("Payment terms"),
      expect.stringContaining("Risk review"),
    ]);

    fireEvent.click(getRunButtons()[0]);

    await waitFor(() => {
      expect(apiClient.getCheckRun).toHaveBeenCalledWith("check-1");
    });

    expect(getRunLabels()).toEqual([
      expect.stringContaining("Payment terms"),
      expect.stringContaining("Risk review"),
    ]);
  });

  it("renders dedicated guideline creation route", () => {
    renderAt("/guidelines/new");

    expect(
      screen.getByRole("heading", { level: 2, name: "New Guideline Rule" }),
    ).toBeVisible();
    expect(screen.getByLabelText("Rule Name")).toBeVisible();
    expect(screen.getByLabelText("Clause Text to Look For")).toBeVisible();
    expect(screen.getByText("How lexical clause check works")).toBeVisible();
    expect(
      screen.getByRole("option", { name: "Gemini contract review" }),
    ).toBeVisible();
    expect(
      screen.getByLabelText(
        "Run this rule automatically for every new contract.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Back to Guidelines" }),
    ).toHaveAttribute("href", "/guidelines");
  });

  it("renders dedicated batch contract import route", () => {
    renderAt("/contracts/import");

    expect(
      screen.getByRole("heading", { level: 2, name: "Batch Import Contracts" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Import Contracts" }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Back to Contracts" }),
    ).toHaveAttribute("href", "/contracts");
  });

  it("renders dedicated guideline execution route", () => {
    renderAt("/guidelines/run");

    expect(
      screen.getByRole("heading", { level: 2, name: "Run Guideline" }),
    ).toBeVisible();
  });

  it("runs a strict keyword guideline locally", async () => {
    window.localStorage.setItem(
      "ldi.guidelineRules",
      JSON.stringify([
        {
          id: "rule-keyword",
          name: "Keyword rule",
          rule_type: "keyword_match",
          instructions: "Must contain: payment terms",
          required_terms: ["payment terms"],
          forbidden_terms: [],
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );
    vi.mocked(apiClient.getDocumentText).mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "This contract includes payment terms.",
      has_text: true,
    });

    renderAt("/guidelines/run");

    fireEvent.click(
      await screen.findByRole("button", { name: "Run Guideline" }),
    );

    expect(
      await screen.findByRole("heading", { level: 2, name: "Guidelines" }),
    ).toBeVisible();
    expect(await screen.findByText("Strict keyword check")).toBeVisible();
    expect(screen.getByText("Flagged items: 0")).toBeVisible();
  });

  it("matches strict keywords regardless of case and collapsed whitespace", async () => {
    window.localStorage.setItem(
      "ldi.guidelineRules",
      JSON.stringify([
        {
          id: "rule-keyword",
          name: "Keyword rule",
          rule_type: "keyword_match",
          instructions: "Must contain: payment terms",
          required_terms: ["Payment Terms"],
          forbidden_terms: [],
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );
    vi.mocked(apiClient.getDocumentText).mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "This contract includes PAYMENT\n   TERMS in section 4.",
      has_text: true,
    });

    renderAt("/guidelines/run");

    fireEvent.click(
      await screen.findByRole("button", { name: "Run Guideline" }),
    );

    expect(
      await screen.findByRole("heading", { level: 2, name: "Guidelines" }),
    ).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: /Keyword ruleCreated/i }),
    );
    expect(screen.getByText("Flagged items: 0")).toBeVisible();
  });

  it("opens guideline creation from selected contracts", async () => {
    renderAt("/contracts");

    const alphaCheckbox = await screen.findByRole("checkbox", {
      name: "Select Alpha",
    });
    fireEvent.click(alphaCheckbox);

    fireEvent.click(
      await screen.findByRole("button", { name: "Check Guidelines" }),
    );

    expect(
      await screen.findByRole("heading", { level: 2, name: "Run Guideline" }),
    ).toBeVisible();
    await waitFor(() => {
      expect(screen.getByLabelText("Scope")).toHaveValue("selected");
    });
  });

  it("redirects legacy checks route to guidelines", async () => {
    renderAt("/checks");

    expect(
      await screen.findByRole("heading", { level: 2, name: "Guidelines" }),
    ).toBeVisible();
  });

  it("redirects legacy results route to guidelines", async () => {
    renderAt("/results?checkId=00000000-0000-4000-8000-000000000000");

    expect(
      await screen.findByRole("heading", { level: 2, name: "Guidelines" }),
    ).toBeVisible();
    expect(screen.getByText("Guideline Checks")).toBeVisible();
  });

  it("renders not found route for unknown paths", () => {
    renderAt("/missing-page");

    expect(
      screen.getByRole("heading", { level: 2, name: "Page not found" }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Go to dashboard" }),
    ).toHaveAttribute("href", "/");
  });
});
