from __future__ import annotations

import json

import pytest

from py_ai_api.services.gemini import GeminiReviewer

pytestmark = pytest.mark.unit


def test_gemini_reviewer_parses_json_response() -> None:
    seen = {}

    def _requester(req, timeout: float) -> bytes:
        seen["url"] = req.full_url
        seen["body"] = json.loads(req.data.decode("utf-8"))
        seen["timeout"] = timeout
        return json.dumps(
            {
                "candidates": [
                    {
                        "content": {
                            "parts": [
                                {
                                    "text": json.dumps(
                                        {
                                            "outcome": "match",
                                            "confidence": 0.91,
                                            "summary": "The contract clearly includes the required right.",
                                            "evidence_snippets": ["Either party may terminate for convenience on 30 days notice."],
                                        }
                                    )
                                }
                            ]
                        }
                    }
                ]
            }
        ).encode("utf-8")

    reviewer = GeminiReviewer(
        api_key="test-key",
        model="gemini-2.5-flash",
        timeout_seconds=12.0,
        requester=_requester,
    )

    result = reviewer.review_document(
        instructions="Review the full contract for termination for convenience.",
        filename="msa.pdf",
        document_text="Either party may terminate for convenience on 30 days notice.",
    )

    assert "gemini-2.5-flash:generateContent" in seen["url"]
    assert seen["body"]["generationConfig"]["responseMimeType"] == "application/json"
    assert seen["timeout"] == 12.0
    assert result.outcome == "match"
    assert result.confidence == 0.91
    assert result.evidence_snippets == ["Either party may terminate for convenience on 30 days notice."]


def test_gemini_reviewer_parses_contract_chat_response() -> None:
    def _requester(req, timeout: float) -> bytes:
        assert timeout == 12.0
        body = json.loads(req.data.decode("utf-8"))
        assert body["generationConfig"]["responseMimeType"] == "application/json"
        return json.dumps(
            {
                "candidates": [
                    {
                        "content": {
                            "parts": [
                                {
                                    "text": json.dumps(
                                        {
                                            "answer": "Yes, the contract allows termination with notice.",
                                            "citations": [
                                                {
                                                    "document_id": "doc-1",
                                                    "snippet_text": "Either party may terminate for convenience on 30 days notice.",
                                                    "reason": "This is the termination clause.",
                                                }
                                            ],
                                        }
                                    )
                                }
                            ]
                        }
                    }
                ]
            }
        ).encode("utf-8")

    reviewer = GeminiReviewer(
        api_key="test-key",
        model="gemini-2.5-flash",
        timeout_seconds=12.0,
        requester=_requester,
    )

    result = reviewer.answer_contract_question(
        contract_id="contract-1",
        messages=[{"role": "user", "content": "Can either party terminate for convenience?"}],
        documents=[
            {
                "document_id": "doc-1",
                "filename": "msa.pdf",
                "text": "Either party may terminate for convenience on 30 days notice.",
            }
        ],
    )

    assert result.answer == "Yes, the contract allows termination with notice."
    assert len(result.citations) == 1
    assert result.citations[0].document_id == "doc-1"
    assert result.citations[0].reason == "This is the termination clause."
