from __future__ import annotations

import logging

import pytest

from py_ai_api.logging import configure_logging

pytestmark = pytest.mark.unit


def test_configure_logging_uses_uppercased_level_and_format(monkeypatch) -> None:
    basic_config_calls: list[dict[str, object]] = []

    def _fake_basic_config(**kwargs) -> None:
        basic_config_calls.append(kwargs)

    monkeypatch.setattr(logging, "basicConfig", _fake_basic_config)

    configure_logging("debug")

    assert basic_config_calls == [
        {
            "level": logging.DEBUG,
            "format": "%(asctime)s %(levelname)s %(name)s %(message)s",
        }
    ]


def test_configure_logging_falls_back_to_info_for_unknown_level(monkeypatch) -> None:
    basic_config_calls: list[dict[str, object]] = []
    monkeypatch.setattr(logging, "basicConfig", lambda **kwargs: basic_config_calls.append(kwargs))

    configure_logging("not-a-level")

    assert basic_config_calls[0]["level"] == logging.INFO
