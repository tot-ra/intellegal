from __future__ import annotations

import re
from typing import Any

from pydantic import BaseModel, Field

from .extraction import _clamp_confidence
from .gemini import GeminiReviewResult, GeminiReviewer
from .qdrant import QdrantService


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


class AnalysisPipeline:
    def __init__(self, *, qdrant: QdrantService, gemini_reviewer: GeminiReviewer | None = None) -> None:
        self._qdrant = qdrant
        self._gemini_reviewer = gemini_reviewer

    def analyze_clause(self, *, required_clause_text: str, document_ids: list[str] | None) -> AnalysisResult:
        docs = sorted({doc_id for doc_id in (document_ids or []) if doc_id})
        required_clause = required_clause_text.strip()
        if not docs:
            return AnalysisResult(items=[], diagnostics={"fallback": "no_documents"})

        required_tokens = _tokenize(required_clause)
        items: list[AnalysisResultItem] = []
        for document_id in docs:
            chunks = self._qdrant.get_document_chunks(document_id=document_id, limit=64)
            if not chunks:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="review",
                        confidence=0.35,
                        summary="No indexed chunks were found for this document; manual review is required.",
                    )
                )
                continue

            best_chunk: dict[str, Any] | None = None
            best_score = 0.0
            for chunk in chunks:
                text = str(chunk.get("text") or "")
                chunk_tokens = _tokenize(text)
                token_score = _token_overlap(required_tokens, chunk_tokens)
                phrase_match = 1.0 if required_clause and required_clause.lower() in text.lower() else 0.0
                score = max(token_score, phrase_match)
                if score > best_score:
                    best_score = score
                    best_chunk = chunk

            evidence = _build_evidence(best_chunk, best_score) if best_chunk and best_score >= 0.35 else []
            if best_score >= 0.7:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="match",
                        confidence=_clamp_confidence(0.72 + (best_score * 0.2)),
                        summary="Required clause appears to be present in the document.",
                        evidence=evidence,
                    )
                )
            elif best_score >= 0.35:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="review",
                        confidence=_clamp_confidence(0.45 + (best_score * 0.2)),
                        summary="Possible clause match found, but confidence is not high enough for an automatic decision.",
                        evidence=evidence,
                    )
                )
            else:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="missing",
                        confidence=0.2,
                        summary="No convincing evidence of the required clause was found in indexed chunks.",
                    )
                )

        return AnalysisResult(items=items, diagnostics={"document_count": len(docs), "strategy": "lexical_clause"})

    def analyze_company_name(
        self,
        *,
        old_company_name: str,
        new_company_name: str | None,
        document_ids: list[str] | None,
    ) -> AnalysisResult:
        docs = sorted({doc_id for doc_id in (document_ids or []) if doc_id})
        old_name = old_company_name.strip().lower()
        new_name = (new_company_name or "").strip().lower()
        if not docs:
            return AnalysisResult(items=[], diagnostics={"fallback": "no_documents"})

        items: list[AnalysisResultItem] = []
        for document_id in docs:
            chunks = self._qdrant.get_document_chunks(document_id=document_id, limit=64)
            if not chunks:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="review",
                        confidence=0.35,
                        summary="No indexed chunks were found for this document; manual review is required.",
                    )
                )
                continue

            old_hits = [chunk for chunk in chunks if old_name and old_name in str(chunk.get("text") or "").lower()]
            new_hits = [chunk for chunk in chunks if new_name and new_name in str(chunk.get("text") or "").lower()]

            if new_name:
                if new_hits and not old_hits:
                    items.append(
                        AnalysisResultItem(
                            document_id=document_id,
                            outcome="match",
                            confidence=0.9,
                            summary="New company name is present and old name was not found.",
                            evidence=_collect_name_evidence(old_hits, new_hits),
                        )
                    )
                elif old_hits and not new_hits:
                    items.append(
                        AnalysisResultItem(
                            document_id=document_id,
                            outcome="missing",
                            confidence=0.25,
                            summary="Old company name is still present and new name was not found.",
                            evidence=_collect_name_evidence(old_hits, new_hits),
                        )
                    )
                elif old_hits and new_hits:
                    items.append(
                        AnalysisResultItem(
                            document_id=document_id,
                            outcome="review",
                            confidence=0.6,
                            summary="Both old and new company names were found; manual confirmation is required.",
                            evidence=_collect_name_evidence(old_hits, new_hits),
                        )
                    )
                else:
                    items.append(
                        AnalysisResultItem(
                            document_id=document_id,
                            outcome="review",
                            confidence=0.45,
                            summary="Neither old nor new company name was found in indexed chunks.",
                        )
                    )
                continue

            if old_hits:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="match",
                        confidence=0.85,
                        summary="Old company name was found in the document.",
                        evidence=_collect_name_evidence(old_hits, []),
                    )
                )
            else:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="missing",
                        confidence=0.25,
                        summary="Old company name was not found in indexed chunks.",
                    )
                )

        return AnalysisResult(items=items, diagnostics={"document_count": len(docs), "strategy": "lexical_company"})

    def analyze_llm_review(
        self,
        *,
        instructions: str,
        documents: list[dict[str, str]] | None,
    ) -> AnalysisResult:
        if self._gemini_reviewer is None:
            raise RuntimeError("Gemini reviewer is not configured")

        prepared_documents = [doc for doc in (documents or []) if str(doc.get("document_id") or "").strip()]
        if not prepared_documents:
            return AnalysisResult(items=[], diagnostics={"fallback": "no_documents"})

        items: list[AnalysisResultItem] = []
        for document in prepared_documents:
            document_id = str(document.get("document_id") or "").strip()
            filename = str(document.get("filename") or "").strip()
            document_text = str(document.get("text") or "").strip()

            if not document_text:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="review",
                        confidence=0.2,
                        summary="No extracted text is available for this document; manual review is required.",
                    )
                )
                continue

            review = self._gemini_reviewer.review_document(
                instructions=instructions,
                filename=filename,
                document_text=document_text,
            )
            evidence = _evidence_from_snippets(review, document_text)
            items.append(
                AnalysisResultItem(
                    document_id=document_id,
                    outcome=review.outcome,
                    confidence=_clamp_confidence(review.confidence),
                    summary=review.summary,
                    evidence=evidence,
                )
            )

        return AnalysisResult(items=items, diagnostics={"document_count": len(prepared_documents), "strategy": "gemini_llm_review"})


