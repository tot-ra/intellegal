from __future__ import annotations

import shlex
from typing import Any, Literal

from pydantic import BaseModel, Field

from .db import ChunkSearchStore
from .indexing import HashEmbeddingGenerator
from .qdrant import QdrantService


class SearchSectionsResultItem(BaseModel):
    document_id: str
    page_number: int
    chunk_id: str | None = None
    score: float
    snippet_text: str


class SearchSectionsResult(BaseModel):
    items: list[SearchSectionsResultItem] = Field(default_factory=list)
    diagnostics: dict[str, Any] = Field(default_factory=dict)


def _parse_query_text(query_text: str) -> tuple[str, list[str]]:
    query = query_text.strip()
    if not query:
        return "", []

    try:
        tokens = shlex.split(query)
    except ValueError:
        tokens = query.split()

    positive_terms: list[str] = []
    excluded_terms: list[str] = []
    for token in tokens:
        normalized = token.strip()
        if not normalized:
            continue
        if normalized.startswith("!") and len(normalized) > 1:
            excluded_terms.append(normalized[1:].strip().lower())
            continue
        positive_terms.append(normalized)

    return " ".join(positive_terms).strip(), excluded_terms


def _contains_excluded_text(text: str, excluded_terms: list[str]) -> bool:
    normalized = text.strip().lower()
    if not normalized or not excluded_terms:
        return False
    return any(term in normalized for term in excluded_terms)


def _collapse_to_documents(
    items: list[SearchSectionsResultItem],
    *,
    limit: int,
) -> list[SearchSectionsResultItem]:
    best_by_document: dict[str, SearchSectionsResultItem] = {}
    for item in items:
        current = best_by_document.get(item.document_id)
        if current is None:
            best_by_document[item.document_id] = item
            continue
        if item.score > current.score:
            best_by_document[item.document_id] = item
            continue
        if item.score == current.score and item.page_number < current.page_number:
            best_by_document[item.document_id] = item

    ranked = sorted(
        best_by_document.values(),
        key=lambda item: (-item.score, item.document_id, item.page_number),
    )
    return ranked[: max(1, limit)]


class SearchPipeline:
    def __init__(
        self,
        *,
        qdrant: QdrantService,
        vector_size: int,
        chunk_store: ChunkSearchStore | None = None,
    ) -> None:
        self._qdrant = qdrant
        self._embeddings = HashEmbeddingGenerator(vector_size=vector_size)
        self._chunk_store = chunk_store

    def search_sections(
        self,
        *,
        query_text: str,
        document_ids: list[str] | None,
        limit: int = 10,
        strategy: Literal["semantic", "strict"] = "semantic",
        result_mode: Literal["sections", "contracts"] = "sections",
    ) -> SearchSectionsResult:
        query = query_text.strip()
        if not query:
            return SearchSectionsResult(
                items=[],
                diagnostics={"fallback": "empty_query", "result_mode": result_mode},
            )
        positive_query, excluded_terms = _parse_query_text(query)
        requested_limit = max(1, limit)
        candidate_limit = requested_limit
        if result_mode == "contracts":
            candidate_limit = max(candidate_limit, requested_limit * 5)

        if strategy == "strict":
            if self._chunk_store is None:
                return SearchSectionsResult(
                    items=[],
                    diagnostics={
                        "fallback": "strict_unavailable",
                        "result_mode": result_mode,
                        "strategy": "postgres_strict_text",
                    },
                )
            chunks = self._chunk_store.search_sections_strict(
                query_text=positive_query,
                exclude_texts=excluded_terms,
                document_ids=document_ids,
                limit=candidate_limit,
            )
            items = [
                SearchSectionsResultItem(
                    document_id=chunk["document_id"],
                    page_number=int(chunk.get("page_number") or 1),
                    chunk_id=chunk.get("chunk_id"),
                    score=round(float(chunk.get("score") or 0.0), 6),
                    snippet_text=str(chunk.get("text") or ""),
                )
                for chunk in chunks
            ]
            if result_mode == "contracts":
                items = _collapse_to_documents(items, limit=requested_limit)
            return SearchSectionsResult(
                items=items,
                diagnostics={
                    "query_length": len(query),
                    "excluded_terms": excluded_terms,
                    "result_count": len(items),
                    "result_mode": result_mode,
                    "strategy": "postgres_strict_text",
                },
            )

        if not positive_query:
            if self._chunk_store is None:
                return SearchSectionsResult(
                    items=[],
                    diagnostics={
                        "excluded_terms": excluded_terms,
                        "fallback": "semantic_exclusion_requires_positive_query",
                        "result_mode": result_mode,
                        "strategy": "qdrant_vector",
                    },
                )
            chunks = self._chunk_store.search_sections_strict(
                query_text="",
                exclude_texts=excluded_terms,
                document_ids=document_ids,
                limit=candidate_limit,
            )
            items = [
                SearchSectionsResultItem(
                    document_id=chunk["document_id"],
                    page_number=int(chunk.get("page_number") or 1),
                    chunk_id=chunk.get("chunk_id"),
                    score=round(float(chunk.get("score") or 0.0), 6),
                    snippet_text=str(chunk.get("text") or ""),
                )
                for chunk in chunks
            ]
            if result_mode == "contracts":
                items = _collapse_to_documents(items, limit=requested_limit)
            return SearchSectionsResult(
                items=items,
                diagnostics={
                    "excluded_terms": excluded_terms,
                    "fallback": "semantic_exclusion_uses_lexical_candidates",
                    "query_length": len(query),
                    "result_count": len(items),
                    "result_mode": result_mode,
                    "strategy": "qdrant_vector",
                },
            )

        vector = self._embeddings.embed(positive_query)
        chunks = self._qdrant.search_chunks(
            query_vector=vector,
            document_ids=document_ids,
            limit=max(candidate_limit, requested_limit * (4 if excluded_terms else 1)),
        )
        filtered_chunks = [chunk for chunk in chunks if not _contains_excluded_text(str(chunk.get("text") or ""), excluded_terms)]
        items = [
            SearchSectionsResultItem(
                document_id=chunk["document_id"],
                page_number=int(chunk.get("page_number") or 1),
                chunk_id=chunk.get("chunk_id"),
                score=round(float(chunk.get("score") or 0.0), 6),
                snippet_text=str(chunk.get("text") or ""),
            )
            for chunk in filtered_chunks[: max(1, candidate_limit)]
        ]
        if result_mode == "contracts":
            items = _collapse_to_documents(items, limit=requested_limit)
        else:
            items = items[:requested_limit]
        return SearchSectionsResult(
            items=items,
            diagnostics={
                "query_length": len(query),
                "excluded_terms": excluded_terms,
                "filtered_out_count": max(0, len(chunks) - len(filtered_chunks)),
                "result_count": len(items),
                "result_mode": result_mode,
                "strategy": "qdrant_vector",
            },
        )
