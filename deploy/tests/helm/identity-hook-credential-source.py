# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# Разбор рендера для identity-hook-credential-source-test.sh.
# Живёт отдельным файлом, а не встроенным текстом: встроенный боролся бы за
# стандартный ввод с данными, которые ему же и подаются.
import sys, re, yaml

prof = sys.argv[1]
docs = [d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict)]

cfg = next((d for d in docs
            if d.get("kind") == "ConfigMap"
            and d["metadata"]["name"].endswith("kratos-config")), None)
dep = next((d for d in docs
            if d.get("kind") == "Deployment"
            and d["metadata"]["name"].endswith("kratos")), None)

if cfg is None or dep is None:
    print("ПРОПУСК|служба личности в этом профиле не развёрнута|0")
    raise SystemExit(0)

body = "".join(cfg.get("data", {}).values())

# Ссылка ищется вместе с ПУТЁМ КЛЮЧА, в котором она стоит. Это не педантизм:
# служба личности переопределяет конфигурацию переменными ПО ПУТИ КЛЮЧА
# (`secrets.cookie` → `SECRETS_COOKIE`), и такая ссылка источник ИМЕЕТ, хотя
# одноимённой переменной нет. А путь, проходящий через ЭЛЕМЕНТ МАССИВА
# (хуки потоков), переменной невыразим — и вот у такой ссылки источником может
# быть только подстановка. Без этого различения проба обвиняла бы исправное:
# первая её редакция назвала находкой ровно те две ссылки, что работают.
def walk(node, path, out):
    if isinstance(node, dict):
        for k, v in node.items():
            walk(v, path + [str(k)], out)
    elif isinstance(node, list):
        for i, v in enumerate(node):
            walk(v, path + ["[]"], out)
    elif isinstance(node, str):
        for name in re.findall(r"\$\{([A-Z0-9_]+)\}", node):
            out.append((name, list(path)))

found = []
for doc in yaml.safe_load_all(body):
    if isinstance(doc, (dict, list)):
        walk(doc, [], found)

refs = sorted({n for n, _ in found})
# Путь выразим переменной, только если в нём нет элемента массива.
expressible = {}
for name, path in found:
    # Массив СКАЛЯРОВ переменной выразим: `secrets.cookie[0]` → `SECRETS_COOKIE`,
    # список из одного значения. Массив ОБЪЕКТОВ — нет: у элемента нет имени, и
    # адресовать `hooks[2].config.auth.config.value` переменной нечем. Отсюда
    # признак: `[]` только ПОСЛЕДНИМ звеном пути — выразимо; `[]` в середине —
    # невыразимо, и источником может быть только подстановка.
    inner = path[:-1] if path and path[-1] == "[]" else path
    var = None if "[]" in inner else "_".join(p.upper() for p in inner)
    prev = expressible.get(name, "unset")
    expressible[name] = var if prev == "unset" or prev == var else None

print("ССЫЛКИ|" + ",".join(refs) + "|" + str(len(refs)))

spec = dep["spec"]["template"]["spec"]
inits = spec.get("initContainers") or []
main = next(c for c in spec["containers"] if c["name"].startswith("kratos"))

# (2) источник каждой ссылки
provided = set()
for c in inits:
    for e in c.get("env") or []:
        provided.add(e["name"])
for e in main.get("env") or []:
    provided.add(e["name"])
missing = []
by_path = []
for r in refs:
    if r in provided:
        continue
    var = expressible.get(r)
    if var and (var in provided or var.removeprefix("KRATOS_") in provided):
        by_path.append(r)          # источник есть — переменная по пути ключа
        continue
    missing.append(r)
if by_path:
    print("ПУТЬ|источник по пути ключа: " + ", ".join(by_path) + "|0")
if missing:
    print("ОТКАЗ|ссылка без источника в поде: " + ", ".join(missing) + "|")

# (3) процесс читает отрендеренное
args = main.get("args") or []
cfgpaths = [args[i + 1] for i, a in enumerate(args)
            if a == "--config" and i + 1 < len(args)]
# Контейнер подстановки — тот, кто ПИШЕТ в файл, который читает процесс:
# записываемый том, чей путь монтирования накрывает путь `--config`. Судить по
# упоминанию пути в тексте команды нельзя: тот же путь стоит у соседнего
# init-контейнера миграций, который ничего не подставляет, и проба обвиняла бы
# невиновного.
def writes_config(c):
    for m in c.get("volumeMounts") or []:
        if m.get("readOnly"):
            continue
        for p in cfgpaths:
            if p.startswith(m["mountPath"].rstrip("/") + "/"):
                return True
    return False

renderers = [c for c in inits if writes_config(c)]
if refs and not renderers:
    print("ОТКАЗ|ни один контейнер подстановки не пишет в файл, который читает процесс: " +
          ", ".join(cfgpaths) + "|")

# (4) отказ закрытый — и он судит ФОРМУ ссылки, а не одно имя.
#
# Здесь стояло `"grep -q" not in script`, то есть проверка по СЛОВУ: она была
# зелена и на шаге, который искал остаток по конкретному имени. Форма, о которой
# такой шаг не знает, не даёт ни красного, ни зелёного — она молчит, и ровно это
# было предметом задачи #1677. Теперь требуется класс символов в самом поиске:
# перечень имён им невыразим by construction.
FORM_CLASS = "[A-Za-z_][A-Za-z0-9_]*"
for c in renderers:
    script = " ".join(c.get("args") or [])
    if ":?" not in script:
        print("ОТКАЗ|подстановка не отвергает ПУСТУЮ либо НЕДОЕХАВШУЮ величину|")
    if "exit 1" not in script:
        print("ОТКАЗ|подстановка не отвергает ОСТАВШУЮСЯ ссылку|")
    elif FORM_CLASS not in script:
        print("ОТКАЗ|подстановка судит остаток по ИМЕНИ, а не по ФОРМЕ ссылки: "
              "ссылка иной формы уедет неподставленной, и шаг смолчит|")

# (5) положительный контроль: том отрендеренного смонтирован главному
for c in renderers:
    outs = {m["name"] for m in (c.get("volumeMounts") or []) if not m.get("readOnly")}
    seen = {m["name"] for m in (main.get("volumeMounts") or [])}
    if not (outs & seen):
        print("ОТКАЗ|том отрендеренного НЕ смонтирован главному контейнеру — "
              "путь --config указывает в пустоту|")
