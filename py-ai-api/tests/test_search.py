from __future__ import annotations

import pytest

from py_ai_api.search import SearchPipeline

pytestmark = pytest.mark.unit


class _FakeQdrant:
    def __init__(self) -> None:
        self.called = False
        self.calls: list[dict[str, object]] = []

    def search_chunks(self, **kwargs: object):
        self.called = True
        self.calls.append(kwargs)
        return [
            {
                "document_id": "doc-1",
                "page_number": 2,
                "chunk_id": "11",
                "score": 0.67,
                "text": "payment terms are due in 30 days",
            },
            {
                "document_id": "doc-1",
                "page_number": 3,
                "chunk_id": "12",
                "score": 0.61,
                "text": "payment terms exclude late fee penalties",
            }
        ]


class _FakeChunkStore:
    def __init__(self) -> None:
        self.calls: list[dict[str, object]] = []

    def search_sections_strict(
        self,
        *,
        query_text: str,
        exclude_texts: list[str] | None,
        document_ids: list[str] | None,
        limit: int,
    ):
        self.calls.append(
            {
                "query_text": query_text,
                "exclude_texts": exclude_texts,
                "document_ids": document_ids,
                "limit": limit,
            }
        )
        return [
            {
                "document_id": "doc-2",
                "page_number": 4,
                "chunk_id": "3",
                "score": 0.82,
                "text": "strict postgres match",
            }
        ]


def test_search_sections_semantic_strategy_uses_qdrant() -> None:
    qdrant = _FakeQdrant()
    pipeline = SearchPipeline(qdrant=qdrant, vector_size=8)

    result = pipeline.search_sections(query_text="payment terms !late", document_ids=["doc-1"], limit=5)

    assert result.diagnostics["strategy"] == "qdrant_vector"
    assert result.diagnostics["excluded_terms"] == ["late"]
    assert result.diagnostics["filtered_out_count"] == 1
    assert len(result.items) == 1
    assert result.items[0].document_id == "doc-1"
    assert qdrant.called is True
    assert qdrant.calls == [{"query_vector": qdrant.calls[0]["query_vector"], "document_ids": ["doc-1"], "limit": 20}]


def test_search_sections_strict_strategy_uses_chunk_store() -> None:
    qdrant = _FakeQdrant()
    chunk_store = _FakeChunkStore()
    pipeline = SearchPipeline(qdrant=qdrant, vector_size=8, chunk_store=chunk_store)

    result = pipeline.search_sections(
        query_text="payment terms !late",
        document_ids=["doc-2"],
        limit=7,
        strategy="strict",
    )

    assert result.diagnostics["strategy"] == "postgres_strict_text"
    assert len(result.items) == 1
    assert result.items[0].document_id == "doc-2"
    assert chunk_store.calls == [
        {"query_text": "payment terms", "exclude_texts": ["late"], "document_ids": ["doc-2"], "limit": 7}
    ]
    assert qdrant.called is False


def test_search_sections_semantic_negation_only_uses_chunk_store_fallback() -> None:
    qdrant = _FakeQdrant()
    chunk_store = _FakeChunkStore()
    pipeline = SearchPipeline(qdrant=qdrant, vector_size=8, chunk_store=chunk_store)

    result = pipeline.search_sections(
        query_text="!termination",
        document_ids=["doc-2"],
        limit=3,
        strategy="semantic",
    )

    assert result.diagnostics["fallback"] == "semantic_exclusion_uses_lexical_candidates"
    assert result.diagnostics["excluded_terms"] == ["termination"]
    assert chunk_store.calls == [
        {"query_text": "", "exclude_texts": ["termination"], "document_ids": ["doc-2"], "limit": 3}
    ]
    assert qdrant.called is False


def test_search_sections_contract_mode_collapses_qdrant_hits_by_document() -> None:
    qdrant = _FakeQdrant()
    pipeline = SearchPipeline(qdrant=qdrant, vector_size=8)

    result = pipeline.search_sections(
        query_text="payment terms",
        document_ids=["doc-1"],
        limit=1,
        result_mode="contracts",
    )

    assert result.diagnostics["result_mode"] == "contracts"
    assert len(result.items) == 1
    assert result.items[0].document_id == "doc-1"
    assert qdrant.calls == [{"query_vector": qdrant.calls[0]["query_vector"], "document_ids": ["doc-1"], "limit": 5}]


def test_search_sections_contract_mode_overfetches_strict_candidates() -> None:
    qdrant = _FakeQdrant()
    chunk_store = _FakeChunkStore()
    pipeline = SearchPipeline(qdrant=qdrant, vector_size=8, chunk_store=chunk_store)

    result = pipeline.search_sections(
        query_text="payment terms",
        document_ids=["doc-2"],
        limit=2,
        strategy="strict",
        result_mode="contracts",
    )

    assert result.diagnostics["result_mode"] == "contracts"
    assert chunk_store.calls == [
        {"query_text": "payment terms", "exclude_texts": [], "document_ids": ["doc-2"], "limit": 10}
    ]
