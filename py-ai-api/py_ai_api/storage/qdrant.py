import logging
from dataclasses import dataclass
from typing import Any

from qdrant_client import QdrantClient, models

from ..config import Settings

logger = logging.getLogger(__name__)

_DISTANCE_MAP: dict[str, models.Distance] = {
    "cosine": models.Distance.COSINE,
    "dot": models.Distance.DOT,
    "euclid": models.Distance.EUCLID,
    "manhattan": models.Distance.MANHATTAN,
}


@dataclass(frozen=True)
class CollectionSchema:
    name: str
    vector_size: int
    distance: models.Distance
    payload_fields: dict[str, models.PayloadSchemaType]


def build_collection_schema(settings: Settings) -> CollectionSchema:
    distance = _DISTANCE_MAP.get(settings.qdrant_distance.lower())
    if distance is None:
        raise ValueError(
            "invalid QDRANT_DISTANCE value "
            f"{settings.qdrant_distance!r}. Valid values: {', '.join(sorted(_DISTANCE_MAP))}."
        )

    return CollectionSchema(
        name=settings.qdrant_collection_name,
        vector_size=settings.qdrant_vector_size,
        distance=distance,
        payload_fields={
            "document_id": models.PayloadSchemaType.KEYWORD,
            "document_version_id": models.PayloadSchemaType.KEYWORD,
            "chunk_id": models.PayloadSchemaType.INTEGER,
            "page_number": models.PayloadSchemaType.INTEGER,
            "checksum": models.PayloadSchemaType.KEYWORD,
            "source_uri": models.PayloadSchemaType.KEYWORD,
        },
    )


class QdrantService:
    def __init__(self, settings: Settings, client: QdrantClient | None = None) -> None:
        self._schema = build_collection_schema(settings)
        self._client = client or QdrantClient(
            url=settings.qdrant_url,
            api_key=settings.qdrant_api_key,
            timeout=settings.qdrant_startup_timeout_seconds,
        )

    @property
    def collection_name(self) -> str:
        return self._schema.name

    def startup_check(self) -> None:
        self._client.get_collections()
        self.ensure_collection()
        logger.info("qdrant startup checks passed", extra={"collection": self.collection_name})

    def ensure_collection(self) -> None:
        if not self._client.collection_exists(self.collection_name):
            self._client.create_collection(
                collection_name=self.collection_name,
                vectors_config=models.VectorParams(
                    size=self._schema.vector_size,
                    distance=self._schema.distance,
                ),
                on_disk_payload=True,
            )
            logger.info("created qdrant collection", extra={"collection": self.collection_name})

        for field_name, field_schema in self._schema.payload_fields.items():
            self._client.create_payload_index(
                collection_name=self.collection_name,
                field_name=field_name,
                field_schema=field_schema,
                wait=True,
            )

    def count_chunks(self, *, document_id: str, checksum: str) -> int:
        response = self._client.count(
            collection_name=self.collection_name,
            count_filter=models.Filter(
                must=[
                    models.FieldCondition(key="document_id", match=models.MatchValue(value=document_id)),
                    models.FieldCondition(key="checksum", match=models.MatchValue(value=checksum)),
                ]
            ),
            exact=True,
        )
        return int(response.count)

    def delete_document_chunks(self, *, document_id: str) -> None:
        self._client.delete(
            collection_name=self.collection_name,
            points_selector=models.FilterSelector(
                filter=models.Filter(
                    must=[models.FieldCondition(key="document_id", match=models.MatchValue(value=document_id))]
                )
            ),
            wait=True,
        )

    def upsert_chunks(self, points: list[dict[str, Any]]) -> None:
        if not points:
            return

        qdrant_points: list[models.PointStruct] = []
        for point in points:
            qdrant_points.append(
                models.PointStruct(
                    id=point["id"],
                    vector=point["vector"],
                    payload=point["payload"],
                )
            )

        self._client.upsert(
            collection_name=self.collection_name,
            points=qdrant_points,
            wait=True,
        )

    def get_document_chunks(self, *, document_id: str, limit: int = 64) -> list[dict[str, Any]]:
        points, _ = self._client.scroll(
            collection_name=self.collection_name,
            scroll_filter=models.Filter(
                must=[models.FieldCondition(key="document_id", match=models.MatchValue(value=document_id))]
            ),
            limit=max(1, limit),
            with_payload=True,
            with_vectors=False,
        )

        chunks: list[dict[str, Any]] = []
        for point in points:
            payload = point.payload or {}
            text = str(payload.get("text") or "").strip()
            if not text:
                continue
            chunks.append(
                {
                    "document_id": str(payload.get("document_id") or document_id),
                    "chunk_id": payload.get("chunk_id"),
                    "page_number": int(payload.get("page_number") or 0),
                    "text": text,
                }
            )
        return chunks

    def search_chunks(
        self,
        *,
        query_vector: list[float],
        document_ids: list[str] | None = None,
        limit: int = 10,
    ) -> list[dict[str, Any]]:
        if not query_vector:
            return []

        target_documents = {doc_id for doc_id in (document_ids or []) if doc_id}
        search_limit = max(1, limit * 4)
        raw_results: list[Any]
        if hasattr(self._client, "query_points"):
            try:
                response = self._client.query_points(
                    collection_name=self.collection_name,
                    query=query_vector,
                    limit=search_limit,
                    with_payload=True,
                    with_vectors=False,
                )
                raw_results = list(getattr(response, "points", []) or [])
            except AttributeError:
                raw_results = self._client.search(
                    collection_name=self.collection_name,
                    query_vector=query_vector,
                    limit=search_limit,
                    with_payload=True,
                    with_vectors=False,
                )
        else:
            raw_results = self._client.search(
                collection_name=self.collection_name,
                query_vector=query_vector,
                limit=search_limit,
                with_payload=True,
                with_vectors=False,
            )

        matches: list[dict[str, Any]] = []
        for result in raw_results:
            payload = result.payload or {}
            document_id = str(payload.get("document_id") or "").strip()
            if not document_id:
                continue
            if target_documents and document_id not in target_documents:
                continue

            text = str(payload.get("text") or "").strip()
            if not text:
                continue

            chunk_id = payload.get("chunk_id")
            page_number = int(payload.get("page_number") or 0)
            matches.append(
                {
                    "document_id": document_id,
                    "chunk_id": str(chunk_id) if chunk_id is not None else None,
                    "page_number": page_number if page_number > 0 else 1,
                    "text": text,
                    "score": float(getattr(result, "score", 0.0) or 0.0),
                }
            )
            if len(matches) >= limit:
                break

        return matches
