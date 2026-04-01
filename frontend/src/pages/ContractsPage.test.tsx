import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ContractsPage } from "./ContractsPage";

const apiMocks = vi.hoisted(() => ({
  listContracts: vi.fn(),
  listDocuments: vi.fn(),
  deleteContract: vi.fn(),
  chatWithContractSearch: vi.fn(),
}));

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>();
  return {
    ...actual,
    apiClient: {
      ...actual.apiClient,
      listContracts: apiMocks.listContracts,
      listDocuments: apiMocks.listDocuments,
      deleteContract: apiMocks.deleteContract,
      chatWithContractSearch: apiMocks.chatWithContractSearch,
    },
  };
});

function renderPage() {
  const router = createMemoryRouter(
    [
      { path: "/contracts", element: <ContractsPage /> },
      {
        path: "/contracts/:contractId/edit",
        element: <div>Contract detail</div>,
      },
      { path: "/contracts/compare", element: <div>Compare</div> },
      { path: "/guidelines/run", element: <div>Guidelines run</div> },
    ],
    { initialEntries: ["/contracts"] },
  );

  render(<RouterProvider router={router} />);
}

describe("ContractsPage", () => {
  afterEach(() => {
    cleanup();
    Object.values(apiMocks).forEach((mockFn) => mockFn.mockReset());
  });

  it("asks the search-backed contracts assistant across all indexed contracts by default", async () => {
    apiMocks.listContracts.mockResolvedValue({
      items: [
        {
          id: "contract-1",
          name: "Alpha",
          language: "eng",
          file_count: 1,
          tags: ["vendor"],
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    });
    apiMocks.listDocuments.mockResolvedValue({
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
      ],
    });
    apiMocks.chatWithContractSearch.mockResolvedValue({
      answer: "Alpha mentions payment terms.",
      citations: [
        {
          document_id: "doc-1",
          contract_id: "contract-1",
          filename: "alpha.pdf",
          snippet_text: "payment terms apply",
          reason: "Matched payment language",
        },
      ],
      results: [
        {
          contract_id: "contract-1",
          document_id: "doc-1",
          contract_name: "Alpha",
          filename: "alpha.pdf",
          score: 0.97,
          snippet_text: "payment terms apply",
        },
      ],
    });

    renderPage();

    await screen.findByText("Alpha");
    fireEvent.click(
      screen.getByRole("button", { name: "Open contracts assistant" }),
    );
    fireEvent.change(
      screen.getByLabelText("Ask a question about the contracts list"),
      {
        target: { value: "Which contracts mention payment terms?" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));

    await waitFor(() => {
      expect(apiMocks.chatWithContractSearch).toHaveBeenCalledWith({
        messages: [
          {
            role: "user",
            content: "Which contracts mention payment terms?",
          },
        ],
        document_ids: undefined,
        limit: 3,
      });
    });

    expect(
      await screen.findByText("Alpha mentions payment terms."),
    ).toBeVisible();
    expect(screen.getByText(/No contracts selected/)).toBeVisible();
    expect(
      screen.getByRole("link", { name: /alpha.pdf: Matched payment language/ }),
    ).toHaveAttribute("href", "/contracts/contract-1/edit");
    expect(screen.getByText("I found 1 result.")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Show Results" }));
    expect(screen.getByText("payment terms apply")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Filter List By IDs" }));
    expect(
      screen.getByText(/Assistant filter active: 1 contract shown/),
    ).toBeVisible();
  });

  it("scopes assistant retrieval to selected contracts for deeper investigation", async () => {
    apiMocks.listContracts.mockResolvedValue({
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
          created_at: "2026-01-02T00:00:00Z",
          updated_at: "2026-01-02T00:00:00Z",
        },
      ],
    });
    apiMocks.listDocuments.mockResolvedValue({
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
          created_at: "2026-01-02T00:00:00Z",
          updated_at: "2026-01-02T00:00:00Z",
        },
      ],
    });
    apiMocks.chatWithContractSearch.mockResolvedValue({
      answer: "Strict search found a match in Alpha.",
      citations: [],
      results: [
        {
          contract_id: "contract-1",
          document_id: "doc-1",
          contract_name: "Alpha",
          filename: "alpha.pdf",
          score: 0.91,
          snippet_text: "late fee applies after 10 days",
        },
      ],
    });

    renderPage();

    await screen.findByText("Alpha");
    fireEvent.click(screen.getByLabelText("Select Alpha"));
    fireEvent.click(
      screen.getByRole("button", { name: "Open contracts assistant" }),
    );
    fireEvent.change(
      screen.getByLabelText("Ask a question about the contracts list"),
      {
        target: { value: "late fee" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));

    await waitFor(() => {
      expect(apiMocks.chatWithContractSearch).toHaveBeenCalledWith({
        messages: [{ role: "user", content: "late fee" }],
        document_ids: ["doc-1"],
        limit: 3,
      });
    });

    expect(
      screen.getByText("Only the selected contracts are searched."),
    ).toBeVisible();
  });
});
