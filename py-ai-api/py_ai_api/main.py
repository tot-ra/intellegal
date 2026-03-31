import logging
from contextlib import asynccontextmanager
from functools import lru_cache
from typing import Annotated, Any, Literal

from fastapi import Depends, FastAPI
from fastapi.responses import JSONResponse
from pydantic import BaseModel

from .auth import require_internal_service_auth
from .analysis import AnalysisPipeline, AnalysisResult, ContractChatResult
from .config import Settings, get_settings
from .db import ChunkSearchStore, check_connection
from .extraction import ExtractionError, ExtractionPipeline, ExtractionResult
from .gemini import GeminiError, GeminiReviewer
from .indexing import IndexPageInput, IndexingPipeline, IndexingResult
from .logging import configure_logging
from .qdrant import QdrantService
from .search import SearchPipeline, SearchSectionsResult

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = get_settings()
    configure_logging(settings.log_level)
    if settings.database_startup_check:
        check_connection(settings.database_url)
    qdrant_collection = settings.qdrant_collection_name
    if settings.qdrant_startup_check_enabled:
        qdrant_service = QdrantService(settings)
        qdrant_service.startup_check()
        app.state.qdrant_service = qdrant_service
        qdrant_collection = qdrant_service.collection_name
    logger.info(
        "starting service",
        extra={
            "app_env": settings.app_env,
            "qdrant_collection": qdrant_collection,
            "qdrant_url": settings.qdrant_url,
        },
    )
    yield


app = FastAPI(title="Python AI API", version="0.1.0", lifespan=lifespan)


class ExtractJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    document_id: str
    storage_uri: str
    mime_type: str | None = None


class AcceptedJobResponse(BaseModel):
    job_id: str
    status: str
    job_type: str
    result: dict[str, Any] | None = None


class IndexJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    document_id: str
    version_checksum: str | None = None
    reindex: bool = False
    extracted_text: str | None = None
    pages: list[IndexPageInput] | None = None
    source_uri: str | None = None


class AnalyzeClauseJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    check_id: str
    document_ids: list[str] | None = None
    required_clause_text: str
    context_hint: str | None = None


class AnalyzeCompanyNameJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    check_id: str
    document_ids: list[str] | None = None
    old_company_name: str
    new_company_name: str | None = None


class AnalyzeLLMReviewDocument(BaseModel):
    document_id: str
    filename: str | None = None
    text: str | None = None


class AnalyzeLLMReviewJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    check_id: str
    document_ids: list[str] | None = None
    instructions: str
    documents: list[AnalyzeLLMReviewDocument] | None = None


class SearchSectionsJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    query_text: str
    document_ids: list[str] | None = None
    limit: int = 10
    strategy: Literal["semantic", "strict"] = "semantic"
    result_mode: Literal["sections", "contracts"] = "sections"


class ContractChatMessage(BaseModel):
    role: Literal["user", "assistant"]
    content: str


class ContractChatDocument(BaseModel):
    document_id: str
    filename: str | None = None
    text: str | None = None


class ContractChatJobRequest(BaseModel):
    job_id: str
    request_id: str | None = None
    contract_id: str
    messages: list[ContractChatMessage]
    documents: list[ContractChatDocument] | None = None


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


@app.get("/internal/v1/health")
def health(settings: Annotated[Settings, Depends(get_settings)]) -> dict[str, str]:
    return {
        "status": "ok",
        "service": settings.app_name,
        "env": settings.app_env,
        "qdrant_collection": settings.qdrant_collection_name,
        "gemini_configured": "true" if settings.gemini_api_key.strip() else "false",
    }


@app.get("/internal/v1/bootstrap/auth-check", dependencies=[Depends(require_internal_service_auth)])
def auth_check() -> dict[str, str]:
    return {"status": "ok"}


@app.post(
    "/internal/v1/extract",
    status_code=202,
    response_model=AcceptedJobResponse,
    dependencies=[Depends(require_internal_service_auth)],
)
def start_extract_job(
    payload: ExtractJobRequest,
    pipeline: Annotated[ExtractionPipeline, Depends(get_extraction_pipeline)],
) -> AcceptedJobResponse | JSONResponse:
    try:
        result = pipeline.extract_from_uri(payload.storage_uri, payload.mime_type)
    except ExtractionError as exc:
        return JSONResponse(
            status_code=exc.status_code,
            content={
                "error": {
                    "code": exc.code,
                    "message": str(exc),
                    "retriable": exc.retriable,
                    "details": exc.details,
                }
            },
        )

    return AcceptedJobResponse(
        job_id=payload.job_id,
        status="completed",
        job_type="extract",
        result=result.model_dump(),
    )


