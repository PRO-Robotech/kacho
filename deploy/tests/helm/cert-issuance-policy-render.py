#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Разбор рендера одной цепочки для cert-issuance-policy-render-test.sh.

Вход — рендер зонтика в stdin; аргументы: <цепочка> <учётка контроллера
cert-manager> <пространство релиза>. Выход — строки `ВИД|текст|доп`:
ENABLED / DISABLED|<почему> / CERT|<имя> / OK|<утверждение> / BAD|<находка>.
Код ≠ 0 — разбор не состоялся (условие, а не находка).
"""
import re
import sys

import yaml

stack, cm_sa, release_ns = sys.argv[1], sys.argv[2], sys.argv[3]
docs = [d for d in yaml.safe_load_all(sys.stdin.read()) if isinstance(d, dict)]

issuers = [d for d in docs if d.get("kind") == "ClusterIssuer" and "ca" in (d.get("spec") or {})]
policies = [d for d in docs if d.get("kind") == "CertificateRequestPolicy"]
bindings = [d for d in docs if d.get("kind") == "ClusterRoleBinding"
            and (d.get("metadata", {}).get("labels") or {}).get("kacho.cloud/component") == "cert-issuance-policy"]
out = []

if not issuers:
    # Цепочка без внутреннего УЦ (mTLS выключен) — политике судить нечего.
    if policies:
        out.append("BAD|политика рендерится, а внутреннего УЦ в цепочке нет|")
    else:
        out.append("DISABLED|внутреннего УЦ в цепочке нет (mTLS выключен)|")
    print("\n".join(out))
    sys.exit(0)
if len(issuers) != 1:
    out.append("BAD|внутренних УЦ %d — какой из них под политикой, не установить|" % len(issuers))
    print("\n".join(out))
    sys.exit(0)
issuer = issuers[0]["metadata"]["name"]
ca_root = [d for d in docs if d.get("kind") == "Certificate"
           and (d.get("spec") or {}).get("isCA")]
ca_ns = ca_root[0]["metadata"].get("namespace", release_ns) if ca_root else release_ns

if not policies:
    if ca_ns == release_ns:
        out.append("BAD|политика выпуска выключена, а cert-manager СВОЙ (УЦ в пространстве релиза %s) — "
                   "стенд остался бы без политики молча|" % release_ns)
    else:
        out.append("DISABLED|УЦ в пространстве %s|" % ca_ns)
    print("\n".join(out))
    sys.exit(0)

out.append("ENABLED||")
arms = {}
for p in policies:
    arm = (p["metadata"].get("labels") or {}).get("kacho.cloud/issuance-arm", "")
    ref = ((p.get("spec") or {}).get("selector") or {}).get("issuerRef") or {}
    if ref.get("name") != issuer or ref.get("kind") != "ClusterIssuer":
        out.append("BAD|ветвь «%s» смотрит на %s/%s, а внутренний УЦ цепочки — ClusterIssuer/%s|"
                   % (arm, ref.get("kind"), ref.get("name"), issuer))
        continue
    arms[arm] = p
for arm in ("workload", "certificates"):
    if arm in arms:
        out.append("OK|ветвь «%s» есть и судит ClusterIssuer/%s|" % (arm, issuer))
    else:
        out.append("BAD|ветви «%s» для ClusterIssuer/%s нет|" % (arm, issuer))

ct = arms.get("certificates")
if ct is not None:
    name = ct["metadata"]["name"]
    subjects = [s for b in bindings
                if (b.get("roleRef") or {}).get("name") == name + ":use"
                for s in (b.get("subjects") or [])]
    want = [{"kind": "ServiceAccount", "name": cm_sa, "namespace": ca_ns}]
    got = [{"kind": s.get("kind"), "name": s.get("name"), "namespace": s.get("namespace")} for s in subjects]
    if got == want:
        out.append("OK|ветвь «certificates» открыта ровно учётке контроллера %s/%s|" % (ca_ns, cm_sa))
    else:
        out.append("BAD|ветвь «certificates» открыта %r, ждали ровно %r (релиз CERT_MANAGER_RELEASE)|"
                   % (got, want))

LABEL = r"[a-z0-9]([-a-z0-9]*[a-z0-9])?"
for c in docs:
    if c.get("kind") != "Certificate":
        continue
    spec = c.get("spec") or {}
    if (spec.get("issuerRef") or {}).get("name") != issuer:
        continue
    cname = c["metadata"]["name"]
    ns = c["metadata"].get("namespace", release_ns)
    out.append("CERT|%s|" % cname)
    bad = []
    for u in spec.get("uris") or []:
        m = re.fullmatch(r"spiffe://[^/]+/ns/(%s)/sa/(%s)" % (LABEL, LABEL), u)
        if not m:
            bad.append("URI «%s» не в форме spiffe://<домен>/ns/<ns>/sa/<учётка>" % u)
        elif m.group(1) != ns:
            bad.append("URI «%s» называет пространство «%s», а сертификат выпускается в «%s»"
                       % (u, m.group(1), ns))
    for d in spec.get("dnsNames") or []:
        if "." in d and not (d.endswith("." + ns) or d.endswith("." + ns + ".svc")
                             or d.endswith("." + ns + ".svc.cluster.local")):
            bad.append("DNS «%s» не принадлежит пространству «%s»" % (d, ns))
    if spec.get("commonName"):
        bad.append("общее имя «%s» — ветвь его не допускает" % spec["commonName"])
    for f in ("ipAddresses", "emailAddresses", "otherNames"):
        if spec.get(f):
            bad.append("%s — ветвь их не допускает" % f)
    if spec.get("isCA"):
        bad.append("isCA — листовой ветви не допускается")
    if bad:
        out.append("BAD|сертификат %s/%s политике НЕ выпускаем: %s|" % (ns, cname, "; ".join(bad)))
print("\n".join(out))
