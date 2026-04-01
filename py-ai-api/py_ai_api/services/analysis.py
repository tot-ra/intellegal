from __future__ import annotations

import re
from typing import Any

from ..models.analysis import (
    AnalysisEvidenceSnippet,
    AnalysisResult,
    AnalysisResultItem,
    ContractChatCitation,
    ContractChatResult,
)
from ..storage.qdrant import QdrantService
from ..utils.confidence import clamp_confidence
from .gemini import GeminiContractChatResult, GeminiReviewResult, GeminiReviewer


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
                        confidence=clamp_confidence(0.72 + (best_score * 0.2)),
                        summary="Required clause appears to be present in the document.",
                        evidence=evidence,
                    )
                )
            elif best_score >= 0.35:
                items.append(
                    AnalysisResultItem(
                        document_id=document_id,
                        outcome="review",
                        confidence=clamp_confidence(0.45 + (best_score * 0.2)),
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
                    confidence=clamp_confidence(review.confidence),
                    summary=review.summary,
                    evidence=evidence,
                )
            )

        return AnalysisResult(items=items, diagnostics={"document_count": len(prepared_documents), "strategy": "gemini_llm_review"})

    def answer_contract_question(
        self,
        *,
        contract_id: str,
        messages: list[dict[str, str]] | None,
        documents: list[dict[str, str]] | None,
    ) -> ContractChatResult:
        if self._gemini_reviewer is None:
            raise RuntimeError("Gemini reviewer is not configured")

        prepared_messages = [
            {
                "role": str(message.get("role") or "").strip().lower(),
                "content": str(message.get("content") or "").strip(),
            }
            for message in (messages or [])
            if str(message.get("content") or "").strip()
        ]
        prepared_documents = [
            {
                "document_id": str(document.get("document_id") or "").strip(),
                "filename": str(document.get("filename") or "").strip(),
                "text": str(document.get("text") or "").strip(),
            }
            for document in (documents or [])
            if str(document.get("document_id") or "").strip() and str(document.get("text") or "").strip()
        ]
        if not prepared_messages:
            return ContractChatResult(answer="I need a question before I can review the contract.", citations=[])
        if not prepared_documents:
            return ContractChatResult(answer="No extracted contract text is available yet.", citations=[])

        response: GeminiContractChatResult = self._gemini_reviewer.answer_contract_question(
            contract_id=contract_id,
            messages=prepared_messages,
            documents=prepared_documents,
        )
        citations = [
            ContractChatCitation(
                document_id=citation.document_id,
                snippet_text=citation.snippet_text,
                reason=citation.reason or None,
            )
            for citation in response.citations
        ]
        return ContractChatResult(answer=response.answer, citations=citations)


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
            score=round(clamp_confidence(score), 3),
        )
    ]


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
