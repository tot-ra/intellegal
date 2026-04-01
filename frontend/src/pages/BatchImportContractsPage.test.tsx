import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { BatchImportContractsPage } from "./BatchImportContractsPage";

const apiMocks = vi.hoisted(() => ({
  createContract: vi.fn(),
  addContractFile: vi.fn(),
}));

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>();
  return {
    ...actual,
    apiClient: {
      ...actual.apiClient,
      createContract: apiMocks.createContract,
      addContractFile: apiMocks.addContractFile,
    },
  };
});

function renderPage() {
  const router = createMemoryRouter(
    [
      { path: "/", element: <BatchImportContractsPage /> },
      {
        path: "/contracts/:contractId/edit",
        element: <div>Contract edit page</div>,
      },
    ],
    { initialEntries: ["/"] },
  );

  render(<RouterProvider router={router} />);
}

describe("BatchImportContractsPage", () => {
  beforeEach(() => {
    apiMocks.createContract
      .mockResolvedValueOnce({
        id: "contract-1",
        name: "alpha",
        language: "eng",
        file_count: 0,
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      })
      .mockResolvedValueOnce({
        id: "contract-2",
        name: "beta",
        language: "eng",
        file_count: 0,
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      });
    apiMocks.addContractFile
      .mockResolvedValueOnce({
        id: "doc-1",
        contract_id: "contract-1",
        filename: "alpha.pdf",
        mime_type: "application/pdf",
        status: "indexed",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      })
      .mockRejectedValueOnce(
        new ApiError(503, {
          error: {
            code: "upstream_unavailable",
            message: "failed to extract document text",
            retriable: true,
          },
        }),
      );
  });

  afterEach(() => {
    cleanup();
    apiMocks.createContract.mockReset();
    apiMocks.addContractFile.mockReset();
    window.localStorage.clear();
  });

  it("creates one contract per file and keeps going through processing warnings", async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText("Tags"), {
      target: { value: "MSA, Vendor" },
    });
    const fileInput = document.querySelector('input[type="file"]');
    if (!(fileInput instanceof HTMLInputElement)) {
      throw new Error("Expected file input to be present.");
    }

    fireEvent.change(fileInput, {
      target: {
        files: [
          new File(["pdf-content"], "alpha.pdf", { type: "application/pdf" }),
          new File(["pdf-content"], "beta.pdf", { type: "application/pdf" }),
        ],
      },
    });

    fireEvent.click(screen.getByRole("button", { name: "Import Contracts" }));

    await waitFor(() => {
      expect(apiMocks.createContract).toHaveBeenNthCalledWith(
        1,
        expect.objectContaining({
          name: "alpha",
          source_type: "upload",
          tags: ["MSA", "Vendor"],
        }),
        expect.any(Object),
      );
    });

    expect(apiMocks.createContract).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        name: "beta",
        source_type: "upload",
        tags: ["MSA", "Vendor"],
      }),
      expect.any(Object),
    );
    expect(apiMocks.addContractFile).toHaveBeenNthCalledWith(
      1,
      "contract-1",
      expect.objectContaining({
        filename: "alpha.pdf",
        mime_type: "application/pdf",
        tags: ["MSA", "Vendor"],
      }),
      expect.any(Object),
    );
    expect(apiMocks.addContractFile).toHaveBeenNthCalledWith(
      2,
      "contract-2",
      expect.objectContaining({
        filename: "beta.pdf",
        mime_type: "application/pdf",
        tags: ["MSA", "Vendor"],
      }),
      expect.any(Object),
    );

    expect(await screen.findByText("Contract created.")).toBeVisible();
    expect(
      await screen.findByText(
        "Contract created, but text processing needs attention.",
      ),
    ).toBeVisible();
  });
});
