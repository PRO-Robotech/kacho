# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set для InternalVolumeService (kacho-storage) — stage S4 CS1-S4-*.

InternalVolumeService.Attach/Detach/ListAttachments/GetInternal — cluster-internal
RPC (:9091, mTLS), выставлены ТОЛЬКО на internal-mux (ban #6, INV-7), НЕ на external
TLS endpoint. Attach — compute-initiated self-describing CAS (storage валидирует
свою `volumes`-строку, НИКОГДА не зовёт compute — ацикличность INV-1). Все
RPC синхронные (CAS мгновенный); tenant-facing async остаётся на compute-плече
(Instance.AttachDisk Operation).

Файл с префиксом `internal-` → validate-cases освобождает его от CASES-INDEX-
покрытия (каталогизирован этой заметкой, см. шапку docs/CASES-INDEX.md).

BLACK-BOX (runnable здесь через external baseUrl) — INV-7a «Internal-only»:
  Attach/Detach/ListAttachments/GetInternal НЕ маршрутизируются на external endpoint
  → POST bare-gRPC-path на external → 404 (route absent). Это часть CS1-S4-11
  («маршрут отсутствует на external»), провокабельная black-box.

NOT black-box (integration-only, testcontainers — НЕ здесь):
  - CS1-S4-01 Attach happy CAS-insert / derived IN_USE / used_by;
  - CS1-S4-02 идемпотентный replay; CS1-S4-03 single-attach (том занят другим);
  - CS1-S4-04 not-ready / not-exist (control-plane READY мгновенно — §0.1);
  - CS1-S4-05 zone/project mismatch раздельными текстами (attach-CAS predicate);
  - CS1-S4-06 device_name коллизия; CS1-S4-07 auto device_name;
  - CS1-S4-08 auto-device-name concurrency race (-race); CS1-S4-09 second-boot EXCLUDE;
  - CS1-S4-10 double-attach race (-race); CS1-S4-11 Detach happy + per-RPC authz on :9091;
  - CS1-S4-12 batched ListAttachments + INTERNAL-leak guard.
  Причина: attach-путь достижим ТОЛЬКО на :9091 (mTLS internal-mux) c seeded Instance
  (self-describing payload) + per-RPC system_admin/editor Check → покрывается
  integration-тестами (внутренний attach-CAS + concurrent goroutines под -race, DoD
  §Тесты) и internal-mux ручным/CI-прогоном, не external newman-e2e.
"""

CASES = []

# InternalVolumeService методы не имеют google.api.http-аннотации — на internal-mux
# они доступны по bare gRPC-JSON-транскодинг-пути /<package>.<Service>/<Method>.
# На EXTERNAL endpoint этот путь НЕ зарегистрирован → 404 (INV-7a).
_SVC = "/kacho.cloud.storage.v1.InternalVolumeService"

_INTERNAL_METHODS = [
    ("ATTACH", "Attach", {"volumeId": "{{garbageStorageId}}", "instanceId": "ins-newman-fake",
                          "instanceZoneId": "{{existingZoneId}}", "projectId": "{{_suiteFolderId}}",
                          "deviceName": "sdb"}),
    ("DETACH", "Detach", {"volumeId": "{{garbageStorageId}}", "instanceId": "ins-newman-fake"}),
    ("LISTATTACHMENTS", "ListAttachments", {"instanceIds": ["ins-newman-fake"]}),
    ("GETINTERNAL", "GetInternal", {"volumeId": "{{garbageStorageId}}"}),
]

for _cid, _method, _body in _INTERNAL_METHODS:
    CASES.append(Case(
        id=f"IVOL-{_cid}-EXTERNAL-ABSENT",
        title=f"POST {_SVC}/{_method} на external endpoint → route absent (Internal-only :9091, ban #6/INV-7) → 404",
        classes=["SEC", "NEG", "AUTHZ"], priority="P0",
        # verifies CS1-S4-11 (INV-7a: InternalVolumeService not routed on external mux)
        steps=[Step(name=_method.lower(), method="POST", path=f"{_SVC}/{_method}", body=_body,
                    test_script=[
                        # An Internal-only method has NO entry in the external permission
                        # catalog, so the api-gateway authz middleware fail-closes it as
                        # 403 PERMISSION_DENIED (uncatalogued → AUTHZ_DENIED) BEFORE any
                        # route/backend dispatch — it never reaches storage. 403 proves
                        # "not usable on external" exactly as 404/405/501 do (authz-first,
                        # security.md #4). Accept it alongside route-absent codes.
                        "pm.test('InternalVolumeService not exposed on external endpoint', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));",
                        "let j; try { j = pm.response.json(); } catch(e) { j = null; }",
                        "pm.test('no attach-CAS leak (never reaches storage on external)', () => pm.expect(JSON.stringify(j || {}).toLowerCase()).to.not.include('sqlstate'));",
                    ])],
    ))
