#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# kube-api-fixture.py — НАСТОЯЩИЙ ПУТЬ `lookup` helm БЕЗ КЛАСТЕРА.
#
# ПРЕДМЕТ. Шаблон, читающий кластер (`lookup`), под `helm template` без
# `--dry-run=server` получает ПУСТОЙ словарь: рендер офлайн проверяет только
# ветку «кластер не прочитан». Судить ветку «прочитан» можно было бы лишь
# поднятым кластером — а офлайновый каталог `tests/helm/` кластера не имеет
# by construction. Поэтому здесь отвечает узел, который говорит helm ровно
# то, что сказал бы API-сервер: обнаружение (`/version`, `/api`, `/apis`,
# `/api/v1`) и список ConfigMap пространства имён ВМЕСТЕ с ключом `items`.
#
# ЧЕМ ЭТО НЕ СНИСХОДИТЕЛЬНЕЕ ПРОДУКТА. Сам `lookup` не подменяется: helm
# ходит своим клиентом, разбирает ответ своим разбором и отдаёт шаблону то же
# `UnstructuredContent()`, что отдал бы против кластера. Подменён только ИСТОЧНИК
# объектов — файлом, который пишет проба. Всё, чего узел не знает, — 404, как у
# сервера, у которого такого объекта нет (так helm спрашивает о существовании
# отрендеренных объектов перед server-side dry-run).
#
# ФОРМА
#   kube-api-fixture.py <файл-объектов.json> <файл-порта>
# Файл объектов — {"<ns>": [<ConfigMap>, …]}. Узел слушает 127.0.0.1 на порту,
# выбранном ядром, и пишет номер в <файл-порта> ПОСЛЕ того, как начал слушать:
# читатель, дождавшийся файла, не опередит сокет. Файл объектов перечитывается на
# КАЖДОМ запросе — проба меняет содержимое между рендерами без перезапуска узла.
import http.server
import json
import os
import sys

# Обнаружение называет каждый вид, который чарт службы рендерит: server-side
# dry-run сопоставляет КАЖДЫЙ объект релиза с ресурсом сервера и отказывает на
# неизвестном виде («no matches for kind»). Вид, которого здесь нет, — не
# находка о чарте, а несозданное условие пробы.
def _res(name, kind, namespaced=True):
    return {"name": name, "singularName": name[:-1], "namespaced": namespaced,
            "kind": kind, "verbs": ["get", "list"]}


GROUPS = {
    "apps/v1": [_res("deployments", "Deployment")],
    "rbac.authorization.k8s.io/v1": [_res("roles", "Role"), _res("rolebindings", "RoleBinding")],
    "cert-manager.io/v1": [_res("certificates", "Certificate")],
}

DISCOVERY = {
    "/version": {"major": "1", "minor": "31", "gitVersion": "v1.31.0"},
    "/api": {"kind": "APIVersions", "versions": ["v1"], "serverAddressByClientCIDRs": []},
    "/apis": {"kind": "APIGroupList", "apiVersion": "v1", "groups": [
        {"name": gv.split("/")[0],
         "versions": [{"groupVersion": gv, "version": gv.split("/")[1]}],
         "preferredVersion": {"groupVersion": gv, "version": gv.split("/")[1]}}
        for gv in GROUPS]},
    "/api/v1": {"kind": "APIResourceList", "groupVersion": "v1", "resources": [
        _res("configmaps", "ConfigMap"), _res("secrets", "Secret"),
        _res("services", "Service"), _res("serviceaccounts", "ServiceAccount"),
    ]},
}
for _gv, _rs in GROUPS.items():
    DISCOVERY["/apis/" + _gv] = {"kind": "APIResourceList", "groupVersion": _gv, "resources": _rs}

NOT_FOUND = {"kind": "Status", "apiVersion": "v1", "status": "Failure",
             "reason": "NotFound", "code": 404}


def answer(objects_path, path):
    if path in DISCOVERY:
        return DISCOVERY[path]
    parts = path.strip("/").split("/")
    # /api/v1/namespaces/<ns>/configmaps[/<name>]
    if len(parts) in (5, 6) and parts[:3] == ["api", "v1", "namespaces"] and parts[4] == "configmaps":
        with open(objects_path, encoding="utf-8") as fh:
            items = json.load(fh).get(parts[3], [])
        if len(parts) == 5:
            return {"kind": "ConfigMapList", "apiVersion": "v1", "metadata": {}, "items": items}
        for it in items:
            if it.get("metadata", {}).get("name") == parts[5]:
                return it
    return None


def main():
    objects_path, port_path = sys.argv[1], sys.argv[2]

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            body = answer(objects_path, self.path.split("?", 1)[0])
            self.send_response(200 if body is not None else 404)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(body if body is not None else NOT_FOUND).encode())

        def log_message(self, *args):
            pass

    srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    tmp = port_path + ".part"
    with open(tmp, "w", encoding="utf-8") as fh:
        fh.write(str(srv.server_address[1]))
    os.replace(tmp, port_path)
    srv.serve_forever()


if __name__ == "__main__":
    main()