def _build_evidence(chunk: dict[str, Any] | None, score: float) -> list[AnalysisEvidenceSnippet]:
    if not chunk:
        return []
    snippet = str(chunk.get("text") or "").strip()
    if not snippet:
        return []
    page_number = int(chunk.get("page_number") or 0)
    chunk_id = chunk.get("chunk_id")
    return [
        AnalysisEvidenceSnippet(
            snippet_text=snippet,
            page_number=page_number if page_number > 0 else 1,
            chunk_id=str(chunk_id) if chunk_id is not None else None,
            score=round(_clamp_confidence(score), 3),
        )
    ]


def _collect_name_evidence(old_hits: list[dict[str, Any]], new_hits: list[dict[str, Any]]) -> list[AnalysisEvidenceSnippet]:
    evidence: list[AnalysisEvidenceSnippet] = []
    selected = old_hits[:1] + new_hits[:1]
    for chunk in selected:
        evidence.extend(_build_evidence(chunk, 0.9))
    return evidence


def _tokenize(text: str) -> set[str]:
    return {token for token in re.findall(r"[a-z0-9]+", text.lower()) if len(token) > 1}


def _token_overlap(left: set[str], right: set[str]) -> float:
    if not left or not right:
        return 0.0
    return len(left.intersection(right)) / float(len(left))


def _evidence_from_snippets(review: GeminiReviewResult, document_text: str) -> list[AnalysisEvidenceSnippet]:
    pages = _split_pages(document_text)
    evidence: list[AnalysisEvidenceSnippet] = []
    for snippet in review.evidence_snippets:
        page_number = _find_page_number(pages, snippet)
        evidence.append(
            AnalysisEvidenceSnippet(
                snippet_text=snippet,
                page_number=page_number,
            )
        )
    return evidence


def _split_pages(document_text: str) -> list[str]:
    parts = [part.strip() for part in document_text.split("\f")]
    return [part for part in parts if part]


def _find_page_number(pages: list[str], snippet: str) -> int:
    if not pages:
        return 1
    normalized_snippet = " ".join(snippet.lower().split())
    if not normalized_snippet:
        return 1
    for index, page in enumerate(pages, start=1):
        normalized_page = " ".join(page.lower().split())
        if normalized_snippet in normalized_page:
            return index
    return 1
