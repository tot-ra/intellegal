from __future__ import annotations

import psycopg
from psycopg.rows import dict_row


def check_connection(database_url: str) -> None:
    with psycopg.connect(database_url) as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT 1")
            cur.fetchone()


class ChunkSearchStore:
    def __init__(self, database_url: str) -> None:
        self._database_url = database_url

    def upsert_document_chunks(
        self,
        *,
        document_id: str,
        checksum: str,
        chunks: list[dict[str, str | int]],
    ) -> None:
        with psycopg.connect(self._database_url) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "DELETE FROM indexed_document_chunks WHERE document_id = %s",
                    (document_id,),
                )
                if chunks:
                    cur.executemany(
                        """
                        INSERT INTO indexed_document_chunks
                            (document_id, checksum, chunk_id, page_number, snippet_text)
                        VALUES (%s, %s, %s, %s, %s)
                        """,
                        [
                            (
                                document_id,
                                checksum,
                                int(chunk["chunk_id"]),
                                int(chunk["page_number"]),
                                str(chunk["snippet_text"]),
                            )
                            for chunk in chunks
                        ],
                    )

    def search_sections_strict(
        self,
        *,
        query_text: str,
        exclude_texts: list[str] | None = None,
        document_ids: list[str] | None,
        limit: int,
    ) -> list[dict[str, str | int | float | None]]:
        ids = [doc_id for doc_id in (document_ids or []) if doc_id]
        excluded_terms = [term.strip().lower() for term in (exclude_texts or []) if term and term.strip()]
        query = """
            WITH input AS (
                SELECT CASE
                           WHEN %(query_text)s <> '' THEN websearch_to_tsquery('simple', %(query_text)s)
                           ELSE NULL
                       END AS tsq,
                       lower(%(query_text)s) AS qnorm,
                       %(query_text)s <> '' AS has_positive_query
            ),
            ranked AS (
                SELECT
                    c.document_id,
                    c.page_number,
                    c.chunk_id::text AS chunk_id,
                    c.snippet_text,
                    (
                        CASE
                            WHEN i.has_positive_query AND c.search_vector @@ i.tsq THEN ts_rank_cd(c.search_vector, i.tsq, 32)
                            ELSE 0
                        END * 0.70
                    ) +
                    (
                        CASE
                            WHEN i.has_positive_query THEN similarity(lower(c.snippet_text), i.qnorm)
                            ELSE 0
                        END * 0.20
                    ) +
                    (
                        CASE
                            WHEN i.has_positive_query THEN word_similarity(i.qnorm, lower(c.snippet_text))
                            ELSE 0
                        END * 0.10
                    ) AS score
                FROM indexed_document_chunks AS c
                CROSS JOIN input AS i
                WHERE (%(document_ids)s::text[] IS NULL OR c.document_id = ANY(%(document_ids)s))
                  AND NOT EXISTS (
                      SELECT 1
                      FROM unnest(COALESCE(%(excluded_terms)s::text[], ARRAY[]::text[])) AS excluded(term)
                      WHERE position(excluded.term in lower(c.snippet_text)) > 0
                  )
                  AND (
                      NOT i.has_positive_query
                      OR c.search_vector @@ i.tsq
                      OR similarity(lower(c.snippet_text), i.qnorm) >= 0.18
                      OR word_similarity(i.qnorm, lower(c.snippet_text)) >= 0.35
                  )
            )
            SELECT
                document_id,
                page_number,
                chunk_id,
                score,
                snippet_text
            FROM ranked
            ORDER BY score DESC, document_id ASC, page_number ASC
            LIMIT %(limit)s
        """

        with psycopg.connect(self._database_url, row_factory=dict_row) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    query,
                    {
                        "query_text": query_text,
                        "excluded_terms": excluded_terms if excluded_terms else None,
                        "document_ids": ids if ids else None,
                        "limit": max(1, limit),
                    },
                )
                rows = cur.fetchall()

        return [
            {
                "document_id": str(row.get("document_id") or ""),
                "page_number": int(row.get("page_number") or 1),
                "chunk_id": str(row.get("chunk_id")) if row.get("chunk_id") is not None else None,
                "score": round(float(row.get("score") or 0.0), 6),
                "text": str(row.get("snippet_text") or ""),
            }
            for row in rows
        ]
