import pytest

from py_ai_api.config import Settings

pytestmark = pytest.mark.unit


def test_database_url_adds_sslmode_disable() -> None:
    settings = Settings(database_url="postgresql://app:app@localhost:5432/legal_doc_intel")
    assert settings.database_url.endswith("sslmode=disable")
