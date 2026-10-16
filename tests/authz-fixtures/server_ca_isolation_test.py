#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Проба: корень удостоверяющего слушателя выдачи — СВОЙ у каждого посева (kacho#3238).

ПРЕДМЕТ. Посев вынимает корень внутреннего удостоверяющего из Secret стенда и
проверяет им лист слушателя выдачи (`POST /iam/v1/token`). Файл корня лежал по
ОБЩЕМУ пути `/tmp/kaname-server-ca/ca.crt`: два посева на одной машине против
двух стендов переписывали его друг другу, и обмен падал
`CERTIFICATE_VERIFY_FAILED: unable to get local issuer certificate` — проверка
подписи честно отказала чужому корню (замер 2026-10-16: файл прогона t3238-sc
содержал корень, выпущенный на 82 минуты позже корня этого стенда).

Обязано: (A) без переменных каждый процесс получает СВОЙ каталог, не общий
литерал; (B) два процесса — два разных каталога; (C) `IAM_SERVER_CA_DIR`
соблюдается — так прогонщик отдаёт посеву и перевыпуску ОДИН каталог своего
прогона; (D) `IAM_SERVER_CA_FILE` соблюдается дословно; (E) каталог по умолчанию
снимается при выходе процесса; (F) проверка подписи на месте — `verify_mode`
CERT_REQUIRED у контекста обмена.

СЕТИ НЕТ: импорт модуля в отдельном процессе. Исполняет
`.github/scripts/run-python-probes.py` (образец tests/authz-fixtures/*_test.py).
"""
from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile

HERE = pathlib.Path(__file__).resolve().parent
SHARED = "/tmp/kaname-server-ca"
FAILS: list[str] = []
CHECKS = [0]

PROBE = r"""
import json, os, ssl, sys
sys.path.insert(0, sys.argv[1])
import mint_rs256 as m
print(json.dumps({"file": m.IAM_SERVER_CA_FILE, "dir": os.path.dirname(m.IAM_SERVER_CA_FILE)}))
"""


def check(name: str, cond: bool, detail: str = "") -> None:
    CHECKS[0] += 1
    print(f"{'ok  ' if cond else 'FAIL'} {name}{(' — ' + detail) if detail and not cond else ''}")
    if not cond:
        FAILS.append(name)


def probe(extra: dict[str, str]) -> dict:
    env = {k: v for k, v in os.environ.items()
           if k not in ("IAM_SERVER_CA_DIR", "IAM_SERVER_CA_FILE")}
    env.update(extra)
    out = subprocess.run([sys.executable, "-c", PROBE, str(HERE)],
                         capture_output=True, text=True, env=env)
    if out.returncode != 0:
        raise RuntimeError(f"импорт mint_rs256 упал: {out.stderr.strip()[-400:]}")
    return json.loads(out.stdout.strip().splitlines()[-1])


def self_signed_root() -> bytes:
    import datetime
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(x509.oid.NameOID.COMMON_NAME, "ca-isolation-probe")])
    now = datetime.datetime.now(datetime.timezone.utc)
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
            .public_key(key.public_key()).serial_number(1)
            .not_valid_before(now).not_valid_after(now + datetime.timedelta(days=1))
            .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
            .sign(key, hashes.SHA256()))
    return cert.public_bytes(serialization.Encoding.PEM)


def main() -> int:
    a, b = probe({}), probe({})
    check("A1 по умолчанию каталог не общий литерал", a["dir"] != SHARED, a["dir"])
    check("A2 по умолчанию файл не под общим литералом",
          not a["file"].startswith(SHARED + "/"), a["file"])
    check("B1 два процесса — два каталога", a["dir"] != b["dir"], f"{a['dir']} = {b['dir']}")
    check("E1 каталог по умолчанию снят при выходе", not os.path.exists(a["dir"]), a["dir"])

    with tempfile.TemporaryDirectory(prefix="ca-iso-") as tmp:
        d = os.path.join(tmp, "run")
        c = probe({"IAM_SERVER_CA_DIR": d})
        check("C1 IAM_SERVER_CA_DIR соблюдается", c["file"] == os.path.join(d, "ca.crt"), c["file"])
        f = os.path.join(tmp, "given.crt")
        e = probe({"IAM_SERVER_CA_FILE": f})
        check("D1 IAM_SERVER_CA_FILE соблюдается дословно", e["file"] == f, e["file"])

    sys.path.insert(0, str(HERE))
    import ssl
    import mint_rs256 as m
    with tempfile.TemporaryDirectory(prefix="ca-iso-") as tmp:
        root = os.path.join(tmp, "ca.crt")
        with open(root, "wb") as fh:
            fh.write(self_signed_root())
        ctx = m.platform_tls_context(ca_file=root)
        check("F1 проверка подписи у контекста обмена на месте",
              ctx.verify_mode == ssl.CERT_REQUIRED, str(ctx.verify_mode))
        check("F2 корень контекста — ровно поданный файл", len(ctx.get_ca_certs()) == 1,
              str(len(ctx.get_ca_certs())))

    print(f"server_ca_isolation: проверок {CHECKS[0]}, провалено {len(FAILS)}")
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
