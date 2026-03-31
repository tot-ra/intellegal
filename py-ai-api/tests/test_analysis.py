from __future__ import annotations

from py_ai_api.analysis import AnalysisPipeline
from py_ai_api.gemini import GeminiReviewResult


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


def test_company_name_analysis_handles_old_and_new_name_conflict() -> None:
    pipeline = AnalysisPipeline(
        qdrant=_FakeQdrant(
            {
                "doc-1": [
                    {"chunk_id": 1, "page_number": 1, "text": "Supplier: Old Corp"},
                    {"chunk_id": 2, "page_number": 1, "text": "Signed by New Corp"},
                ]
            }
        )
    )

    result = pipeline.analyze_company_name(
        old_company_name="Old Corp",
        new_company_name="New Corp",
        document_ids=["doc-1"],
    )

    assert len(result.items) == 1
    assert result.items[0].outcome == "review"
    assert len(result.items[0].evidence) == 2


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
