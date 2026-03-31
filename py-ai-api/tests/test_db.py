from __future__ import annotations

import pytest

from py_ai_api import db

pytestmark = pytest.mark.unit


class _FakeCursor:
    def __init__(self, *, fetchone_result=None, fetchall_result=None) -> None:
        self.fetchone_result = fetchone_result
        self.fetchall_result = fetchall_result or []
        self.execute_calls: list[tuple[object, object]] = []
        self.executemany_calls: list[tuple[object, object]] = []

    def __enter__(self) -> "_FakeCursor":
        return self

    def __exit__(self, *_args) -> bool:
        return False

    def execute(self, query, params=None) -> None:
        self.execute_calls.append((query, params))

    def executemany(self, query, params) -> None:
        self.executemany_calls.append((query, params))

    def fetchone(self):
        return self.fetchone_result

    def fetchall(self):
        return self.fetchall_result


class _FakeConnection:
    def __init__(self, cursor: _FakeCursor) -> None:
        self._cursor = cursor

    def __enter__(self) -> "_FakeConnection":
        return self

    def __exit__(self, *_args) -> bool:
        return False

    def cursor(self) -> _FakeCursor:
        return self._cursor


def test_check_connection_executes_simple_probe(monkeypatch) -> None:
    cursor = _FakeCursor(fetchone_result=(1,))
    connect_calls: list[tuple[str, object]] = []

    def _fake_connect(database_url: str, row_factory=None):
        connect_calls.append((database_url, row_factory))
        return _FakeConnection(cursor)

    monkeypatch.setattr(db.psycopg, "connect", _fake_connect)

    db.check_connection("postgresql://db")

    assert connect_calls == [("postgresql://db", None)]
    assert cursor.execute_calls == [("SELECT 1", None)]


def test_upsert_document_chunks_replaces_existing_rows(monkeypatch) -> None:
    cursor = _FakeCursor()

    def _fake_connect(database_url: str, row_factory=None):
        assert database_url == "postgresql://db"
        assert row_factory is None
        return _FakeConnection(cursor)

    monkeypatch.setattr(db.psycopg, "connect", _fake_connect)

    store = db.ChunkSearchStore("postgresql://db")
    store.upsert_document_chunks(
        document_id="doc-1",
        checksum="sha-001",
        chunks=[
            {"chunk_id": "7", "page_number": "2", "snippet_text": "Clause text"},
            {"chunk_id": 8, "page_number": 3, "snippet_text": "Second snippet"},
        ],
    )

    assert cursor.execute_calls == [
        ("DELETE FROM indexed_document_chunks WHERE document_id = %s", ("doc-1",))
    ]
    assert len(cursor.executemany_calls) == 1
    query, rows = cursor.executemany_calls[0]
    assert "INSERT INTO indexed_document_chunks" in query
    assert rows == [
        ("doc-1", "sha-001", 7, 2, "Clause text"),
        ("doc-1", "sha-001", 8, 3, "Second snippet"),
    ]


def test_upsert_document_chunks_skips_insert_when_no_chunks(monkeypatch) -> None:
    cursor = _FakeCursor()
    monkeypatch.setattr(db.psycopg, "connect", lambda *_args, **_kwargs: _FakeConnection(cursor))

    store = db.ChunkSearchStore("postgresql://db")
    store.upsert_document_chunks(document_id="doc-1", checksum="sha-001", chunks=[])

    assert cursor.execute_calls == [
        ("DELETE FROM indexed_document_chunks WHERE document_id = %s", ("doc-1",))
    ]
    assert cursor.executemany_calls == []


def test_search_sections_strict_normalizes_database_rows(monkeypatch) -> None:
    cursor = _FakeCursor(
        fetchall_result=[
            {
                "document_id": "doc-1",
                "page_number": 4,
                "chunk_id": 9,
                "score": 0.8234567,
                "snippet_text": "Strict match",
            },
            {
                "document_id": None,
                "page_number": None,
                "chunk_id": None,
                "score": None,
                "snippet_text": None,
            },
        ]
    )
    connect_calls: list[tuple[str, object]] = []

    def _fake_connect(database_url: str, row_factory=None):
        connect_calls.append((database_url, row_factory))
        return _FakeConnection(cursor)

    monkeypatch.setattr(db.psycopg, "connect", _fake_connect)

    store = db.ChunkSearchStore("postgresql://db")
    result = store.search_sections_strict(
        query_text="payment terms",
        exclude_texts=["late fee", "termination"],
        document_ids=["doc-1", "", None],
        limit=0,
    )

    assert connect_calls == [("postgresql://db", db.dict_row)]
    assert len(cursor.execute_calls) == 1
    query, params = cursor.execute_calls[0]
    assert "websearch_to_tsquery" in query
    assert params == {
        "query_text": "payment terms",
        "excluded_terms": ["late fee", "termination"],
        "document_ids": ["doc-1"],
        "limit": 1,
    }
    assert result == [
        {
            "document_id": "doc-1",
            "page_number": 4,
            "chunk_id": "9",
            "score": 0.823457,
            "text": "Strict match",
        },
        {
            "document_id": "",
            "page_number": 1,
            "chunk_id": None,
            "score": 0.0,
            "text": "",
        },
    ]
