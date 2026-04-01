from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from pydantic import BaseModel, Field


class IndexPageInput(BaseModel):
    page_number: int
    text: str


class IndexingResult(BaseModel):
    document_id: str
    checksum: str
    chunk_count: int
    indexed: bool
    skipped_reason: str | None = None
    diagnostics: dict[str, Any] = Field(default_factory=dict)


@dataclass
class Chunk:
    chunk_id: int
    page_number: int
    text: str
