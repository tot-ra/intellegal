import { ApiError, apiClient } from "../api/client";

export const SUPPORTED_CONTRACT_MIME_TYPES = ["application/pdf", "image/jpeg", "image/png"] as const;
export type SupportedContractMimeType = (typeof SUPPORTED_CONTRACT_MIME_TYPES)[number];

export async function toBase64(file: File): Promise<string> {
  const buffer = await file.arrayBuffer();
  let binary = "";
  const bytes = new Uint8Array(buffer);

  for (const value of bytes) {
    binary += String.fromCharCode(value);
  }

  return btoa(binary);
}

export function deriveContractNameFromFile(file: File | undefined): string {
  if (!file) {
    return "";
  }

  const trimmedName = file.name.trim();
  if (trimmedName.length === 0) {
    return "";
  }

  const extensionIndex = trimmedName.lastIndexOf(".");
  if (extensionIndex <= 0) {
    return trimmedName;
  }

  return trimmedName.slice(0, extensionIndex).trim() || trimmedName;
}

export function parseTagsInput(tagsInput: string): string[] {
  return Array.from(
    new Set(
      tagsInput
        .split(",")
        .map((tag) => tag.trim())
        .filter((tag) => tag.length > 0)
    )
  );
}

export function isSupportedContractFile(file: File): file is File & { type: SupportedContractMimeType } {
  return SUPPORTED_CONTRACT_MIME_TYPES.includes(file.type as SupportedContractMimeType);
}

export function isRecoverableProcessingError(err: unknown): err is ApiError {
  return (
    err instanceof ApiError &&
    err.code === "upstream_unavailable" &&
    (err.message === "failed to extract document text" || err.message === "failed to index document text")
  );
}

export async function createSingleFileContract(file: File, tags: string[]) {
  const contractName = deriveContractNameFromFile(file);
  if (!contractName) {
    throw new Error(`Contract name could not be derived from "${file.name}".`);
  }
  if (!isSupportedContractFile(file)) {
    throw new Error(`"${file.name}" is not supported. Only PDF, JPEG, and PNG files are supported.`);
  }

  const contract = await apiClient.createContract(
    {
      name: contractName,
      source_type: "upload",
      tags: tags.length > 0 ? tags : undefined
    },
    { idempotencyKey: globalThis.crypto?.randomUUID?.() ?? `contract-${Date.now()}-${file.name}` }
  );

  const contentBase64 = await toBase64(file);

  try {
    await apiClient.addContractFile(
      contract.id,
      {
        filename: file.name,
        mime_type: file.type,
        source_type: "upload",
        tags: tags.length > 0 ? tags : undefined,
        content_base64: contentBase64
      },
      { idempotencyKey: globalThis.crypto?.randomUUID?.() ?? `upload-${Date.now()}-${file.name}` }
    );

    return { contract, processingIssue: false };
  } catch (err) {
    if (isRecoverableProcessingError(err)) {
      return { contract, processingIssue: true };
    }
    throw err;
  }
}
