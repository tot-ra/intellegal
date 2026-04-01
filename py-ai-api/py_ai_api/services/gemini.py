from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Callable
from urllib import error, parse, request


class GeminiError(RuntimeError):
    pass


@dataclass(frozen=True)
class GeminiReviewResult:
    outcome: str
    confidence: float
    summary: str
    evidence_snippets: list[str]


@dataclass(frozen=True)
class GeminiContractChatCitation:
    document_id: str
    snippet_text: str
    reason: str


@dataclass(frozen=True)
class GeminiContractChatResult:
    answer: str
    citations: list[GeminiContractChatCitation]


class GeminiReviewer:
    def __init__(
        self,
        *,
        api_key: str,
        model: str,
        timeout_seconds: float = 30.0,
        requester: Callable[[request.Request, float], bytes] | None = None,
    ) -> None:
        self._api_key = api_key.strip()
        self._model = model.strip()
        self._timeout_seconds = timeout_seconds
        self._requester = requester or _default_requester

    def review_document(self, *, instructions: str, filename: str, document_text: str) -> GeminiReviewResult:
        if not self._api_key:
            raise GeminiError("GEMINI_API_KEY is not configured")
        if not self._model:
            raise GeminiError("GEMINI_MODEL is not configured")

        prompt = _build_review_prompt(
            instructions=instructions,
            filename=filename,
            document_text=document_text,
        )
        payload = {
            "contents": [{"role": "user", "parts": [{"text": prompt}]}],
            "generationConfig": {"temperature": 0.1, "responseMimeType": "application/json"},
        }
        endpoint = (
            "https://generativelanguage.googleapis.com/v1beta/models/"
            f"{parse.quote(self._model, safe='')}:generateContent?key={parse.quote(self._api_key, safe='')}"
        )
        req = request.Request(
            endpoint,
            data=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST",
        )

        try:
            raw = self._requester(req, self._timeout_seconds)
        except error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise GeminiError(f"Gemini request failed with HTTP {exc.code}: {detail}") from exc
        except error.URLError as exc:
            raise GeminiError(f"Gemini request failed: {exc.reason}") from exc

        response_payload = json.loads(raw.decode("utf-8"))
        text = _extract_text(response_payload)
        if not text:
            raise GeminiError("Gemini response did not include review content")

        parsed = json.loads(text)
        outcome = str(parsed.get("outcome") or "").strip().lower()
        if outcome not in {"match", "missing", "review"}:
            outcome = "review"

        confidence = _coerce_confidence(parsed.get("confidence"))
        summary = str(parsed.get("summary") or "Gemini review completed.").strip()
        evidence_snippets = [
            str(item).strip()
            for item in (parsed.get("evidence_snippets") or [])
            if str(item).strip()
        ][:3]

        return GeminiReviewResult(
            outcome=outcome,
            confidence=confidence,
            summary=summary,
            evidence_snippets=evidence_snippets,
        )

    def answer_contract_question(
        self,
        *,
        contract_id: str,
        messages: list[dict[str, str]],
        documents: list[dict[str, str]],
    ) -> GeminiContractChatResult:
        if not self._api_key:
            raise GeminiError("GEMINI_API_KEY is not configured")
        if not self._model:
            raise GeminiError("GEMINI_MODEL is not configured")

        prompt = _build_contract_chat_prompt(
            contract_id=contract_id,
            messages=messages,
            documents=documents,
        )
        payload = {
            "contents": [{"role": "user", "parts": [{"text": prompt}]}],
            "generationConfig": {"temperature": 0.15, "responseMimeType": "application/json"},
        }
        endpoint = (
            "https://generativelanguage.googleapis.com/v1beta/models/"
            f"{parse.quote(self._model, safe='')}:generateContent?key={parse.quote(self._api_key, safe='')}"
        )
        req = request.Request(
            endpoint,
            data=json.dumps(payload).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST",
        )

        try:
            raw = self._requester(req, self._timeout_seconds)
        except error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise GeminiError(f"Gemini request failed with HTTP {exc.code}: {detail}") from exc
        except error.URLError as exc:
            raise GeminiError(f"Gemini request failed: {exc.reason}") from exc

        response_payload = json.loads(raw.decode("utf-8"))
        text = _extract_text(response_payload)
        if not text:
            raise GeminiError("Gemini response did not include contract chat content")

        parsed = json.loads(text)
        answer = str(parsed.get("answer") or "").strip()
        citations: list[GeminiContractChatCitation] = []
        for item in parsed.get("citations") or []:
            document_id = str((item or {}).get("document_id") or "").strip()
            snippet_text = str((item or {}).get("snippet_text") or "").strip()
            reason = str((item or {}).get("reason") or "").strip()
            if not document_id or not snippet_text:
                continue
            citations.append(
                GeminiContractChatCitation(
                    document_id=document_id,
                    snippet_text=snippet_text,
                    reason=reason,
                )
            )

        return GeminiContractChatResult(
            answer=answer or "I could not answer confidently from the contract text.",
            citations=citations[:4],
        )


