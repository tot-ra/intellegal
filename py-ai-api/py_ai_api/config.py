from __future__ import annotations

from functools import lru_cache

from pydantic import model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(extra="ignore")

    app_name: str = "python-ai-api"
    app_env: str = "dev"
    log_level: str = "INFO"

    internal_service_token: str = ""
    database_url: str = "postgresql://app:app@postgres:5432/legal_doc_intel?sslmode=disable"
    database_startup_check: bool = False

    qdrant_url: str = "http://qdrant:6333"
    qdrant_api_key: str | None = None
    qdrant_collection_name: str = "document_chunks"
    qdrant_vector_size: int = 1536
    qdrant_distance: str = "cosine"
    qdrant_startup_timeout_seconds: float = 5.0
    qdrant_startup_check_enabled: bool = True
    index_chunk_size: int = 800
    index_chunk_overlap: int = 120
    gemini_api_key: str = ""
    gemini_model: str = "gemini-2.5-flash"
    gemini_timeout_seconds: float = 30.0

    @model_validator(mode="after")
    def ensure_database_sslmode(self) -> "Settings":
        if "sslmode=" in self.database_url:
            return self

        separator = "&" if "?" in self.database_url else "?"
        self.database_url = f"{self.database_url}{separator}sslmode=disable"
        return self


@lru_cache
def get_settings() -> Settings:
    return Settings()
