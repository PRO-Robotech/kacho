#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Имя из SAN листа собственного фронта и якорь доверия к нему — из посадки.

ПРЕДМЕТ (kacho#3315). Собственный фронт службы доступа пробрасывается на петлю.
Его лист несёт в SAN только DNS-имена Service (`<svc>.<ns>.svc` и соседние
формы), поэтому адрес проб `https://127.0.0.1:<порт>` проверку имени не
проходит, и newman отдаёт `errored` — ни красного, ни зелёного. Проверку не
ослабляют: адрес строится от ИМЕНИ из SAN, имя пинится на петлю
(`lib/pin-resolve.cjs`, форма `curl --resolve`), цепочка проверяется по УЦ
посадки.

ОТКУДА ИМЯ И УЦ. Из секретов листов пространства (`type=kubernetes.io/tls`):
секрет, чей лист называет в SAN Service этого фронта в этом пространстве, —
лист фронта; его `ca.crt` — УЦ, выпустивший лист (cert-manager кладёт его
рядом). Имя не выписывается и не выводится из имени Service догадкой: берётся
та форма, что стоит в SAN, в порядке `<svc>.<ns>.svc`,
`<svc>.<ns>.svc.cluster.local`, `<svc>.<ns>`.

ИСХОДЫ:
  0 — имя на stdout, УЦ записан в --ca-out;
  3 — УСЛОВИЕ НЕ СОЗДАНО (причина — в stderr): секреты не прочитаны; ни один
      лист не называет Service в SAN; таких листов больше одного (чей лист
      служит фронту — неоднозначно); у листа нет `ca.crt`. Адрес НЕ строится от
      IP «на всякий случай»: вызывающий переменную не инъектирует, и кейс сам
      скажет третьим исходом;
  2 — вызов не по форме.

Разбор листа — `openssl x509 -ext subjectAltName`: зависимостей сверх
стандартной библиотеки и openssl у прогонщика нет.
"""
import argparse
import base64
import json
import os
import subprocess
import sys

UNMET = 3


def unmet(msg):
    print("own-front-tls-name: условие не создано — " + msg, file=sys.stderr)
    sys.exit(UNMET)


def san_dns(pem):
    """DNS-имена SAN первого листа PEM; None — лист не разобран."""
    try:
        out = subprocess.run(
            ["openssl", "x509", "-noout", "-ext", "subjectAltName"],
            input=pem, capture_output=True, check=True,
        ).stdout.decode()
    except (OSError, subprocess.CalledProcessError):
        return None
    names = []
    for part in out.replace("\n", ",").split(","):
        part = part.strip()
        if part.startswith("DNS:"):
            names.append(part[4:].strip().lower())
    return names


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("service")
    ap.add_argument("--namespace", required=True)
    ap.add_argument("--ca-out", required=True)
    a = ap.parse_args()
    svc, ns = a.service.lower(), a.namespace.lower()
    wanted = ["%s.%s.svc" % (svc, ns), "%s.%s.svc.cluster.local" % (svc, ns), "%s.%s" % (svc, ns)]

    kubectl = os.environ.get("KUBECTL", "kubectl")
    try:
        raw = subprocess.run(
            [kubectl, "-n", ns, "get", "secrets", "--field-selector", "type=kubernetes.io/tls", "-o", "json"],
            capture_output=True, check=True,
        ).stdout
        items = json.loads(raw).get("items") or []
    except (OSError, subprocess.CalledProcessError, ValueError) as e:
        unmet("секреты листов пространства %s не прочитаны (%s)" % (ns, type(e).__name__))

    hits = []
    for it in items:
        data = it.get("data") or {}
        crt = data.get("tls.crt")
        if not crt:
            continue
        try:
            pem = base64.b64decode(crt)
        except ValueError:
            continue
        names = san_dns(pem) or []
        name = next((w for w in wanted if w in names), None)
        if name:
            hits.append((it.get("metadata", {}).get("name", "?"), name, data.get("ca.crt") or ""))

    if not hits:
        unmet("ни один лист пространства %s не называет Service %s в SAN (искали %s; осмотрено секретов %d)"
              % (ns, svc, ", ".join(wanted), len(items)))
    if len(hits) > 1:
        unmet("Service %s называют в SAN %d листов (%s) — чей служит фронту, неоднозначно"
              % (svc, len(hits), ", ".join(h[0] for h in hits)))
    secret, name, ca = hits[0]
    if not ca:
        unmet("у листа secret/%s нет ca.crt — проверять цепочку не по чему" % secret)
    try:
        ca_pem = base64.b64decode(ca)
    except ValueError:
        unmet("ca.crt secret/%s не base64" % secret)
    with open(a.ca_out, "wb") as f:
        f.write(ca_pem)
    print("own-front-tls-name: %s — имя %s из SAN secret/%s, УЦ — его ca.crt" % (svc, name, secret), file=sys.stderr)
    print(name)


if __name__ == "__main__":
    main()
