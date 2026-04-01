from __future__ import annotations

import hashlib
import uuid
from time import monotonic
from typing import Any

from ..models.indexing import Chunk, IndexPageInput, IndexingResult
from ..storage.postgres import ChunkSearchStore
from ..storage.qdrant import QdrantService
from .extraction import ExtractionError, _clamp_confidence


class HashEmbeddingGenerator:
    def __init__(self, vector_size: int) -> None:
        self._vector_size = vector_size

    def embed(self, text: str) -> list[float]:
        if self._vector_size <= 0:
            return []
        if not text:
            return [0.0] * self._vector_size

        values: list[float] = []
        counter = 0
        seed = text.encode("utf-8")
        while len(values) < self._vector_size:
            digest = hashlib.sha256(seed + counter.to_bytes(4, "big")).digest()
            counter += 1
            for byte in digest:
                values.append((byte / 255.0) * 2.0 - 1.0)
                if len(values) == self._vector_size:
                    break
        return values


class IndexingPipeline:
    def __init__(
        self,
        *,
        qdrant: QdrantService,
        vector_size: int,
        chunk_size: int = 800,
        chunk_overlap: int = 120,
        embedding_generator: HashEmbeddingGenerator | None = None,
        chunk_store: ChunkSearchStore | None = None,
    ) -> None:
        if chunk_size <= 0:
            raise ValueError("chunk_size must be positive")
        if chunk_overlap < 0 or chunk_overlap >= chunk_size:
            raise ValueError("chunk_overlap must be >= 0 and smaller than chunk_size")
        self._qdrant = qdrant
        self._chunk_size = chunk_size
        self._chunk_overlap = chunk_overlap
        self._embeddings = embedding_generator or HashEmbeddingGenerator(vector_size)
        self._chunk_store = chunk_store

    def index_document(
        self,
        *,
        document_id: str,
        checksum: str,
        text: str | None,
        pages: list[IndexPageInput] | None,
        reindex: bool,
        source_uri: str | None = None,
        version_id: str | None = None,
    ) -> IndexingResult:
        started = monotonic()
        checksum = (checksum or "").strip()
        if not checksum:
            raise ExtractionError(
                "version_checksum is required for indexing",
                code="invalid_argument",
                status_code=400,
                retriable=False,
            )

        existing = self._qdrant.count_chunks(document_id=document_id, checksum=checksum)
        if existing > 0 and not reindex:
            return IndexingResult(
                document_id=document_id,
                checksum=checksum,
                chunk_count=existing,
                indexed=False,
                skipped_reason="already_indexed",
                diagnostics={
                    "idempotent_skip": True,
                    "existing_chunk_count": existing,
                    "checksum_match": True,
                    "processing_ms": int((monotonic() - started) * 1000),
                },
            )

        normalized_pages = _prepare_pages(text=text, pages=pages)
        chunks = _chunk_pages(normalized_pages, chunk_size=self._chunk_size, overlap=self._chunk_overlap)
        points: list[dict[str, Any]] = []
        doc_version_id = version_id or checksum
        for chunk in chunks:
            vector = self._embeddings.embed(chunk.text)
            points.append(
                {
                    "id": _qdrant_point_id(document_id=document_id, document_version_id=doc_version_id, chunk_id=chunk.chunk_id),
                    "vector": vector,
                    "payload": {
                        "document_id": document_id,
                        "document_version_id": doc_version_id,
                        "chunk_id": chunk.chunk_id,
                        "page_number": chunk.page_number,
                        "checksum": checksum,
                        "source_uri": source_uri or "",
                        "text": chunk.text,
                        "chunk_confidence": round(_clamp_confidence(_chunk_confidence(chunk.text)), 3),
                    },
                }
            )

        if reindex:
            self._qdrant.delete_document_chunks(document_id=document_id)
        self._qdrant.upsert_chunks(points)
        if self._chunk_store is not None:
            self._chunk_store.upsert_document_chunks(
                document_id=document_id,
                checksum=checksum,
                chunks=[
                    {
                        "chunk_id": chunk.chunk_id,
                        "page_number": chunk.page_number,
                        "snippet_text": chunk.text,
                    }
                    for chunk in chunks
                ],
            )
        return IndexingResult(
            document_id=document_id,
            checksum=checksum,
            chunk_count=len(points),
            indexed=True,
            diagnostics={
                "idempotent_skip": False,
                "reindex": reindex,
                "chunk_size": self._chunk_size,
                "chunk_overlap": self._chunk_overlap,
                "page_count": len(normalized_pages),
                "empty_content": len(normalized_pages) == 0,
                "processing_ms": int((monotonic() - started) * 1000),
            },
        )


def _prepare_pages(text: str | None, pages: list[IndexPageInput] | None) -> list[IndexPageInput]:
    if pages:
        clean_pages = [
            IndexPageInput(page_number=page.page_number, text=(page.text or "").strip())
            for page in sorted(pages, key=lambda page: page.page_number)
        ]
        return [page for page in clean_pages if page.text]
    if text and text.strip():
        raw_pages = [part.strip() for part in text.split("\f")]
        pages_from_text = [
            IndexPageInput(page_number=idx, text=page_text)
            for idx, page_text in enumerate(raw_pages, start=1)
            if page_text
        ]
        if pages_from_text:
            return pages_from_text

    return []


def _chunk_pages(pages: list[IndexPageInput], *, chunk_size: int, overlap: int) -> list[Chunk]:
    chunks: list[Chunk] = []
    next_chunk_id = 1
    step = chunk_size - overlap

    for page in pages:
        content = page.text
        if len(content) <= chunk_size:
            chunks.append(Chunk(chunk_id=next_chunk_id, page_number=page.page_number, text=content))
            next_chunk_id += 1
            continue

        start = 0
        while start < len(content):
            end = min(len(content), start + chunk_size)
            piece = content[start:end].strip()
            if piece:
                chunks.append(Chunk(chunk_id=next_chunk_id, page_number=page.page_number, text=piece))
                next_chunk_id += 1
            if end >= len(content):
                break
            start += step

    return chunks


def _chunk_confidence(text: str) -> float:
    length = len(text)
    if length < 64:
        return 0.58
    if length < 192:
        return 0.74
    return 0.9


def _qdrant_point_id(*, document_id: str, document_version_id: str, chunk_id: int) -> str:
    seed = f"{document_id}:{document_version_id}:{chunk_id}"
    return str(uuid.uuid5(uuid.NAMESPACE_URL, seed))
