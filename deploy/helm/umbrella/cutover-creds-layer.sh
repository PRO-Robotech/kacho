# shellcheck shell=bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cutover-creds-layer.sh — ПЕРЕЧЕНЬ КООРДИНАТ СЛОЯ УЧЁТНЫХ ДАННЫХ ПЛОЩАДКИ И ЕГО
# ПРОВЕРКА (NTF-1, полоса D2; замысел З28 «Слой учётных данных fe3455», CX1-85, N10).
#
# Только библиотека: подключается `source` скриптом раскатки
# (cutover-fe3455.sh, шаг 1a) и пробой раскатки; исполнение по пути отказывает
# кодом 2. Проверка одна — функция `creds_layer_stray` ниже; второго её
# написания нет ни в скрипте раскатки, ни в пробе.
#
# ── ЗАЧЕМ ПРОВЕРКА ─────────────────────────────────────────────────────────────
#
# Слой учётных данных площадки (`values.fe3455-secrets.yaml`) — вне git: его не
# видят ни ревью, ни гейты. Пока в этом файле жил весь накладной слой поставщика
# личности, ПОСАДКА поставщиков была невидима для дерева, и «ноль находок» гейтов
# по ней значил «ноль прочитанного». Соглашение само такого раздела не держит:
# проще всего поменять живой кластер правкой файла, которого никто не видит.
# Поэтому раздел ПРОВЕРЯЕТСЯ: слой вправе объявлять только координаты перечня, а
# ключ посадки, вернувшийся в него, отказывает раскатке, а не уезжает молча.
#
# Перечень — перечень ЛИСТОВЫХ путей, а не поддеревьев: разрешённое поддерево
# целиком впустило бы назад каждый ключ посадки под ним.
#
# ── ПОЧЕМУ ПЕРЕЧЕНЬ ПУСТ ───────────────────────────────────────────────────────
#
# Последней координатой перечня был адрес нашего ретранслятора почты
# (`global.kacho.identity.smtp.connectionURI`). Узел почты площадки fe3455 —
# теперь приёмник в кластере, объявленный отслеживаемым профилем
# values.fe3455.yaml целиком (адрес, поля отправителя, якорь — решение Д46). У
# адреса одного узла было бы два источника с разным старшинством: слой,
# наложенный последним, заменил бы адрес профиля, а якорь профиля (секрет
# приёмника) остался бы. Такое сочетание страж зонтика отвергает, поэтому
# координата снята тем же изменением, что вводит узел в профиль.
#
# У слоя разрешённых координат не осталось. Файл слоя при этом обязателен (его
# имя покрыто шаблоном игнорирования), пустой слой `{}` законен. Слой, который
# ещё несёт адрес почты, отвергается ДО рендера с именем координаты — копия
# слоя у оператора площадки правится удалением строки. Настоящая учётная
# координата, если появится, вписывается в перечень с доводом рядом.

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo "FATAL: cutover-creds-layer.sh — библиотека, подключайте source" >&2
  exit 2
fi

# CRED_PATHS — разрешённые листовые координаты слоя учётных данных, элемент на
# строку. Присваивание массива перечня в файле ровно одно: гейт MAIL-54
# (deploy/identity_mail_lane_single_declaration_test.go) находит его по
# ограничителю и судит каждую строку.
CRED_PATHS=(
)

# creds_layer_stray <файл слоя> — печатает координаты слоя вне CRED_PATHS, по
# одной на строку; пустой вывод — слой несёт только разрешённое. Код 2 — слой не
# прочитан (нет python3 с PyYAML либо файл не YAML): это «не выполнилось», а не
# «посторонних координат нет».
creds_layer_stray() {
  local layer="${1:-}"
  [ -f "$layer" ] || { echo "FATAL: creds_layer_stray — файла слоя \`$layer\` нет" >&2; return 2; }
  CRED_PATHS="${CRED_PATHS[*]}" python3 - "$layer" <<'PY' || return 2
import os, sys, yaml
allowed = set(os.environ["CRED_PATHS"].split())
tree = yaml.safe_load(open(sys.argv[1])) or {}
def leaves(node, path=()):
    if isinstance(node, dict):
        for k, v in node.items():
            yield from leaves(v, path + (str(k),))
    else:
        yield ".".join(path)
print("\n".join(sorted(p for p in leaves(tree) if p not in allowed)))
PY
}

# creds_layer_refusal <файл слоя> <координаты> — текст отказа раскатки: какие
# координаты слоя вне перечня и где перечень.
creds_layer_refusal() {
  local layer="$1" stray="$2"
  echo "$layer declares coordinates that are NOT on the credentials list (CRED_PATHS in deploy/helm/umbrella/cutover-creds-layer.sh):" >&2
  while IFS= read -r c; do [ -n "$c" ] && printf '  %s\n' "$c" >&2; done <<<"$stray"
  echo "posture must live in a TRACKED profile of deploy/stacks.txt, where review and the gates can see it;
the mail relay address of fe3455 is the in-cluster receiver declared by values.fe3455.yaml and is deleted
from the layer. If a coordinate above really is a credential, add it to CRED_PATHS in
deploy/helm/umbrella/cutover-creds-layer.sh with a reason. Refusing to deploy a layer that no gate has read." >&2
}
