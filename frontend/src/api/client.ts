import { appConfig } from "../config/env";

export type DocumentStatus = "ingested" | "processing" | "indexed" | "failed";
export type CheckRunStatus = "queued" | "running" | "completed" | "failed";
export type CheckType = "clause_presence" | "company_name" | "llm_review";

export type ErrorEnvelope = {
  error: {
    code: string;
    message: string;
    retriable: boolean;
    details?: Record<string, unknown>;
  };
};

export type CreateDocumentRequest = {
  contract_id?: string;
  source_type?: "repository" | "upload" | "api";
  source_ref?: string;
  filename: string;
  mime_type:
    | "application/pdf"
    | "image/jpeg"
    | "image/png"
    | "application/vnd.openxmlformats-officedocument.wordprocessingml.document";
  content_base64: string;
  tags?: string[];
};

export type DocumentResponse = {
  id: string;
  contract_id?: string;
  source_type?: string;
  source_ref?: string;
  tags?: string[];
  filename: string;
  mime_type: string;
  status: DocumentStatus;
  checksum?: string;
  created_at: string;
  updated_at: string;
};

export type DocumentListResponse = {
  items: DocumentResponse[];
  limit: number;
  offset: number;
  total: number;
};

export type CreateContractRequest = {
  name: string;
  source_type?: "repository" | "upload" | "api";
  source_ref?: string;
  tags?: string[];
};

export type UpdateContractRequest = {
  name?: string;
  tags?: string[];
};

export type ContractResponse = {
  id: string;
  name: string;
  source_type?: string;
  source_ref?: string;
  tags?: string[];
  file_count: number;
  files?: DocumentResponse[];
  created_at: string;
  updated_at: string;
};

export type ContractListResponse = {
  items: ContractResponse[];
  limit: number;
  offset: number;
  total: number;
};

export type DocumentTextResponse = {
  document_id: string;
  filename: string;
  text: string;
  has_text: boolean;
};

export type ClauseCheckRequest = {
  document_ids?: string[];
  required_clause_text: string;
  context_hint?: string;
};

export type CompanyNameCheckRequest = {
  document_ids?: string[];
  old_company_name: string;
  new_company_name?: string;
};

export type LLMReviewCheckRequest = {
  document_ids?: string[];
  instructions: string;
};

export type CheckAcceptedResponse = {
  check_id: string;
  status: CheckRunStatus;
  check_type: CheckType;
};

export type CheckRunResponse = {
  check_id: string;
  status: CheckRunStatus;
  check_type: CheckType;
  requested_at: string;
  finished_at?: string;
  failure_reason?: string;
};

export type EvidenceSnippet = {
  snippet_text: string;
  page_number: number;
  chunk_id?: string;
  score?: number;
};

export type CheckResultItem = {
  document_id: string;
  outcome: "match" | "missing" | "review";
  confidence: number;
  summary?: string;
  evidence?: EvidenceSnippet[];
};

export type CheckResultsResponse = {
  check_id: string;
  status: CheckRunStatus;
  items: CheckResultItem[];
};

export type DeleteChecksRequest = {
  check_ids: string[];
};

export type ContractSearchRequest = {
  query_text: string;
  strategy?: "semantic" | "strict";
  result_mode?: "sections" | "contracts";
  document_ids?: string[];
  limit?: number;
};

export type ContractSearchResultItem = {
  document_id: string;
  contract_id?: string;
  filename: string;
  page_number: number;
  chunk_id?: string;
  score: number;
  snippet_text: string;
};

export type ContractSearchResponse = {
  items: ContractSearchResultItem[];
};

export type ContractChatMessage = {
  role: "user" | "assistant";
  content: string;
};

export type ContractChatRequest = {
  messages: ContractChatMessage[];
};

export type ContractChatCitation = {
  document_id: string;
  filename?: string;
  snippet_text: string;
  reason?: string;
};

export type ContractChatResponse = {
  answer: string;
  citations: ContractChatCitation[];
};

export type RequestOptions = {
  idempotencyKey?: string;
  signal?: AbortSignal;
};

