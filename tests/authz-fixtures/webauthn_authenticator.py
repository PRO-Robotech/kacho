#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Подставной аутентификатор ключа доступа для посева (kacho#3334).

ПРЕДМЕТ. Кейсам сброса ключей (`:resetAccessKeys`) нужен человек, у которого ключ ЕСТЬ:
у человека без ключей служба отказывает синхронно (`ACCESS_KEYS_NOT_ENROLLED`), сессии
не гасятся, и кейс проверял бы не сброс. Ключ человеку заводит церемония браузера
(`BeginRegistration` → аутентификатор → `FinishRegistration`); браузера у посева нет,
поэтому церемонию исполняет этот модуль — тем же составом, что настоящий аутентификатор:
ключ ECDSA P-256 (алгоритм −7), результат `none`-аттестации, `clientDataJSON` с
испытанием и происхождением, `authenticatorData` с флагами присутствия и проверки
пользователя и COSE-ключом.

Что модуль НЕ делает: не подделывает ничего. Каждое поле собрано из того, что служба
назвала в испытании (имя доверяющей стороны, испытание), и из происхождения, которое
вызывающий передаёт явно. Отступлений от нормы здесь нет — ключ годный, и служба
обязана принять его как настоящий; отказ службы — находка, а не «условие не создано».

Образец состава — подставной аутентификатор самой службы доступа
(`internal/webauthnverify/webauthntest`); здесь он на Python, потому что посев на нём.
Зависимость — `cryptography` (стандартный набор инструментов прогонщика).
"""
from __future__ import annotations

import base64
import hashlib
import json
import os
import struct

from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import ec

ALG_ES256 = -7

FLAG_UP = 0x01
FLAG_UV = 0x04
FLAG_AT = 0x40


def b64u(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


def b64_any(text: str) -> bytes:
    """Байты поля `bytes` в JSON протобуфа: допустимы обе азбуки, с отступом и без."""
    t = text.replace("-", "+").replace("_", "/")
    return base64.b64decode(t + "=" * (-len(t) % 4))


# ── CBOR: ровно то подмножество, что нужно (RFC 8949 §3, определённая длина) ──────


def _head(major: int, n: int) -> bytes:
    if n < 24:
        return bytes([major << 5 | n])
    if n < 256:
        return bytes([major << 5 | 24, n])
    if n < 65536:
        return bytes([major << 5 | 25]) + struct.pack(">H", n)
    return bytes([major << 5 | 26]) + struct.pack(">I", n)


def cbor(v) -> bytes:
    if isinstance(v, bool):
        raise TypeError("булево значение в этом подмножестве CBOR не нужно")
    if isinstance(v, int):
        return _head(0, v) if v >= 0 else _head(1, -1 - v)
    if isinstance(v, bytes):
        return _head(2, len(v)) + v
    if isinstance(v, str):
        raw = v.encode("utf-8")
        return _head(3, len(raw)) + raw
    if isinstance(v, dict):
        return _head(5, len(v)) + b"".join(cbor(k) + cbor(x) for k, x in v.items())
    raise TypeError(f"тип {type(v).__name__} в этом подмножестве CBOR не нужен")


class Authenticator:
    """Один ключ одного человека: ES256, `none`-аттестация, счётчик ноль."""

    def __init__(self) -> None:
        self._key = ec.generate_private_key(ec.SECP256R1())
        self.credential_id = os.urandom(32)

    def _cose_key(self) -> bytes:
        nums = self._key.public_key().public_numbers()
        return cbor({1: 2, 3: ALG_ES256, -1: 1,
                     -2: nums.x.to_bytes(32, "big"), -3: nums.y.to_bytes(32, "big")})

    def create(self, challenge: bytes, rp_id: str, origin: str) -> dict:
        """Результат церемонии регистрации в форме тела `FinishRegistration.credential`."""
        client_data = json.dumps({"type": "webauthn.create", "challenge": b64u(challenge),
                                  "origin": origin, "crossOrigin": False},
                                 separators=(",", ":")).encode("utf-8")
        auth_data = (hashlib.sha256(rp_id.encode("utf-8")).digest()
                     + bytes([FLAG_UP | FLAG_UV | FLAG_AT]) + struct.pack(">I", 0)
                     + bytes(16)
                     + struct.pack(">H", len(self.credential_id)) + self.credential_id
                     + self._cose_key())
        attestation = cbor({"fmt": "none", "attStmt": {}, "authData": auth_data})
        return {"id": base64.b64encode(self.credential_id).decode("ascii"),
                "clientDataJson": base64.b64encode(client_data).decode("ascii"),
                "attestationObject": base64.b64encode(attestation).decode("ascii")}


# ── самопроверка: CBOR сверяется с векторами RFC 8949 Приложение A ────────────────


def _self_test() -> int:
    vectors = [
        (0, "00"), (23, "17"), (24, "1818"), (100, "1864"), (1000, "1903e8"),
        (-1, "20"), (-7, "26"), (-100, "3863"),
        (b"\x01\x02\x03\x04", "4401020304"), ("IETF", "6449455446"),
        ({}, "a0"), ({1: 2, 3: 4}, "a201020304"),
    ]
    bad = [f"{v!r}: {cbor(v).hex()} != {h}" for v, h in vectors if cbor(v).hex() != h]
    a = Authenticator()
    res = a.create(b"c" * 32, "kacho.test", "https://kacho.test")
    obj = b64_any(res["attestationObject"])
    cd = json.loads(b64_any(res["clientDataJson"]))
    if cd["type"] != "webauthn.create" or cd["origin"] != "https://kacho.test":
        bad.append(f"clientDataJSON: {cd}")
    if hashlib.sha256(b"kacho.test").digest() not in obj:
        bad.append("authenticatorData не несёт хэш имени доверяющей стороны")
    if b64_any(res["id"]) != a.credential_id:
        bad.append("идентификатор удостоверения не круглый")
    for b in bad:
        print("ОТКАЗ", b)
    print(f"перепись: проверок {len(vectors) + 3}, отказов {len(bad)}")
    return 1 if bad else 0


if __name__ == "__main__":
    import sys
    sys.exit(_self_test())
