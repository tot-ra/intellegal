import { expect, test } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const fixturePath = path.resolve(
  __dirname,
  "../../tests/Founders-Agreement_updated-062023.docx",
);
const fixtureName = path.basename(fixturePath);
const contractName = "Founders-Agreement_updated-062023";
const contractId = "contract-e2e-1";
const documentId = "document-e2e-1";
const timestamp = "2026-04-01T10:00:00Z";
const extractedText = "Founders agreement extracted text";

test("uploads a contract from the shared tests fixtures", async ({ page }) => {
  let createContractBody: Record<string, unknown> | null = null;
  let addContractFileBody: Record<string, unknown> | null = null;

  await page.route("http://localhost:8080/api/v1/contracts", async (route) => {
    if (route.request().method() !== "POST") {
      await route.fallback();
      return;
    }

    createContractBody = route.request().postDataJSON() as Record<
      string,
      unknown
    >;

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: contractId,
        name: contractName,
        language: "eng",
        source_type: "upload",
        file_count: 1,
        created_at: timestamp,
        updated_at: timestamp,
      }),
    });
  });

  await page.route(
    `http://localhost:8080/api/v1/contracts/${contractId}/files`,
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }

      addContractFileBody = route.request().postDataJSON() as Record<
        string,
        unknown
      >;

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          id: documentId,
          contract_id: contractId,
          filename: fixtureName,
          mime_type:
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
          status: "indexed",
          created_at: timestamp,
          updated_at: timestamp,
        }),
      });
    },
  );

  await page.route(
    `http://localhost:8080/api/v1/contracts/${contractId}`,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          id: contractId,
          name: contractName,
          language: "eng",
          source_type: "upload",
          file_count: 1,
          tags: [],
          files: [
            {
              id: documentId,
              contract_id: contractId,
              filename: fixtureName,
              mime_type:
                "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
              status: "indexed",
              created_at: timestamp,
              updated_at: timestamp,
            },
          ],
          created_at: timestamp,
          updated_at: timestamp,
        }),
      });
    },
  );

  await page.route(
    /http:\/\/localhost:8080\/api\/v1\/contracts\?.*/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [
            {
              id: contractId,
              name: contractName,
              language: "eng",
              source_type: "upload",
              file_count: 1,
              tags: [],
              created_at: timestamp,
              updated_at: timestamp,
            },
          ],
          limit: 200,
          offset: 0,
          total: 1,
        }),
      });
    },
  );

  await page.route(
    /http:\/\/localhost:8080\/api\/v1\/documents\?.*/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [
            {
              id: documentId,
              contract_id: contractId,
              filename: fixtureName,
              mime_type:
                "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
              source_type: "upload",
              status: "indexed",
              created_at: timestamp,
              updated_at: timestamp,
            },
          ],
          limit: 200,
          offset: 0,
          total: 1,
        }),
      });
    },
  );

  await page.route(
    `http://localhost:8080/api/v1/documents/${documentId}/text`,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          document_id: documentId,
          filename: fixtureName,
          text: extractedText,
          has_text: true,
        }),
      });
    },
  );

  await page.goto("/contracts/new");

  await page.locator('input[type="file"]').setInputFiles(fixturePath);

  await expect(
    page.locator(".selected-upload-name", { hasText: fixtureName }),
  ).toBeVisible();
  await expect(page.getByText(`Upload order: ${fixtureName}`)).toBeVisible();

  await page.getByRole("button", { name: "Create Contract" }).click();

  await expect(page).toHaveURL(
    new RegExp(`/contracts/${contractId}/edit\\?notice=Contract%20created`),
  );
  await expect(
    page.getByRole("heading", { name: "Contract Details" }),
  ).toBeVisible();
  await expect(
    page.getByText("Contract created. Indexing status will update here."),
  ).toBeVisible();
  await expect(
    page.locator(".file-name", { hasText: fixtureName }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Contract Text" }),
  ).toBeVisible();
  await expect(page.getByText(extractedText)).toBeVisible();

  await page.getByRole("link", { name: "Back to Contracts" }).click();

  await expect(page).toHaveURL("/contracts");
  await expect(page.getByRole("heading", { name: "Contracts" })).toBeVisible();
  const contractRow = page.locator("tbody tr", {
    has: page.getByRole("link", { name: contractName }),
  });
  await expect(contractRow).toBeVisible();
  await expect(contractRow.getByRole("cell").nth(2)).toHaveText("1");

  await page.getByRole("link", { name: contractName }).click();

  await expect(page).toHaveURL(`/contracts/${contractId}/edit`);
  await expect(
    page.getByRole("heading", { name: "Contract Text" }),
  ).toBeVisible();
  await expect(
    page.locator(".file-name", { hasText: fixtureName }),
  ).toBeVisible();
  await expect(page.getByText(extractedText)).toBeVisible();

  expect(createContractBody).toMatchObject({
    name: contractName,
    language: "eng",
    source_type: "upload",
  });
  expect(addContractFileBody).toMatchObject({
    filename: fixtureName,
    mime_type:
      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    source_type: "upload",
  });
  expect(typeof addContractFileBody?.content_base64).toBe("string");
  expect(
    (addContractFileBody?.content_base64 as string).length,
  ).toBeGreaterThan(100);
});
