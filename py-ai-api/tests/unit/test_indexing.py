from __future__ import annotations

import uuid

import pytest

from py_ai_api.models.indexing import IndexPageInput
from py_ai_api.services.indexing import IndexingPipeline

pytestmark = pytest.mark.unit


class _FakeQdrant:
    def __init__(self, *, existing: int = 0) -> None:
        self.existing = existing
        self.deleted_document_ids: list[str] = []
        self.upserts: list[list[dict[str, object]]] = []

    def count_chunks(self, *, document_id: str, checksum: str) -> int:
        assert document_id
        assert checksum
        return self.existing

    def delete_document_chunks(self, *, document_id: str) -> None:
        self.deleted_document_ids.append(document_id)

    def upsert_chunks(self, points: list[dict[str, object]]) -> None:
        self.upserts.append(points)


def test_indexing_splits_text_into_overlapping_chunks() -> None:
    qdrant = _FakeQdrant()
    pipeline = IndexingPipeline(qdrant=qdrant, vector_size=8, chunk_size=10, chunk_overlap=3)

    result = pipeline.index_document(
        document_id="doc-1",
        checksum="sha-v1",
        text="ABCDEFGHIJKLMNO",
        pages=None,
        reindex=False,
    )

    assert result.indexed is True
    assert result.chunk_count == 2
    assert len(qdrant.upserts) == 1
    points = qdrant.upserts[0]
    assert points[0]["payload"]["page_number"] == 1
    assert points[0]["payload"]["text"] == "ABCDEFGHIJ"
    assert points[1]["payload"]["text"] == "HIJKLMNO"
    assert str(uuid.UUID(str(points[0]["id"]))) == points[0]["id"]
    assert str(uuid.UUID(str(points[1]["id"]))) == points[1]["id"]


def test_indexing_skips_when_checksum_is_already_indexed() -> None:
    qdrant = _FakeQdrant(existing=4)
    pipeline = IndexingPipeline(qdrant=qdrant, vector_size=8)

    result = pipeline.index_document(
        document_id="doc-1",
        checksum="sha-v1",
        text="contract text",
        pages=None,
        reindex=False,
    )

    assert result.indexed is False
    assert result.skipped_reason == "already_indexed"
    assert result.chunk_count == 4
    assert not qdrant.upserts


def test_reindex_deletes_old_chunks_then_upserts() -> None:
    qdrant = _FakeQdrant(existing=2)
    pipeline = IndexingPipeline(qdrant=qdrant, vector_size=8)

    result = pipeline.index_document(
        document_id="doc-1",
        checksum="sha-v2",
        text=None,
        pages=[IndexPageInput(page_number=1, text="renewed contract text")],
        reindex=True,
    )

    assert result.indexed is True
    assert qdrant.deleted_document_ids == ["doc-1"]
    assert len(qdrant.upserts) == 1


def test_indexing_allows_empty_extracted_content() -> None:
    qdrant = _FakeQdrant()
    pipeline = IndexingPipeline(qdrant=qdrant, vector_size=8)

    result = pipeline.index_document(
        document_id="doc-1",
        checksum="sha-v1",
        text="",
        pages=None,
        reindex=False,
    )

    assert result.indexed is True
    assert result.chunk_count == 0
    assert result.diagnostics["page_count"] == 0
    assert result.diagnostics["empty_content"] is True
    assert len(qdrant.upserts) == 1
    assert qdrant.upserts[0] == []
