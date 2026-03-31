from __future__ import annotations

import json

from py_ai_api.gemini import GeminiReviewer


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
