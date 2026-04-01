from __future__ import annotations

import pytest
from fastapi import HTTPException, status

from py_ai_api.api.auth import require_internal_service_auth
from py_ai_api.config import Settings

pytestmark = pytest.mark.unit


def test_require_internal_service_auth_rejects_missing_configuration() -> None:
    with pytest.raises(HTTPException) as err:
        require_internal_service_auth(Settings(internal_service_token=""), token="shared-secret")

    assert err.value.status_code == status.HTTP_503_SERVICE_UNAVAILABLE
    assert err.value.detail == "Service auth is not configured"


def test_require_internal_service_auth_rejects_wrong_token() -> None:
    with pytest.raises(HTTPException) as err:
        require_internal_service_auth(Settings(internal_service_token="shared-secret"), token="wrong")

    assert err.value.status_code == status.HTTP_401_UNAUTHORIZED
    assert err.value.detail == "Unauthorized"


def test_require_internal_service_auth_accepts_matching_token() -> None:
    assert require_internal_service_auth(Settings(internal_service_token="shared-secret"), token="shared-secret") is None