def _default_requester(req: request.Request, timeout_seconds: float) -> bytes:
    with request.urlopen(req, timeout=timeout_seconds) as response:
        return response.read()


def _extract_text(payload: dict[str, Any]) -> str:
    for candidate in payload.get("candidates") or []:
        content = candidate.get("content") or {}
        for part in content.get("parts") or []:
            text = str(part.get("text") or "").strip()
            if text:
                return text
    return ""


def _coerce_confidence(value: Any) -> float:
    try:
        confidence = float(value)
    except (TypeError, ValueError):
        return 0.5
    if confidence < 0:
        return 0.0
    if confidence > 1:
        return 1.0
    return confidence


def _build_review_prompt(*, instructions: str, filename: str, document_text: str) -> str:
    return (
        "You are reviewing a legal contract against a user-defined guideline.\n"
        "Return only valid JSON with this exact shape:\n"
        '{"outcome":"match|missing|review","confidence":0.0,"summary":"...","evidence_snippets":["..."]}\n'
        "Rules:\n"
        "- Use outcome=match only when the contract clearly satisfies the guideline.\n"
        "- Use outcome=missing only when the contract clearly fails the guideline.\n"
        "- Use outcome=review when the answer is ambiguous, mixed, or risky.\n"
        "- confidence must be between 0 and 1.\n"
        "- summary must be one short sentence.\n"
        "- evidence_snippets must contain up to 3 verbatim snippets copied from the contract.\n"
        "- If there is not enough text to decide, use outcome=review.\n\n"
        f"Filename: {filename or 'unknown'}\n"
        f"Guideline instructions:\n{instructions.strip()}\n\n"
        f"Contract text:\n{document_text.strip()}"
    )


def _build_contract_chat_prompt(
    *,
    contract_id: str,
    messages: list[dict[str, str]],
    documents: list[dict[str, str]],
) -> str:
    rendered_messages = "\n".join(
        f"{str(message.get('role') or 'user').strip().upper()}: {str(message.get('content') or '').strip()}"
        for message in messages
        if str(message.get("content") or "").strip()
    )
    rendered_documents = "\n\n".join(
        (
            f"DOCUMENT_ID: {str(document.get('document_id') or '').strip()}\n"
            f"FILENAME: {str(document.get('filename') or '').strip() or 'unknown'}\n"
            "TEXT:\n"
            f"{str(document.get('text') or '').strip()}"
        )
        for document in documents
        if str(document.get("document_id") or "").strip() and str(document.get("text") or "").strip()
    )
    return (
        "You answer questions about a contract using only the provided document text.\n"
        "Return only valid JSON with this exact shape:\n"
        '{"answer":"...","citations":[{"document_id":"...","snippet_text":"...","reason":"..."}]}\n'
        "Rules:\n"
        "- Keep the answer concise and grounded in the provided text.\n"
        "- If the answer is unclear, say so plainly.\n"
        "- citations must contain up to 4 supporting snippets copied verbatim from the documents.\n\n"
        f"CONTRACT_ID: {contract_id}\n\n"
        f"MESSAGES:\n{rendered_messages}\n\n"
        f"DOCUMENTS:\n{rendered_documents}"
    )
