import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ContractEditPage } from "./ContractEditPage";

const apiMocks = vi.hoisted(() => ({
  getContract: vi.fn(),
  getDocumentText: vi.fn(),
  getCheckRun: vi.fn(),
  getCheckResults: vi.fn(),
  deleteCheckRun: vi.fn(),
  deleteCheckRuns: vi.fn(),
  startClausePresenceCheck: vi.fn(),
  updateContract: vi.fn(),
  reorderContractFiles: vi.fn(),
  addContractFile: vi.fn(),
  getDocumentContentUrl: vi.fn(),
  chatWithContract: vi.fn(),
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
      deleteCheckRun: apiMocks.deleteCheckRun,
      deleteCheckRuns: apiMocks.deleteCheckRuns,
      startClausePresenceCheck: apiMocks.startClausePresenceCheck,
      updateContract: apiMocks.updateContract,
      reorderContractFiles: apiMocks.reorderContractFiles,
      addContractFile: apiMocks.addContractFile,
      getDocumentContentUrl: apiMocks.getDocumentContentUrl,
      chatWithContract: apiMocks.chatWithContract,
    },
  };
});

vi.stubGlobal(
  "confirm",
  vi.fn(() => true),
);

function renderPage({ strict = false }: { strict?: boolean } = {}) {
  const router = createMemoryRouter(
    [
      { path: "/contracts/:contractId/edit", element: <ContractEditPage /> },
      { path: "/guidelines", element: <div>Guidelines page</div> },
    ],
    { initialEntries: ["/contracts/contract-1/edit"] },
  );

  const content = <RouterProvider router={router} />;
  render(strict ? <React.StrictMode>{content}</React.StrictMode> : content);
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
          updated_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );
    window.localStorage.setItem(
      "ldi.pendingAutoGuidelineRuns",
      JSON.stringify([
        {
          contract_id: "contract-1",
          rule_id: "rule-auto",
          created_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "The agreement includes payment terms.",
      has_text: true,
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");

    renderPage();

    const filesHeading = await screen.findByRole("heading", { name: "Files" });
    const guidelineHeading = await screen.findByRole("heading", {
      name: "Guideline Checks",
    });

    expect(
      filesHeading.compareDocumentPosition(guidelineHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByText("Auto keyword")).toBeVisible();
      expect(screen.getByText("Flagged items: 0")).toBeVisible();
    });

    expect(
      JSON.parse(
        window.localStorage.getItem("ldi.pendingAutoGuidelineRuns") ?? "[]",
      ),
    ).toEqual([]);
  });

  it("does not duplicate automatic guideline checks in strict mode", async () => {
    vi.spyOn(Date, "now").mockReturnValue(1_775_000_000_000);
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
          updated_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );
    window.localStorage.setItem(
      "ldi.pendingAutoGuidelineRuns",
      JSON.stringify([
        {
          contract_id: "contract-1",
          rule_id: "rule-auto",
          created_at: "2026-01-01T00:00:00Z",
        },
      ]),
    );

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "The agreement includes payment terms.",
      has_text: true,
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");

    renderPage({ strict: true });

    await waitFor(() => {
      expect(screen.getAllByText("Auto keyword")).toHaveLength(1);
    });

    expect(
      JSON.parse(window.localStorage.getItem("ldi.checkRuns") ?? "[]"),
    ).toHaveLength(1);
    vi.mocked(Date.now).mockRestore();
  });

  it("deduplicates stored guideline checks with the same execution id", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "00000000-0000-4000-8000-000000000301",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "queued",
          requested_at: "2026-01-01T00:00:00Z",
          document_ids: ["doc-1"],
          rule_name: "Termination clause",
        },
        {
          check_id: "00000000-0000-4000-8000-000000000301",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-01T00:00:00Z",
          document_ids: ["doc-1"],
          rule_name: "Termination clause",
        },
      ]),
    );
    window.localStorage.setItem(
      "ldi.runResults",
      JSON.stringify({
        "00000000-0000-4000-8000-000000000301": {
          check_id: "00000000-0000-4000-8000-000000000301",
          status: "completed",
          items: [],
          updated_at: "2026-01-01T00:00:00Z",
        },
      }),
    );

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "Contract text",
      has_text: true,
    });
    apiMocks.getCheckRun.mockResolvedValue({
      check_id: "00000000-0000-4000-8000-000000000301",
      status: "completed",
      check_type: "clause_presence",
      requested_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getCheckResults.mockResolvedValue({
      check_id: "00000000-0000-4000-8000-000000000301",
      status: "completed",
      items: [],
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");

    renderPage();

    await waitFor(() => {
      expect(screen.getAllByText("Termination clause")).toHaveLength(1);
    });
  });

  it("collapses repeated runs of the same guideline into one contract-view item", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "00000000-0000-4000-8000-000000000401",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T00:00:00Z",
          document_ids: ["doc-1"],
          rule_id: "rule-termination",
          rule_name: "Termination clause",
          rule_type: "clause_presence",
          rule_text: "Find termination language",
        },
        {
          check_id: "00000000-0000-4000-8000-000000000402",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-01T00:00:00Z",
          document_ids: ["doc-1"],
          rule_id: "rule-termination",
          rule_name: "Termination clause",
          rule_type: "clause_presence",
          rule_text: "Find termination language",
        },
      ]),
    );
    window.localStorage.setItem(
      "ldi.runResults",
      JSON.stringify({
        "00000000-0000-4000-8000-000000000401": {
          check_id: "00000000-0000-4000-8000-000000000401",
          status: "completed",
          items: [],
          updated_at: "2026-01-02T00:00:00Z",
        },
        "00000000-0000-4000-8000-000000000402": {
          check_id: "00000000-0000-4000-8000-000000000402",
          status: "completed",
          items: [],
          updated_at: "2026-01-01T00:00:00Z",
        },
      }),
    );

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "Contract text",
      has_text: true,
    });
    apiMocks.getCheckRun.mockImplementation(async (checkId: string) => ({
      check_id: checkId,
      status: "completed",
      check_type: "clause_presence",
      requested_at:
        checkId === "00000000-0000-4000-8000-000000000401"
          ? "2026-01-02T00:00:00Z"
          : "2026-01-01T00:00:00Z",
    }));
    apiMocks.getCheckResults.mockResolvedValue({
      check_id: "00000000-0000-4000-8000-000000000401",
      status: "completed",
      items: [],
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");

    renderPage();

    await waitFor(() => {
      expect(screen.getAllByText("Termination clause")).toHaveLength(1);
    });
    expect(screen.getAllByRole("button", { name: "Delete" })).toHaveLength(1);
  });

  it("opens contract chat, asks a question, and highlights cited text", async () => {
    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "Either party may terminate on thirty days written notice.",
      has_text: true,
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");
    apiMocks.chatWithContract.mockResolvedValue({
      answer: "Yes, either party can terminate with thirty days notice.",
      citations: [
        {
          document_id: "doc-1",
          filename: "alpha.pdf",
          snippet_text:
            "Either party may terminate on thirty days written notice.",
          reason: "Termination clause",
        },
      ],
    });

    renderPage();

    await screen.findByText(
      "Either party may terminate on thirty days written notice.",
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Open contract assistant" }),
    );
    fireEvent.change(
      screen.getByLabelText("Ask a question about this contract"),
      {
        target: { value: "Can either party terminate?" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));

    await screen.findByText(
      "Yes, either party can terminate with thirty days notice.",
    );
    expect(apiMocks.chatWithContract).toHaveBeenCalledWith("contract-1", {
      messages: [{ role: "user", content: "Can either party terminate?" }],
    });
    expect(
      screen.getByRole("button", { name: /alpha\.pdf: termination clause/i }),
    ).toBeVisible();
    expect(
      screen
        .getByText("Either party may terminate on thirty days written notice.")
        .closest("mark"),
    ).toHaveClass("contract-chat-highlight");
  });

  it("deletes a single guideline check from the contract page", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "00000000-0000-4000-8000-000000000101",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-01T00:00:00Z",
          document_ids: ["doc-1"],
          rule_name: "Termination clause",
        },
      ]),
    );
    window.localStorage.setItem(
      "ldi.runResults",
      JSON.stringify({
        "00000000-0000-4000-8000-000000000101": {
          check_id: "00000000-0000-4000-8000-000000000101",
          status: "completed",
          items: [],
          updated_at: "2026-01-01T00:00:00Z",
        },
      }),
    );

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "Contract text",
      has_text: true,
    });
    apiMocks.getCheckRun.mockResolvedValue({
      check_id: "00000000-0000-4000-8000-000000000101",
      status: "completed",
      check_type: "clause_presence",
      requested_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getCheckResults.mockResolvedValue({
      check_id: "00000000-0000-4000-8000-000000000101",
      status: "completed",
      items: [],
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");
    apiMocks.deleteCheckRun.mockResolvedValue(undefined);

    renderPage();

    await screen.findByText("Termination clause");
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(apiMocks.deleteCheckRun).toHaveBeenCalledWith(
        "00000000-0000-4000-8000-000000000101",
      );
    });
    await waitFor(() => {
      expect(screen.queryByText("Termination clause")).not.toBeInTheDocument();
    });
  });

  it("bulk deletes selected guideline checks from the contract page", async () => {
    window.localStorage.setItem(
      "ldi.checkRuns",
      JSON.stringify([
        {
          check_id: "00000000-0000-4000-8000-000000000201",
          check_type: "clause_presence",
          execution_mode: "remote",
          status: "completed",
          requested_at: "2026-01-02T00:00:00Z",
          document_ids: ["doc-1"],
          rule_name: "Termination clause",
        },
        {
          check_id: "00000000-0000-4000-8000-000000000202",
          check_type: "llm_review",
          execution_mode: "local",
          status: "completed",
          requested_at: "2026-01-01T00:00:00Z",
          document_ids: ["doc-1"],
          rule_name: "Risk review",
        },
      ]),
    );

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          mime_type: "application/pdf",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.pdf",
      text: "Contract text",
      has_text: true,
    });
    apiMocks.getCheckRun.mockResolvedValue({
      check_id: "00000000-0000-4000-8000-000000000201",
      status: "completed",
      check_type: "clause_presence",
      requested_at: "2026-01-02T00:00:00Z",
    });
    apiMocks.getCheckResults.mockResolvedValue({
      check_id: "00000000-0000-4000-8000-000000000201",
      status: "completed",
      items: [],
    });
    apiMocks.getDocumentContentUrl.mockReturnValue("http://localhost/file.pdf");
    apiMocks.deleteCheckRun.mockResolvedValue(undefined);

    renderPage();

    await screen.findByText("Termination clause");
    fireEvent.click(screen.getByLabelText("Select Termination clause"));
    fireEvent.click(screen.getByLabelText("Select Risk review"));
    fireEvent.click(
      screen.getByRole("button", { name: "Delete Selected (2)" }),
    );

    await waitFor(() => {
      expect(apiMocks.deleteCheckRun).toHaveBeenCalledWith(
        "00000000-0000-4000-8000-000000000201",
      );
    });
    await waitFor(() => {
      expect(screen.queryByText("Termination clause")).not.toBeInTheDocument();
      expect(screen.queryByText("Risk review")).not.toBeInTheDocument();
    });
  });

  it("downloads the original file when the contract source is DOCX", async () => {
    const originalCreateElement = document.createElement.bind(document);
    const createElementSpy = vi.spyOn(document, "createElement");
    const appendChildSpy = vi.spyOn(document.body, "appendChild");
    const removeChildSpy = vi.spyOn(document.body, "removeChild");
    const downloadLink = originalCreateElement("a");
    const clickSpy = vi.spyOn(downloadLink, "click");

    createElementSpy.mockImplementation((tagName: string) => {
      if (tagName.toLowerCase() === "a") {
        return downloadLink;
      }
      return originalCreateElement(tagName);
    });

    apiMocks.getContract.mockResolvedValue({
      id: "contract-1",
      name: "Alpha",
      language: "eng",
      file_count: 1,
      files: [
        {
          id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.docx",
          mime_type:
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
          status: "indexed",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
    apiMocks.getDocumentText.mockResolvedValue({
      document_id: "doc-1",
      filename: "alpha.docx",
      text: "Contract text",
      has_text: true,
    });
    apiMocks.getDocumentContentUrl.mockReturnValue(
      "http://localhost/file.docx",
    );

    renderPage();

    await screen.findByText("Contract text");
    fireEvent.click(screen.getByRole("button", { name: "Original" }));

    expect(apiMocks.getDocumentContentUrl).toHaveBeenCalledWith("doc-1");
    expect(downloadLink.href).toBe("http://localhost/file.docx");
    expect(downloadLink.download).toBe("alpha.docx");
    expect(clickSpy).toHaveBeenCalledTimes(1);
    expect(appendChildSpy).toHaveBeenCalledWith(downloadLink);
    expect(removeChildSpy).toHaveBeenCalledWith(downloadLink);
    expect(
      screen.getByRole("heading", { name: "Contract Text" }),
    ).toBeVisible();

    clickSpy.mockRestore();
    createElementSpy.mockRestore();
    appendChildSpy.mockRestore();
    removeChildSpy.mockRestore();
  });
});
