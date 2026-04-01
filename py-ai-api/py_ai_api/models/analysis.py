from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class AnalysisEvidenceSnippet(BaseModel):
    snippet_text: str
    page_number: int
    chunk_id: str | None = None
    score: float | None = None


class AnalysisResultItem(BaseModel):
    document_id: str
    outcome: str
    confidence: float
    summary: str | None = None
    evidence: list[AnalysisEvidenceSnippet] = Field(default_factory=list)


class AnalysisResult(BaseModel):
    items: list[AnalysisResultItem]
    diagnostics: dict[str, Any] = Field(default_factory=dict)


class ContractChatCitation(BaseModel):
    document_id: str
    snippet_text: str
    reason: str | None = None


class ContractChatResult(BaseModel):
    answer: str
    citations: list[ContractChatCitation] = Field(default_factory=list)
