from __future__ import annotations

from dataclasses import dataclass
from io import BytesIO
from urllib import error as urlerror

import pytest

from py_ai_api.models.extraction import OCRText
from py_ai_api.services.extraction import ExtractionError, ExtractionPipeline, _load_document_bytes

pytestmark = pytest.mark.unit


class _FakePDFExtractor:
    def extract_pages(self, payload: bytes) -> list[str]:
        assert payload == b"pdf-bytes"
        return [
            "  First   page\r\nline 1  \n\nline\t2 ",
            "\n\nSecond\u00a0page ",
        ]


class _FakeDOCXExtractor:
    def extract_text(self, payload: bytes) -> str:
        assert payload == b"docx-bytes"
        return "  Master   Service  Agreement \r\n\nTerm:\t12 months "


@dataclass
class _FakeOCRExtractor:
    confidence: float = 0.81

    def extract(self, payload: bytes) -> OCRText:
        assert payload in {b"jpeg-bytes", b"png-bytes"}
        return OCRText(
            text="  Scanned   text \r\nfrom\timage ",
            confidence=self.confidence,
            diagnostics={"engine": "fake-ocr"},
        )


def test_pdf_extraction_normalizes_text_and_preserves_page_boundaries() -> None:
    pipeline = ExtractionPipeline(
        pdf_extractor=_FakePDFExtractor(),
        docx_extractor=_FakeDOCXExtractor(),
        ocr_extractor=_FakeOCRExtractor(),
    )

    result = pipeline.extract_bytes(b"pdf-bytes", "application/pdf")

    assert result.mime_type == "application/pdf"
    assert len(result.pages) == 2
    assert result.pages[0].text == "First page\nline 1\n\nline 2"
    assert result.pages[1].text == "Second page"
    assert result.text == "First page\nline 1\n\nline 2\n\f\nSecond page"
    assert result.diagnostics["page_count"] == 2
    assert result.diagnostics["ocr_used"] is False


def test_jpeg_ocr_extraction_uses_ocr_confidence_and_metadata() -> None:
    pipeline = ExtractionPipeline(
        pdf_extractor=_FakePDFExtractor(),
        docx_extractor=_FakeDOCXExtractor(),
        ocr_extractor=_FakeOCRExtractor(),
    )

    result = pipeline.extract_bytes(b"jpeg-bytes", "image/jpeg")

    assert result.mime_type == "image/jpeg"
    assert len(result.pages) == 1
    assert result.pages[0].source == "ocr"
    assert result.pages[0].text == "Scanned text\nfrom image"
    assert result.pages[0].confidence == 0.81
    assert result.confidence == 0.81
    assert result.diagnostics["ocr_used"] is True
    assert result.diagnostics["ocr"]["engine"] == "fake-ocr"


def test_png_ocr_extraction_uses_same_ocr_path() -> None:
    pipeline = ExtractionPipeline(
        pdf_extractor=_FakePDFExtractor(),
        docx_extractor=_FakeDOCXExtractor(),
        ocr_extractor=_FakeOCRExtractor(),
    )

    result = pipeline.extract_bytes(b"png-bytes", "image/png")

    assert result.mime_type == "image/png"
    assert len(result.pages) == 1
    assert result.pages[0].source == "ocr"
    assert result.pages[0].text == "Scanned text\nfrom image"


def test_docx_extraction_uses_docx_text_path() -> None:
    pipeline = ExtractionPipeline(
        pdf_extractor=_FakePDFExtractor(),
        docx_extractor=_FakeDOCXExtractor(),
        ocr_extractor=_FakeOCRExtractor(),
    )

    result = pipeline.extract_bytes(b"docx-bytes", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")

    assert result.mime_type == "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
    assert len(result.pages) == 1
    assert result.pages[0].source == "docx_text"
    assert result.pages[0].text == "Master Service Agreement\n\nTerm: 12 months"
    assert result.diagnostics["ocr_used"] is False


def test_load_document_bytes_supports_http(monkeypatch) -> None:
    class _Response(BytesIO):
        def __enter__(self):
            return self

        def __exit__(self, *_args):
            self.close()
            return False

    def _fake_urlopen(*_args, **_kwargs):
        return _Response(b"remote-content")

    monkeypatch.setattr("urllib.request.urlopen", _fake_urlopen)

    payload, file_path = _load_document_bytes("http://minio:9000/contracts/a.pdf")

    assert payload == b"remote-content"
    assert file_path == "/contracts/a.pdf"


def test_load_document_bytes_returns_retriable_error_for_http_connectivity(monkeypatch) -> None:
    def _fail(*_args, **_kwargs):
        raise urlerror.URLError("unreachable")

    monkeypatch.setattr("urllib.request.urlopen", _fail)

    with pytest.raises(ExtractionError) as err:
        _load_document_bytes("http://minio:9000/contracts/a.pdf")

    assert err.value.code == "dependency_unavailable"
    assert err.value.retriable is True
