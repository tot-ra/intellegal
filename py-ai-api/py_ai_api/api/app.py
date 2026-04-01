from __future__ import annotations

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from ..config import get_settings
from ..logging import configure_logging
from ..storage.postgres import check_connection
from ..storage.qdrant import QdrantService
from .routes.internal import router as internal_router

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = get_settings()
    configure_logging(settings.log_level)
    if settings.database_startup_check:
        check_connection(settings.database_url)
    qdrant_collection = settings.qdrant_collection_name
    if settings.qdrant_startup_check_enabled:
        qdrant_service = QdrantService(settings)
        qdrant_service.startup_check()
        app.state.qdrant_service = qdrant_service
        qdrant_collection = qdrant_service.collection_name
    logger.info(
        "starting service",
        extra={
            "app_env": settings.app_env,
            "qdrant_collection": qdrant_collection,
            "qdrant_url": settings.qdrant_url,
        },
    )
    yield


app = FastAPI(title="Python AI API", version="0.1.0", lifespan=lifespan)
app.include_router(internal_router)
