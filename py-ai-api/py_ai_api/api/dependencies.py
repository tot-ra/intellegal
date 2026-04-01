from __future__ import annotations

from functools import lru_cache
from typing import Annotated

from fastapi import Depends

from ..config import Settings, get_settings
from ..services.analysis import AnalysisPipeline
from ..services.extraction import ExtractionPipeline
from ..services.gemini import GeminiReviewer
from ..services.indexing import IndexingPipeline
from ..services.search import SearchPipeline
from ..storage.postgres import ChunkSearchStore
from ..storage.qdrant import QdrantService


@lru_cache
def get_extraction_pipeline() -> ExtractionPipeline:
    return ExtractionPipeline()


def get_chunk_search_store(settings: Annotated[Settings, Depends(get_settings)]) -> ChunkSearchStore:
    return ChunkSearchStore(settings.database_url)


def get_qdrant_service(settings: Annotated[Settings, Depends(get_settings)]) -> QdrantService:
    return QdrantService(settings)


def get_indexing_pipeline(
    settings: Annotated[Settings, Depends(get_settings)],
    qdrant: Annotated[QdrantService, Depends(get_qdrant_service)],
    chunk_store: Annotated[ChunkSearchStore, Depends(get_chunk_search_store)],
) -> IndexingPipeline:
    return IndexingPipeline(
        qdrant=qdrant,
        vector_size=settings.qdrant_vector_size,
        chunk_size=settings.index_chunk_size,
        chunk_overlap=settings.index_chunk_overlap,
        chunk_store=chunk_store,
    )


def get_analysis_pipeline(
    settings: Annotated[Settings, Depends(get_settings)],
    qdrant: Annotated[QdrantService, Depends(get_qdrant_service)],
) -> AnalysisPipeline:
    reviewer = None
    if settings.gemini_api_key.strip():
        reviewer = GeminiReviewer(
            api_key=settings.gemini_api_key,
            model=settings.gemini_model,
            timeout_seconds=settings.gemini_timeout_seconds,
        )
    return AnalysisPipeline(qdrant=qdrant, gemini_reviewer=reviewer)


def get_search_pipeline(
    settings: Annotated[Settings, Depends(get_settings)],
    qdrant: Annotated[QdrantService, Depends(get_qdrant_service)],
    chunk_store: Annotated[ChunkSearchStore, Depends(get_chunk_search_store)],
) -> SearchPipeline:
    return SearchPipeline(qdrant=qdrant, vector_size=settings.qdrant_vector_size, chunk_store=chunk_store)
