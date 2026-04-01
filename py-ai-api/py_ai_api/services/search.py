from __future__ import annotations

import shlex
from typing import Any, Literal

from ..models.search import SearchSectionsResult, SearchSectionsResultItem
from ..storage.postgres import ChunkSearchStore
from ..storage.qdrant import QdrantService
from .indexing import HashEmbeddingGenerator


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
    exclude_next = False
    for token in tokens:
        normalized = token.strip()
        if not normalized:
            continue
        if normalized == "!":
            exclude_next = True
            continue
        if normalized.startswith("!") and len(normalized) > 1:
            excluded_terms.append(normalized[1:].strip().lower())
            exclude_next = False
            continue
        if exclude_next:
            excluded_terms.append(normalized.lower())
            exclude_next = False
            continue
        positive_terms.append(normalized)

    return " ".join(positive_terms).strip(), excluded_terms


def _contains_excluded_text(text: str, excluded_terms: list[str]) -> bool:
    normalized = text.strip().lower()
    if not normalized or not excluded_terms:
        return False
    return any(term in normalized for term in excluded_terms)


def _filter_excluded_documents(
    chunks: list[dict[str, Any]],
    *,
    excluded_document_ids: set[str],
) -> list[dict[str, Any]]:
    if not excluded_document_ids:
        return chunks
    return [chunk for chunk in chunks if str(chunk.get("document_id") or "") not in excluded_document_ids]


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
        excluded_document_ids: set[str] = set()
        if excluded_terms and self._chunk_store is not None:
            excluded_document_ids = set(
                self._chunk_store.find_document_ids_with_text(
                    text_terms=excluded_terms,
                    document_ids=document_ids,
                )
            )
        chunks = self._qdrant.search_chunks(
            query_vector=vector,
            document_ids=document_ids,
            limit=max(candidate_limit, requested_limit * (4 if excluded_terms else 1)),
        )
        filtered_chunks = _filter_excluded_documents(
            [
                chunk
                for chunk in chunks
                if not _contains_excluded_text(str(chunk.get("text") or ""), excluded_terms)
            ],
            excluded_document_ids=excluded_document_ids,
        )
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
                "excluded_document_count": len(excluded_document_ids),
                "filtered_out_count": max(0, len(chunks) - len(filtered_chunks)),
                "result_count": len(items),
                "result_mode": result_mode,
                "strategy": "qdrant_vector",
            },
        )
