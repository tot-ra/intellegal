from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class SearchSectionsResultItem(BaseModel):
    document_id: str
    page_number: int
    chunk_id: str | None = None
    score: float
    snippet_text: str


class SearchSectionsResult(BaseModel):
    items: list[SearchSectionsResultItem] = Field(default_factory=list)
    diagnostics: dict[str, Any] = Field(default_factory=dict)
