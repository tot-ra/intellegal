from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel

from ..models.indexing import IndexPageInput


class ExtractJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    document_id: str
    storage_uri: str
    mime_type: str | None = None


class AcceptedJobResponse(BaseModel):
    job_id: str
    status: str
    job_type: str
    result: dict[str, Any] | None = None


class IndexJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    document_id: str
    version_checksum: str | None = None
    reindex: bool = False
    extracted_text: str | None = None
    pages: list[IndexPageInput] | None = None
    source_uri: str | None = None


class AnalyzeClauseJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    check_id: str
    document_ids: list[str] | None = None
    required_clause_text: str
    context_hint: str | None = None


class AnalyzeLLMReviewDocument(BaseModel):
    document_id: str
    filename: str | None = None
    text: str | None = None


class AnalyzeLLMReviewJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    check_id: str
    document_ids: list[str] | None = None
    instructions: str
    documents: list[AnalyzeLLMReviewDocument] | None = None


class SearchSectionsJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    query_text: str
    document_ids: list[str] | None = None
    limit: int = 10
    strategy: Literal["semantic", "strict"] = "semantic"
    result_mode: Literal["sections", "contracts"] = "sections"


class ContractChatMessage(BaseModel):
    role: Literal["user", "assistant"]
    content: str


class ContractChatDocument(BaseModel):
    document_id: str
    filename: str | None = None
    text: str | None = None


class ContractChatJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    contract_id: str
    messages: list[ContractChatMessage]
    documents: list[ContractChatDocument] | None = None
