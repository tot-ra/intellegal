from __future__ import annotations

from typing import Annotated

from fastapi import APIRouter, Depends
from fastapi.responses import JSONResponse

from ...config import Settings, get_settings
from ...models.analysis import AnalysisResult, ContractChatResult
from ...models.extraction import ExtractionResult
from ...models.indexing import IndexingResult
from ...models.search import SearchSectionsResult
from ...services.analysis import AnalysisPipeline
from ...services.extraction import ExtractionError, ExtractionPipeline
from ...services.gemini import GeminiError
from ...services.indexing import IndexingPipeline
from ...services.search import SearchPipeline
from ..auth import require_internal_service_auth
from ..dependencies import (
    get_analysis_pipeline,
    get_extraction_pipeline,
    get_indexing_pipeline,
    get_search_pipeline,
)
from ..schemas import (
    AcceptedJobResponse,
    AnalyzeClauseJobRequest,
    AnalyzeLLMReviewJobRequest,
    ContractChatJobRequest,
    ExtractJobRequest,
    IndexJobRequest,
    SearchSectionsJobRequest,
)

router = APIRouter()


@router.get("/internal/v1/health")
def health(settings: Annotated[Settings, Depends(get_settings)]) -> dict[str, str]:
    return {
        "status": "ok",
        "service": settings.app_name,
        "env": settings.app_env,
        "qdrant_collection": settings.qdrant_collection_name,
        "gemini_configured": "true" if settings.gemini_api_key.strip() else "false",
    }


@router.get("/internal/v1/bootstrap/auth-check", dependencies=[Depends(require_internal_service_auth)])
def auth_check() -> dict[str, str]:
    return {"status": "ok"}


@router.post(
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
        result: ExtractionResult = pipeline.extract_from_uri(payload.storage_uri, payload.mime_type)
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


@router.post(
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
        result: IndexingResult = pipeline.index_document(
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


@router.post(
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


@router.post(
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


@router.post(
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


@router.post(
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
