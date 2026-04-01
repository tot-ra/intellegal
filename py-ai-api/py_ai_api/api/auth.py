import logging
import secrets
from typing import Annotated

from fastapi import Depends, Header, HTTPException, status

from ..config import Settings, get_settings

logger = logging.getLogger(__name__)


def require_internal_service_auth(
    settings: Annotated[Settings, Depends(get_settings)],
    token: Annotated[str | None, Header(alias="X-Internal-Service-Token")] = None,
) -> None:
    if not settings.internal_service_token:
        logger.error("INTERNAL_SERVICE_TOKEN is not configured")
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Service auth is not configured",
        )

    if token is None or not secrets.compare_digest(token, settings.internal_service_token):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Unauthorized",
        )
