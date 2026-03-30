FROM python:3.14.3-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY pyproject.toml README.md ./
COPY py_ai_api ./py_ai_api

RUN pip install --upgrade pip && pip install .

EXPOSE 8000

CMD ["uvicorn", "py_ai_api.main:app", "--host", "0.0.0.0", "--port", "8000"]
