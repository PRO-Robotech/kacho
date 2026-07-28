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
  → POST bare-gRPC-path на external НЕ обслуживается. Это часть CS1-S4-11 («маршрут
  отсутствует на external»), провокабельная black-box. Что именно наблюдаемо — см.
  _assert_403_means_uncatalogued ниже: authz-слой края стоит перед маршрутизацией,
  поэтому «нет маршрута» и «нет прав» приходят одним кодом и обязаны различаться по
  типу нарушения, а не приниматься оба на веру.

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

_INTERNAL_METHODS = ["Attach", "Detach", "ListAttachments", "GetInternal"]


def _assert_403_means_uncatalogued():
    """Различитель: отказ по правам ≠ отсутствие маршрута.

    Эти пробы бьют в путь, которого на публичном крае нет НАМЕРЕННО (ban #6). Но
    authz-слой края стоит ПЕРЕД маршрутизацией, и на пару (метод, путь), которой нет
    в каталоге прав, он fail-closed отвечает 403 — тем же кодом, каким отвечает на
    ЖИВОЙ маршрут, куда у вызывающего нет доступа. Прежняя формулировка кейса прямо
    заявляла, что «403 доказывает то же, что 404» — не доказывает: кейс, довольный
    любым не-200, остался бы зелёным ровно в том регрессе, который он сторожит —
    когда Internal*-RPC засветился на внешнем крае, а конкретный актор просто не имел
    на него прав.

    Наблюдаемое различие: отказ по КАТАЛОГУ несёт нарушение типа `authz.catalog`
    (левый токен причины «catalog: no entry for method»); решение о правах на
    конкретный объект несёт `authz.no_path` / тип по отношению. Требуем первое.
    Коды 404/405/501 сами по себе означают «не обслуживается» — там различать нечего.
    """
    return [
        "let _d; try { _d = pm.response.json(); } catch (e) { _d = null; }",
        "pm.test('403 here must be an UNCATALOGUED-method denial, not a permission check on a live route', () => {",
        "  if (pm.response.code !== 403) return;",
        "  const types = [];",
        "  ((_d && _d.details) || []).forEach(d => ((d && d.violations) || []).forEach(v => types.push(v && v.type)));",
        "  pm.expect(types, JSON.stringify(_d)).to.include('authz.catalog');",
        "});",
    ]


for _method in _INTERNAL_METHODS:
    CASES.append(Case(
        id=f"IVOL-{_method.upper()}-EXTERNAL-ABSENT",
        title=f"POST {_SVC}/{_method} на external endpoint → not served there (Internal-only :9091, ban #6/INV-7)",
        classes=["SEC", "NEG", "AUTHZ"], priority="P0",
        # verifies CS1-S4-11 (INV-7a: InternalVolumeService not routed on external mux)
        #
        # Тела нет и быть не должно: предмет пробы — что этой пары (метод, путь) на
        # публичном крае НЕТ. Контракта запроса не существует, разбирать нечего;
        # заполненный attach-payload лишь создавал бы впечатление, что край его
        # взвесил, — он не доходит ни до одного разборщика.
        steps=[Step(name=_method.lower(), method="POST", path=f"{_SVC}/{_method}",
                    test_script=[
                        "pm.test('InternalVolumeService not exposed on external endpoint', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));",
                        *_assert_403_means_uncatalogued(),
                        "pm.test('no attach-CAS leak (never reaches storage on external)', () => pm.expect(JSON.stringify(_d || {}).toLowerCase()).to.not.include('sqlstate'));",
                    ])],
    ))
