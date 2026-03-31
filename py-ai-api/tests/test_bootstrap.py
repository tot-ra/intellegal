from py_ai_api.analysis import AnalysisResult, AnalysisResultItem
from fastapi.testclient import TestClient

from py_ai_api.config import get_settings
from py_ai_api.extraction import ExtractionResult, PageExtraction
from py_ai_api.indexing import IndexingResult
from py_ai_api.main import app, get_analysis_pipeline, get_extraction_pipeline, get_indexing_pipeline, get_search_pipeline
from py_ai_api.search import SearchSectionsResult, SearchSectionsResultItem


def _client_with_env(monkeypatch, *, token: str = "test-token") -> TestClient:
    monkeypatch.setenv("INTERNAL_SERVICE_TOKEN", token)
    monkeypatch.setenv("APP_NAME", "python-ai-api-test")
    monkeypatch.setenv("APP_ENV", "test")
    monkeypatch.setenv("QDRANT_STARTUP_CHECK_ENABLED", "false")
    monkeypatch.delenv("GEMINI_API_KEY", raising=False)
    get_settings.cache_clear()
    return TestClient(app)


def test_health_endpoint_returns_service_metadata(monkeypatch) -> None:
    client = _client_with_env(monkeypatch)

    response = client.get("/internal/v1/health")

    assert response.status_code == 200
    assert response.json() == {
        "status": "ok",
        "service": "python-ai-api-test",
        "env": "test",
        "qdrant_collection": "document_chunks",
        "gemini_configured": "false",
    }