@app.post(
    "/internal/v1/index",
    status_code=202,
    response_model=AcceptedJobResponse,
    dependencies=[Depends(require_internal_service_auth)],
)
def start_index_job(
    payload: IndexJobRequest,
    pipeline: Annotated[IndexingPipeline, Depends(get_indexing_pipeline)],
) -> AcceptedJobResponse | JSONResponse:
    try:
        result = pipeline.index_document(
            document_id=payload.document_id,
            checksum=payload.version_checksum or "",
            text=payload.extracted_text,
            pages=payload.pages,
            reindex=payload.reindex,
            source_uri=payload.source_uri,
        )
    except ExtractionError as exc:
        return JSONResponse(
            status_code=exc.status_code,
            content={
                "error": {
                    "code": exc.code,
                    "message": str(exc),
                    "retriable": exc.retriable,
                    "details": exc.details,
                }
            },
        )

    return AcceptedJobResponse(
        job_id=payload.job_id,
        status="completed",
        job_type="index",
        result=result.model_dump(),
    )


@app.post(
    "/internal/v1/analyze/clause",
    status_code=202,
    response_model=AcceptedJobResponse,
    dependencies=[Depends(require_internal_service_auth)],
)
def start_clause_analysis_job(
    payload: AnalyzeClauseJobRequest,
    pipeline: Annotated[AnalysisPipeline, Depends(get_analysis_pipeline)],
) -> AcceptedJobResponse:
    result: AnalysisResult = pipeline.analyze_clause(
        required_clause_text=payload.required_clause_text,
        document_ids=payload.document_ids,
    )
    return AcceptedJobResponse(
        job_id=payload.job_id,
        status="completed",
        job_type="analyze_clause",
        result=result.model_dump(),
    )


@app.post(
    "/internal/v1/analyze/company-name",
    status_code=202,
    response_model=AcceptedJobResponse,
    dependencies=[Depends(require_internal_service_auth)],
)
def start_company_name_analysis_job(
    payload: AnalyzeCompanyNameJobRequest,
    pipeline: Annotated[AnalysisPipeline, Depends(get_analysis_pipeline)],
) -> AcceptedJobResponse:
    result: AnalysisResult = pipeline.analyze_company_name(
        old_company_name=payload.old_company_name,
        new_company_name=payload.new_company_name,
        document_ids=payload.document_ids,
    )
    return AcceptedJobResponse(
        job_id=payload.job_id,
        status="completed",
        job_type="analyze_company_name",
        result=result.model_dump(),
    )


@app.post(
    "/internal/v1/analyze/llm-review",
    status_code=202,
    response_model=AcceptedJobResponse,
    dependencies=[Depends(require_internal_service_auth)],
)
def start_llm_review_analysis_job(
    payload: AnalyzeLLMReviewJobRequest,
    pipeline: Annotated[AnalysisPipeline, Depends(get_analysis_pipeline)],
) -> AcceptedJobResponse | JSONResponse:
    try:
        result: AnalysisResult = pipeline.analyze_llm_review(
            instructions=payload.instructions,
            documents=[document.model_dump() for document in (payload.documents or [])],
        )
    except (GeminiError, RuntimeError) as exc:
        return JSONResponse(
            status_code=503,
            content={
                "error": {
                    "code": "llm_review_unavailable",
                    "message": str(exc),
                    "retriable": True,
                }
            },
        )
    return AcceptedJobResponse(
        job_id=payload.job_id,
        status="completed",
        job_type="analyze_llm_review",
        result=result.model_dump(),
    )


@app.post(
    "/internal/v1/chat/contract",
    status_code=202,
    response_model=AcceptedJobResponse,
    dependencies=[Depends(require_internal_service_auth)],
)
def start_contract_chat_job(
    payload: ContractChatJobRequest,
    pipeline: Annotated[AnalysisPipeline, Depends(get_analysis_pipeline)],
) -> AcceptedJobResponse | JSONResponse:
    try:
        result: ContractChatResult = pipeline.answer_contract_question(
            contract_id=payload.contract_id,
            messages=[message.model_dump() for message in payload.messages],
            documents=[document.model_dump() for document in (payload.documents or [])],
        )
    except (GeminiError, RuntimeError) as exc:
        return JSONResponse(
            status_code=503,
            content={
                "error": {
                    "code": "contract_chat_unavailable",
                    "message": str(exc),
                    "retriable": True,
                }
            },
        )
    return AcceptedJobResponse(
        job_id=payload.job_id,
        status="completed",
        job_type="contract_chat",
        result=result.model_dump(),
    )


@app.post(
    "/internal/v1/search/sections",
    status_code=202,
    response_model=AcceptedJobResponse,
    dependencies=[Depends(require_internal_service_auth)],
)
def start_search_sections_job(
    payload: SearchSectionsJobRequest,
    pipeline: Annotated[SearchPipeline, Depends(get_search_pipeline)],
) -> AcceptedJobResponse:
    result: SearchSectionsResult = pipeline.search_sections(
        query_text=payload.query_text,
        document_ids=payload.document_ids,
        limit=max(1, min(payload.limit, 50)),
        strategy=payload.strategy,
        result_mode=payload.result_mode,
    )
    return AcceptedJobResponse(
        job_id=payload.job_id,
        status="completed",
        job_type="search_sections",
        result=result.model_dump(),
    )
