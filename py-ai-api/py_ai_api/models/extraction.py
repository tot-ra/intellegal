from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from pydantic import BaseModel, Field


class PageExtraction(BaseModel):
    page_number: int
    text: str
    char_count: int
    confidence: float
    source: str


class ExtractionResult(BaseModel):
    mime_type: str
    text: str
    pages: list[PageExtraction]
    confidence: float
    diagnostics: dict[str, Any] = Field(default_factory=dict)


@dataclass
class OCRText:
    text: str
    confidence: float | None
    diagnostics: dict[str, Any]