def test_auth_check_rejects_missing_token(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    response = client.get("/internal/v1/bootstrap/auth-check")

    assert response.status_code == 401
    assert response.json()["detail"] == "Unauthorized"


def test_auth_check_accepts_valid_token(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    response = client.get(
        "/internal/v1/bootstrap/auth-check",
        headers={"X-Internal-Service-Token": "shared-secret"},
    )

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_extract_rejects_missing_token(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    response = client.post(
        "/internal/v1/extract",
        json={
            "job_id": "4ffd2987-9813-4dc5-a654-cb44dcb58de2",
            "document_id": "2a3d41df-f7bf-419d-b0f6-2e62cef5ec7f",
            "storage_uri": "/tmp/document.pdf",
            "mime_type": "application/pdf",
        },
    )

    assert response.status_code == 401
    assert response.json()["detail"] == "Unauthorized"


def test_extract_returns_completed_job_with_result(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    class _FakePipeline:
        def extract_from_uri(self, storage_uri: str, mime_type: str | None = None) -> ExtractionResult:
            assert storage_uri == "/tmp/document.pdf"
            assert mime_type == "application/pdf"
            return ExtractionResult(
                mime_type="application/pdf",
                text="Page one\n\f\nPage two",
                pages=[
                    PageExtraction(page_number=1, text="Page one", char_count=8, confidence=0.7, source="pdf_text"),
                    PageExtraction(page_number=2, text="Page two", char_count=8, confidence=0.72, source="pdf_text"),
                ],
                confidence=0.71,
                diagnostics={"page_count": 2, "ocr_used": False},
            )

    app.dependency_overrides[get_extraction_pipeline] = lambda: _FakePipeline()
    try:
        response = client.post(
            "/internal/v1/extract",
            headers={"X-Internal-Service-Token": "shared-secret"},
            json={
                "job_id": "4ffd2987-9813-4dc5-a654-cb44dcb58de2",
                "document_id": "2a3d41df-f7bf-419d-b0f6-2e62cef5ec7f",
                "storage_uri": "/tmp/document.pdf",
                "mime_type": "application/pdf",
            },
        )
    finally:
        app.dependency_overrides.pop(get_extraction_pipeline, None)

    assert response.status_code == 202
    payload = response.json()
    assert payload["job_id"] == "4ffd2987-9813-4dc5-a654-cb44dcb58de2"
    assert payload["status"] == "completed"
    assert payload["job_type"] == "extract"
    assert payload["result"]["confidence"] == 0.71
    assert payload["result"]["diagnostics"]["page_count"] == 2


def test_index_returns_completed_job_with_result(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    class _FakeIndexPipeline:
        def index_document(self, **kwargs) -> IndexingResult:
            assert kwargs["document_id"] == "2a3d41df-f7bf-419d-b0f6-2e62cef5ec7f"
            assert kwargs["checksum"] == "sha-001"
            return IndexingResult(
                document_id=kwargs["document_id"],
                checksum=kwargs["checksum"],
                chunk_count=3,
                indexed=True,
                diagnostics={"chunk_size": 800},
            )

    app.dependency_overrides[get_indexing_pipeline] = lambda: _FakeIndexPipeline()
    try:
        response = client.post(
            "/internal/v1/index",
            headers={"X-Internal-Service-Token": "shared-secret"},
            json={
                "job_id": "af4f6ad1-e9f2-4e74-bd75-b57aa49ff99c",
                "document_id": "2a3d41df-f7bf-419d-b0f6-2e62cef5ec7f",
                "version_checksum": "sha-001",
                "extracted_text": "Page one\fPage two",
                "reindex": False,
            },
        )
    finally:
        app.dependency_overrides.pop(get_indexing_pipeline, None)

    assert response.status_code == 202
    payload = response.json()
    assert payload["job_id"] == "af4f6ad1-e9f2-4e74-bd75-b57aa49ff99c"
    assert payload["job_type"] == "index"
    assert payload["status"] == "completed"
    assert payload["result"]["chunk_count"] == 3


def test_clause_analysis_returns_completed_job_with_result(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    class _FakeAnalysisPipeline:
        def analyze_clause(self, **kwargs) -> AnalysisResult:
            assert kwargs["required_clause_text"] == "must include payment terms"
            assert kwargs["document_ids"] == ["doc-1"]
            return AnalysisResult(
                items=[
                    AnalysisResultItem(
                        document_id="doc-1",
                        outcome="match",
                        confidence=0.92,
                        summary="Clause found.",
                    )
                ],
                diagnostics={"strategy": "test"},
            )

    app.dependency_overrides[get_analysis_pipeline] = lambda: _FakeAnalysisPipeline()
    try:
        response = client.post(
            "/internal/v1/analyze/clause",
            headers={"X-Internal-Service-Token": "shared-secret"},
            json={
                "job_id": "f463315e-bcc6-4477-926f-83020b3a4bae",
                "check_id": "06f16fc3-0d05-4550-9f20-bf757b13f0af",
                "document_ids": ["doc-1"],
                "required_clause_text": "must include payment terms",
            },
        )
    finally:
        app.dependency_overrides.pop(get_analysis_pipeline, None)

    assert response.status_code == 202
    payload = response.json()
    assert payload["job_type"] == "analyze_clause"
    assert payload["result"]["items"][0]["outcome"] == "match"


def test_company_name_analysis_returns_completed_job_with_result(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    class _FakeAnalysisPipeline:
        def analyze_company_name(self, **kwargs) -> AnalysisResult:
            assert kwargs["old_company_name"] == "Old Corp"
            assert kwargs["new_company_name"] == "New Corp"
            return AnalysisResult(
                items=[
                    AnalysisResultItem(
                        document_id="doc-1",
                        outcome="review",
                        confidence=0.6,
                        summary="Both names found.",
                    )
                ]
            )

    app.dependency_overrides[get_analysis_pipeline] = lambda: _FakeAnalysisPipeline()
    try:
        response = client.post(
            "/internal/v1/analyze/company-name",
            headers={"X-Internal-Service-Token": "shared-secret"},
            json={
                "job_id": "f463315e-bcc6-4477-926f-83020b3a4bae",
                "check_id": "06f16fc3-0d05-4550-9f20-bf757b13f0af",
                "document_ids": ["doc-1"],
                "old_company_name": "Old Corp",
                "new_company_name": "New Corp",
            },
        )
    finally:
        app.dependency_overrides.pop(get_analysis_pipeline, None)

    assert response.status_code == 202
    payload = response.json()
    assert payload["job_type"] == "analyze_company_name"
    assert payload["result"]["items"][0]["outcome"] == "review"


def test_llm_review_analysis_returns_completed_job_with_result(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    class _FakeAnalysisPipeline:
        def analyze_llm_review(self, **kwargs) -> AnalysisResult:
            assert kwargs["instructions"] == "Review the full contract for termination rights."
            assert kwargs["documents"] == [
                {
                    "document_id": "doc-1",
                    "filename": "msa.pdf",
                    "text": "Contract text",
                }
            ]
            return AnalysisResult(
                items=[
                    AnalysisResultItem(
                        document_id="doc-1",
                        outcome="review",
                        confidence=0.77,
                        summary="Potential termination right found.",
                    )
                ],
                diagnostics={"strategy": "gemini_llm_review"},
            )

    app.dependency_overrides[get_analysis_pipeline] = lambda: _FakeAnalysisPipeline()
    try:
        response = client.post(
            "/internal/v1/analyze/llm-review",
            headers={"X-Internal-Service-Token": "shared-secret"},
            json={
                "job_id": "f463315e-bcc6-4477-926f-83020b3a4bb0",
                "check_id": "06f16fc3-0d05-4550-9f20-bf757b13f0b0",
                "document_ids": ["doc-1"],
                "instructions": "Review the full contract for termination rights.",
                "documents": [
                    {
                        "document_id": "doc-1",
                        "filename": "msa.pdf",
                        "text": "Contract text",
                    }
                ],
            },
        )
    finally:
        app.dependency_overrides.pop(get_analysis_pipeline, None)

    assert response.status_code == 202
    payload = response.json()
    assert payload["job_type"] == "analyze_llm_review"
    assert payload["result"]["items"][0]["outcome"] == "review"


def test_llm_review_analysis_returns_structured_error_when_gemini_is_unavailable(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    class _FakeAnalysisPipeline:
        def analyze_llm_review(self, **kwargs) -> AnalysisResult:
            raise RuntimeError("Gemini reviewer is not configured")

    app.dependency_overrides[get_analysis_pipeline] = lambda: _FakeAnalysisPipeline()
    try:
        response = client.post(
            "/internal/v1/analyze/llm-review",
            headers={"X-Internal-Service-Token": "shared-secret"},
            json={
                "job_id": "f463315e-bcc6-4477-926f-83020b3a4bb1",
                "check_id": "06f16fc3-0d05-4550-9f20-bf757b13f0b1",
                "document_ids": ["doc-1"],
                "instructions": "Review the full contract for termination rights.",
                "documents": [
                    {
                        "document_id": "doc-1",
                        "filename": "msa.pdf",
                        "text": "Contract text",
                    }
                ],
            },
        )
    finally:
        app.dependency_overrides.pop(get_analysis_pipeline, None)

    assert response.status_code == 503
    payload = response.json()
    assert payload["error"]["code"] == "llm_review_unavailable"
    assert payload["error"]["message"] == "Gemini reviewer is not configured"
    assert payload["error"]["retriable"] is True


def test_search_sections_returns_completed_job_with_result(monkeypatch) -> None:
    client = _client_with_env(monkeypatch, token="shared-secret")

    class _FakeSearchPipeline:
        def search_sections(self, **kwargs) -> SearchSectionsResult:
            assert kwargs["query_text"] == "payment terms"
            assert kwargs["document_ids"] == ["doc-1"]
            assert kwargs["limit"] == 5
            assert kwargs["strategy"] == "strict"
            return SearchSectionsResult(
                items=[
                    SearchSectionsResultItem(
                        document_id="doc-1",
                        page_number=3,
                        chunk_id="9",
                        score=0.88,
                        snippet_text="... payment terms within thirty days ...",
                    )
                ],
                diagnostics={"strategy": "qdrant_vector"},
            )

    app.dependency_overrides[get_search_pipeline] = lambda: _FakeSearchPipeline()
    try:
        response = client.post(
            "/internal/v1/search/sections",
            headers={"X-Internal-Service-Token": "shared-secret"},
            json={
                "job_id": "7f891617-7c2a-4f54-8d69-5f6f2d3a4dd1",
                "query_text": "payment terms",
                "document_ids": ["doc-1"],
                "limit": 5,
                "strategy": "strict",
            },
        )
    finally:
        app.dependency_overrides.pop(get_search_pipeline, None)

    assert response.status_code == 202
    payload = response.json()
    assert payload["job_type"] == "search_sections"
    assert payload["result"]["items"][0]["document_id"] == "doc-1"
