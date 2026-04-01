from __future__ import annotations

import pytest

from py_ai_api.services.analysis import AnalysisPipeline
from py_ai_api.services.gemini import GeminiContractChatCitation, GeminiContractChatResult, GeminiReviewResult

pytestmark = pytest.mark.unit


class _FakeQdrant:
    def __init__(self, chunks_by_document: dict[str, list[dict[str, object]]]) -> None:
        self.chunks_by_document = chunks_by_document

    def get_document_chunks(self, *, document_id: str, limit: int = 64) -> list[dict[str, object]]:
        return self.chunks_by_document.get(document_id, [])[:limit]


def test_clause_analysis_returns_match_with_evidence() -> None:
    pipeline = AnalysisPipeline(
        qdrant=_FakeQdrant(
            {
                "doc-1": [
                    {
                        "chunk_id": 1,
                        "page_number": 2,
                        "text": "The agreement must include payment terms and invoice details.",
                    }
                ]
            }
        )
    )

    result = pipeline.analyze_clause(
        required_clause_text="must include payment terms",
        document_ids=["doc-1"],
    )

    assert len(result.items) == 1
    assert result.items[0].outcome == "match"
    assert len(result.items[0].evidence) == 1
    assert result.items[0].evidence[0].page_number == 2


class _FakeGeminiReviewer:
    def review_document(self, *, instructions: str, filename: str, document_text: str) -> GeminiReviewResult:
        assert "termination" in instructions
        assert filename == "msa.pdf"
        assert "Either party may terminate" in document_text
        return GeminiReviewResult(
            outcome="review",
            confidence=0.81,
            summary="Termination right exists but needs legal review.",
            evidence_snippets=["Either party may terminate on thirty days written notice."],
        )

    def answer_contract_question(
        self,
        *,
        contract_id: str,
        messages: list[dict[str, str]],
        documents: list[dict[str, str]],
    ) -> GeminiContractChatResult:
        assert contract_id == "contract-1"
        assert messages == [{"role": "user", "content": "What is the termination notice period?"}]
        assert documents == [
            {
                "document_id": "doc-1",
                "filename": "msa.pdf",
                "text": "Either party may terminate on thirty days written notice.",
            }
        ]
        return GeminiContractChatResult(
            answer="The contract allows termination on thirty days written notice.",
            citations=[
                GeminiContractChatCitation(
                    document_id="doc-1",
                    snippet_text="Either party may terminate on thirty days written notice.",
                    reason="Direct termination clause",
                )
            ],
        )


def test_llm_review_analysis_uses_gemini_and_maps_page_numbers() -> None:
    pipeline = AnalysisPipeline(qdrant=_FakeQdrant({}), gemini_reviewer=_FakeGeminiReviewer())

    result = pipeline.analyze_llm_review(
        instructions="Review the full contract for termination for convenience.",
        documents=[
            {
                "document_id": "doc-1",
                "filename": "msa.pdf",
                "text": "Preamble\fEither party may terminate on thirty days written notice.",
            }
        ],
    )

    assert len(result.items) == 1
    assert result.items[0].outcome == "review"
    assert result.items[0].evidence[0].page_number == 2


def test_contract_chat_returns_guidance_when_question_is_missing() -> None:
    pipeline = AnalysisPipeline(qdrant=_FakeQdrant({}), gemini_reviewer=_FakeGeminiReviewer())

    result = pipeline.answer_contract_question(contract_id="contract-1", messages=[], documents=[{"document_id": "doc-1", "text": "Contract text"}])

    assert result.answer == "I need a question before I can review the contract."
    assert result.citations == []


def test_contract_chat_returns_guidance_when_document_text_is_missing() -> None:
    pipeline = AnalysisPipeline(qdrant=_FakeQdrant({}), gemini_reviewer=_FakeGeminiReviewer())

    result = pipeline.answer_contract_question(
        contract_id="contract-1",
        messages=[{"role": "user", "content": "What is the termination notice period?"}],
        documents=[],
    )

    assert result.answer == "No extracted contract text is available yet."
    assert result.citations == []


def test_contract_chat_uses_gemini_and_maps_citations() -> None:
    pipeline = AnalysisPipeline(qdrant=_FakeQdrant({}), gemini_reviewer=_FakeGeminiReviewer())

    result = pipeline.answer_contract_question(
        contract_id="contract-1",
        messages=[{"role": "user", "content": "What is the termination notice period?"}],
        documents=[
            {
                "document_id": "doc-1",
                "filename": "msa.pdf",
                "text": "Either party may terminate on thirty days written notice.",
            }
        ],
    )

    assert result.answer == "The contract allows termination on thirty days written notice."
    assert len(result.citations) == 1
    assert result.citations[0].document_id == "doc-1"
    assert result.citations[0].reason == "Direct termination clause"