export class ApiError extends Error {
  readonly status: number;
  readonly retriable: boolean;
  readonly code: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, payload: ErrorEnvelope) {
    super(payload.error.message);
    this.name = "ApiError";
    this.status = status;
    this.retriable = payload.error.retriable;
    this.code = payload.error.code;
    this.details = payload.error.details;
  }
}

export type FetchLike = typeof fetch;

export class ApiClient {
  private readonly baseUrl: string;
  private readonly fetchFn: FetchLike;

  constructor(baseUrl = appConfig.apiBaseUrl, fetchFn?: FetchLike) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");

    // Use the native global fetch call path by default so browser receiver semantics stay intact.
    if (fetchFn === undefined || fetchFn === globalThis.fetch) {
      this.fetchFn = ((input: RequestInfo | URL, init?: RequestInit) =>
        globalThis.fetch(input, init)) as FetchLike;
      return;
    }

    // Custom fetch implementations are treated as plain functions.
    this.fetchFn = fetchFn;
  }

  private invokeFetch(
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> {
    const receiver =
      typeof window !== "undefined" && typeof window.fetch === "function"
        ? window
        : globalThis;

    return Reflect.apply(this.fetchFn, receiver, [
      input,
      init,
    ]) as Promise<Response>;
  }

  createDocument(body: CreateDocumentRequest, options?: RequestOptions) {
    return this.request<DocumentResponse>(
      "POST",
      "/api/v1/documents",
      body,
      options,
    );
  }

  createContract(body: CreateContractRequest, options?: RequestOptions) {
    return this.request<ContractResponse>(
      "POST",
      "/api/v1/contracts",
      body,
      options,
    );
  }

  listContracts(params?: { limit?: number; offset?: number }) {
    const query = new URLSearchParams();
    if (params?.limit !== undefined) query.set("limit", String(params.limit));
    if (params?.offset !== undefined)
      query.set("offset", String(params.offset));
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request<ContractListResponse>(
      "GET",
      `/api/v1/contracts${suffix}`,
    );
  }

  getContract(contractId: string) {
    return this.request<ContractResponse>(
      "GET",
      `/api/v1/contracts/${encodeURIComponent(contractId)}`,
    );
  }

  updateContract(contractId: string, body: UpdateContractRequest) {
    return this.request<ContractResponse>(
      "PATCH",
      `/api/v1/contracts/${encodeURIComponent(contractId)}`,
      body,
    );
  }

  deleteContract(contractId: string) {
    return this.request<void>(
      "DELETE",
      `/api/v1/contracts/${encodeURIComponent(contractId)}`,
    );
  }

  addContractFile(
    contractId: string,
    body: CreateDocumentRequest,
    options?: RequestOptions,
  ) {
    return this.request<DocumentResponse>(
      "POST",
      `/api/v1/contracts/${encodeURIComponent(contractId)}/files`,
      body,
      options,
    );
  }

  reorderContractFiles(contractId: string, fileIds: string[]) {
    return this.request<ContractResponse>(
      "PATCH",
      `/api/v1/contracts/${encodeURIComponent(contractId)}/files/order`,
      { file_ids: fileIds },
    );
  }

  listDocuments(params?: {
    status?: DocumentStatus;
    source_type?: "repository" | "upload" | "api";
    tags?: string[];
    limit?: number;
    offset?: number;
  }) {
    const query = new URLSearchParams();
    if (params?.status) query.set("status", params.status);
    if (params?.source_type) query.set("source_type", params.source_type);
    for (const tag of params?.tags ?? []) {
      query.append("tag", tag);
    }
    if (params?.limit !== undefined) query.set("limit", String(params.limit));
    if (params?.offset !== undefined)
      query.set("offset", String(params.offset));

    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return this.request<DocumentListResponse>(
      "GET",
      `/api/v1/documents${suffix}`,
    );
  }

  getDocument(documentId: string) {
    return this.request<DocumentResponse>(
      "GET",
      `/api/v1/documents/${encodeURIComponent(documentId)}`,
    );
  }

  getDocumentText(documentId: string) {
    return this.request<DocumentTextResponse>(
      "GET",
      `/api/v1/documents/${encodeURIComponent(documentId)}/text`,
    );
  }

  getDocumentContentUrl(documentId: string) {
    return `${this.baseUrl}/api/v1/documents/${encodeURIComponent(documentId)}/content`;
  }

  deleteDocument(documentId: string) {
    return this.request<void>(
      "DELETE",
      `/api/v1/documents/${encodeURIComponent(documentId)}`,
    );
  }

  startClausePresenceCheck(body: ClauseCheckRequest, options?: RequestOptions) {
    return this.request<CheckAcceptedResponse>(
      "POST",
      "/api/v1/guidelines/clause-presence",
      body,
      options,
    );
  }

  startCompanyNameCheck(
    body: CompanyNameCheckRequest,
    options?: RequestOptions,
  ) {
    return this.request<CheckAcceptedResponse>(
      "POST",
      "/api/v1/guidelines/company-name",
      body,
      options,
    );
  }

  startLLMReviewCheck(body: LLMReviewCheckRequest, options?: RequestOptions) {
    return this.request<CheckAcceptedResponse>(
      "POST",
      "/api/v1/guidelines/llm-review",
      body,
      options,
    );
  }

  getCheckRun(checkId: string) {
    return this.request<CheckRunResponse>(
      "GET",
      `/api/v1/guidelines/${encodeURIComponent(checkId)}`,
    );
  }

  getCheckResults(checkId: string) {
    return this.request<CheckResultsResponse>(
      "GET",
      `/api/v1/guidelines/${encodeURIComponent(checkId)}/results`,
    );
  }

  deleteCheckRun(checkId: string) {
    return this.request<void>(
      "DELETE",
      `/api/v1/guidelines/${encodeURIComponent(checkId)}`,
    );
  }

  deleteCheckRuns(body: DeleteChecksRequest) {
    return this.request<void>("DELETE", "/api/v1/guidelines", body);
  }

  searchContractSections(
    body: ContractSearchRequest,
    options?: RequestOptions,
  ) {
    return this.request<ContractSearchResponse>(
      "POST",
      "/api/v1/contracts/search",
      body,
      options,
    );
  }

  chatWithContract(
    contractId: string,
    body: ContractChatRequest,
    options?: RequestOptions,
  ) {
    return this.request<ContractChatResponse>(
      "POST",
      `/api/v1/contracts/${encodeURIComponent(contractId)}/chat`,
      body,
      options,
    );
  }

  private async request<T>(
    method: "GET" | "POST" | "PATCH" | "DELETE",
    path: string,
    body?: unknown,
    options?: RequestOptions,
  ): Promise<T> {
    const headers: Record<string, string> = {
      Accept: "application/json",
    };

    if (options?.idempotencyKey) {
      headers["Idempotency-Key"] = options.idempotencyKey;
    }

    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
    }

    const requestUrl = `${this.baseUrl}${path}`;
    const requestInit: RequestInit = {
      method,
      headers,
      signal: options?.signal,
      body: body === undefined ? undefined : JSON.stringify(body),
    };

    let response: Response;
    try {
      response = await this.invokeFetch(requestUrl, requestInit);
    } catch (error) {
      const fallbackReceiver =
        typeof window !== "undefined" && typeof window.fetch === "function"
          ? window
          : globalThis;

      if (
        error instanceof TypeError &&
        /illegal invocation/i.test(error.message) &&
        typeof fallbackReceiver.fetch === "function"
      ) {
        response = (await Reflect.apply(
          fallbackReceiver.fetch,
          fallbackReceiver,
          [requestUrl, requestInit],
        )) as Response;
      } else {
        throw error;
      }
    }

    if (!response.ok) {
      const payload = await this.readErrorEnvelope(response);
      throw new ApiError(response.status, payload);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return (await response.json()) as T;
  }

  private async readErrorEnvelope(response: Response): Promise<ErrorEnvelope> {
    const contentType = response.headers.get("Content-Type") ?? "";
    if (contentType.toLowerCase().includes("application/json")) {
      try {
        return (await response.json()) as ErrorEnvelope;
      } catch {
        // Fall through to the plain-text fallback below.
      }
    }

    let message = `Request failed with status ${response.status}`;
    try {
      const text = (await response.text()).trim();
      if (text) {
        message = text;
      }
    } catch {
      // Use the generic fallback message.
    }

    return {
      error: {
        code: "http_error",
        message,
        retriable: response.status >= 500,
      },
    };
  }
}

export const apiClient = new ApiClient();
