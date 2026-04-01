FROM python:3.14.3-slim AS base

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY pyproject.toml README.md ./
COPY py_ai_api ./py_ai_api

RUN apt-get update \
    && apt-get install -y --no-install-recommends poppler-utils tesseract-ocr \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --upgrade pip \
    && pip install .

EXPOSE 8000

CMD ["uvicorn", "py_ai_api.api.app:app", "--host", "0.0.0.0", "--port", "8000"]

FROM base AS dev

CMD ["uvicorn", "py_ai_api.api.app:app", "--host", "0.0.0.0", "--port", "8000", "--reload", "--reload-dir", "/app/py_ai_api"]
