#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# assert-production-posture — доказать, что стенд РЕАЛЬНО РАБОТАЕТ в
# production-security-posture.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ЭТОТ ГЕЙТ ПЕРЕПИСАН (инцидент 2026-07-25, kacho-storage)
#
# Первый в истории прогон `make dev-prod-up` завершился нулём и напечатал
# «production-posture stand is UP and READY». Это была ЛОЖЬ: 7 сервисов из 8
# действительно переключились, а kacho-storage продолжал работать в dev-режиме с
# ПЛЕЙНТЕКСТОВЫМ соединением к своей БД. Оба гейта пропустили это, и по РАЗНЫМ
# причинам — поэтому никто ничего не заметил:
#
#   * assert-rollout-ready видел здоровый под. Под не перезапускался, значит и не
#     падал. «Ready» не доказывает НИЧЕГО про посадку — это утверждение о
#     живости, а не о безопасности.
#
#   * assert-production-posture (старая версия) читал СОХРАНЁННЫЕ НАСТРОЙКИ —
#     `.spec.containers[].env[].value` подов и `mode: dev` в ConfigMap'ах. То
#     есть проверял НАМЕРЕНИЕ, а не ИСХОД. У storage настройки читались
#     `production`/`require`, а живой процесс работал `dev`/`disable`: все
#     security-ручки приезжают в контейнер через `envFrom: configMapRef` и
#     читаются ОДИН РАЗ, на старте процесса. Правка ConfigMap не меняет
#     pod-template → под не перекатывается → процесс живёт со СТАРЫМ окружением.
#     Boot-guard (`Config.Validate` → refuse-to-start) не сработал: он не может
#     отказать в старте, которого не было.
#
#   * вдобавок старая выборка обходила только ИНДИВИДУАЛЬНО объявленные
#     переменные (`env[].value`), поэтому массовый импорт (`envFrom`) был ей не
#     виден ВООБЩЕ — storage был слепым пятном в обеих половинах гейта.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРИНЦИП НОВОГО ГЕЙТА: проверяем ИСХОД, а не НАМЕРЕНИЕ.
#
# Три НЕЗАВИСИМЫХ проверки. Первые две смотрят на живой процесс и на внешнего
# свидетеля — они устойчивы к тому, что под НЕ ПЕРЕЗАПУСКАЛСЯ (именно этот случай
# и утёк). Третья осталась проверкой намерения, но у неё закрыто слепое пятно —
# она видит значения независимо от способа доставки.
#
#   A. ЖИВОЙ ПРОЦЕСС САМ СООБЩАЕТ СВОЮ ПОСАДКУ.
#      Каждый сервис на старте пишет одну структурную строку
#      `msg="boot security posture"` со значениями, которые процесс РЕАЛЬНО
#      принял (после boot-guard'а, из разобранного конфига — не из перечитанного
#      окружения). Гейт читает её из лога ТЕКУЩЕГО экземпляра контейнера (без
#      `--previous`). Под, который не перекатился, отчитается своей СТАРОЙ
#      посадкой — и гейт покраснеет. Это наблюдаемый факт, а не запись в
#      ConfigMap.
#      ОТСУТСТВИЕ СТРОКИ — ТОЖЕ КРАСНОЕ (fail-closed): иначе сервис уклонялся бы
#      от гейта, просто не логируя посадку.
#
#   B. ШИФРОВАНИЕ СОЕДИНЕНИЙ ПОДТВЕРЖДАЕТ САМА БАЗА (`pg_stat_ssl`).
#      Независимый свидетель: не сервис рассказывает о себе, а СУБД сообщает,
#      по каким соединениям с ней реально говорят. Подделать нельзя — сервис не
#      участвует в ответе. Ровно этот сигнал уличал storage: его бэкенды были
#      плейнтекстовыми, пока его настройки читались `require`.
#      Правило: КАЖДОЕ TCP-соединение клиента обязано быть SSL. Локальные
#      unix-socket соединения суперпользователя (`client_addr IS NULL`) —
#      законное исключение: на них `sslmode` клиента не распространяется, это
#      служебные health/admin-подключения самого чарта.
#
#   C. СОХРАНЁННЫЕ НАСТРОЙКИ — ПО ВСЕМ ПУТЯМ ДОСТАВКИ (закрытое слепое пятно).
#      Backstop-проверка намерения: ловит неприменившийся values-профиль ещё до
#      того, как поды перекатятся. В отличие от старой версии обходит ВСЕ пути
#      доставки: инлайн `env[].value`, `env[].valueFrom.configMapKeyRef`,
#      массовый импорт `envFrom[].configMapRef` (то самое слепое пятно) и
#      конфиг-ФАЙЛЫ в ConfigMap'ах (vpc/nlb/iam отдают режим через config.yaml).
#
# ЧТО ЭТОТ ГЕЙТ ДОКАЗЫВАЕТ: каждый сервис стенда СЕЙЧАС ИСПОЛНЯЕТСЯ в
# production-посадке (authMode=production*, DB под TLS, mTLS на обоих gRPC-
# листенерах, per-RPC authz-Check подключён), и это подтверждено независимо от
# самого сервиса.
# ЧЕГО НЕ ДОКАЗЫВАЕТ: функциональной корректности authz/токенов — это e2e-newman.
#
# Структурную (chart-level) причину класса — ConfigMap, потребляемый подом без
# привязки pod-template к его содержимому — ловит офлайновый
# tests/helm/config-rollout-binding-test.sh, ДО деплоя.
#
# ОБЛАСТЬ ИСПОЛНЕНИЯ: почему у гейта ДВА профиля, а не один.
#
# Гейт целиком описывает БОЕВУЮ посадку и потому исполнялся только под боевым
# профилем (`make dev-prod-up`). Одно из его измерений от посадки НЕ ЗАВИСИТ, и
# ровно поэтому провалилось незамеченным: круг отправителей, которым разрешено
# ГОВОРИТЬ ЗА пользователя. Пустой круг — это не «менее строгая настройка», это
# «принимаем переданную личность от ЛЮБОГО пира с сертификатом внутреннего CA»,
# и на dev-стенде mTLS включён, то есть такие пиры там есть. auth_mode dev не
# делает это допустимым — он лишь выключает стражу старта, которая на боевом
# профиле не дала бы поду подняться. Итог: dev-профиль был единственным местом,
# где несужённый круг мог жить, и единственным местом, куда гейт не приходил.
#
#   POSTURE_PROFILE=production (умолчание) — все три проверки, как раньше.
#   POSTURE_PROFILE=dev — ТОЛЬКО измерение круга отправителей (A), потому что
#     dev-профиль осознанно расслабляет auth_mode / db_sslmode, и требовать их
#     здесь значило бы красить гейт на каждом dev-стенде до полной бесполезности.
#
# Умолчание — боевое, и боевая цель Makefile пинит `POSTURE_PROFILE=production`
# явно: иначе `POSTURE_PROFILE=dev make assert-production-posture` сузил бы
# боевой гейт до одного измерения и увёл бы его в зелёное. Профиль выбирает
# ЦЕЛЬ, а не тот, кто её зовёт.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

NS="${NS:-kacho}"

# Экспортируется: вердикт секции A читает профиль через $ENV внутри jq-программы
# (см. там же, почему не вторым --argjson).
export POSTURE_PROFILE="${POSTURE_PROFILE:-production}"
case "$POSTURE_PROFILE" in
  production|dev) ;;
  *) echo "!!! POSTURE_PROFILE='$POSTURE_PROFILE' — допустимо production|dev"; exit 2 ;;
esac

# Ожидаемый состав стенда. Список ЖЁСТКИЙ намеренно: если бы гейт просто обходил
# «те сервисы, что нашлись», то не развернувшийся сервис делал бы гейт зелёным
# молчанием. Стенд без какого-то сервиса — это осознанное решение оператора,
# поэтому исключение делается ЯВНО через POSTURE_SKIP="registry nlb", а не
# отсутствием в кластере.
#
# deployment|service|postgres statefulset (пусто = сервис без своей БД)
SERVICES="
api-gateway|api-gateway|
compute|compute|kacho-umbrella-pg-compute
kacho-geo|geo|kacho-umbrella-pg-geo
kaname|iam|kacho-umbrella-pg-iam
kacho-nlb|nlb|kacho-umbrella-pg-nlb
kacho-storage|storage|kacho-umbrella-pg-storage
registry|registry|kacho-umbrella-pg-registry
vpc|vpc|kacho-umbrella-pg-vpc
"
POSTURE_SKIP="${POSTURE_SKIP:-}"

# Сервисы, у которых ЕСТЬ измерение «круг отправителей чужой личности» — то есть
# те, что реально зовут grpcsrv.WithTrustedForwarders и потому могут его сузить.
# Только для них поле trusted_forwarders градуируется как требование.
#
# Почему список, а не «требовать со всех»: false в самоотчёте покрывает ДВА разных
# случая (см. pkg/observability/bootposture.go). У api-gateway измерения нет вовсе —
# он личность НЕ принимает, а чеканит из проверенного токена; требовать с него
# сужения значило бы красить сервис за отсутствие механизма, которого у него и не
# должно быть. Сервис, который переданную личность ПРИНИМАЕТ, обязан быть в списке —
# иначе гейт не видит измерения вовсе и молчит ровно там, где надо кричать. Пустой
# список отключает проверку целиком.
#
# registry добавлен: у него появилась ручка
# (KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS), стража старта
# (Config.Validate → grpcsrv.TrustedForwarders.Require — старт не проходит на
# несуженном круге) и
# доверенная пара на ОБОИХ листенерах. До этого публичный листенер вообще читал
# заголовки личности без проверки транспорта.
#
# iam добавлен: ручка authn.trusted-forwarder-sans (env
# KANAME_AUTHN__TRUSTED_FORWARDER_SANS), стража старта
# (validateProductionTrustedForwarders) и общий сборщик цепочки на ОБА листенера.
# У iam цена этого измерения выше, чем у соседей: на :9090 он СОЗНАТЕЛЬНО не
# перепроверяет права конечного пользователя, поэтому не сужённый круг означал
# полномочия жертвы на всей тенантской поверхности.
# vpc добавлен (последний из принимающих личность): ручка
# authz.trusted-forwarder-sans (env KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS), стража
# старта (config.Validate — боевой режим не стартует на пустом списке, считая записи,
# которые примет транспорт, а не длину сырой строки) и доверенная пара на ОБОИХ
# листенерах. До этого оба листенера читали заголовки личности без всякой проверки
# отправителя, а его публичный :9090 открыт всему пространству имён — то есть
# измерение отсутствовало ровно там, где вся тенантская поверхность. Круг сужен по
# ФАКТИЧЕСКИМ отправителям (шлюз, compute, nlb, оператор), найденным по графу
# вызовов: сужение до одного шлюза сломало бы привязку интерфейса и резолв подсети.
FORWARDER_NARROWING_REQUIRED="${FORWARDER_NARROWING_REQUIRED:-geo compute nlb storage registry iam vpc}"

# needs_forwarder_narrowing <svc> — участвует ли сервис в измерении.
needs_forwarder_narrowing() {
  local svc="$1" s
  for s in $FORWARDER_NARROWING_REQUIRED; do [ "$s" = "$svc" ] && return 0; done
  return 1
}

FAILED=0
fail() { echo "  ✗ $*"; FAILED=1; }
ok()   { echo "  ✓ $*"; }

skipped() {
  local svc="$1" s
  for s in $POSTURE_SKIP; do [ "$s" = "$svc" ] && return 0; done
  return 1
}

# ─────────────────────────────────────────────────────────────────────────────
# A. ЖИВОЙ ПРОЦЕСС: посадка, с которой сервис РЕАЛЬНО стартовал.
# ─────────────────────────────────────────────────────────────────────────────
if [ "$POSTURE_PROFILE" = dev ]; then
  echo "=== A. живой процесс: круг отправителей чужой личности (профиль dev) ==="
  echo "    Градуируется ОДНО измерение — trusted_forwarders. Остальные (auth_mode,"
  echo "    db_sslmode, mTLS) dev-профиль расслабляет осознанно; круг — не расслабляет"
  echo "    никто: пустой круг принимает переданную личность от любого пира с"
  echo "    сертификатом внутреннего CA, а mTLS на dev-стенде включён."
else
  echo "=== A. живой процесс: boot security posture (лог текущего контейнера) ==="
fi

# boot_line <pod> — строка «boot security posture» из лога ТЕКУЩЕГО экземпляра
# контейнера. Без `--previous`: нас интересует процесс, исполняющийся ПРЯМО
# СЕЙЧАС, а не тот, что когда-то падал.
#
# --limit-bytes отдаёт НАЧАЛО лога (а не хвост) и ограничивает выкачку: строка
# пишется в composition root, то есть в первых килобайтах. 256 KiB — заведомый
# запас; если сервис успевает налить больше ДО отчёта о посадке, это само по
# себе повод разобраться, а не молча расширять лимит.
BOOT_LOG_HEAD_BYTES="${BOOT_LOG_HEAD_BYTES:-262144}"
boot_line() {
  local pod="$1" c line
  for c in $(kubectl -n "$NS" get pod "$pod" -o jsonpath='{.spec.containers[*].name}' 2>/dev/null); do
    line="$(kubectl -n "$NS" logs "$pod" -c "$c" --limit-bytes="$BOOT_LOG_HEAD_BYTES" 2>/dev/null \
            | jq -Rc 'fromjson? | select(.msg=="boot security posture")' 2>/dev/null | tail -1)"
    if [ -n "$line" ]; then printf '%s' "$line"; return 0; fi
  done
  return 1
}

for row in $SERVICES; do
  dep="${row%%|*}"; rest="${row#*|}"; svc="${rest%%|*}"
  skipped "$svc" && { echo "  — $svc: ЯВНО исключён (POSTURE_SKIP)"; continue; }

  if ! kubectl -n "$NS" get deploy "$dep" >/dev/null 2>&1; then
    fail "$svc: deployment/$dep НЕ РАЗВЁРНУТ (исключить осознанно — POSTURE_SKIP=\"$svc\")"
    continue
  fi

  pods="$(kubectl -n "$NS" get pods -o json 2>/dev/null \
          | jq -r --arg d "$dep" '.items[] | select(.metadata.ownerReferences[]?.kind=="ReplicaSet")
                                 | select(.metadata.name | startswith($d + "-"))
                                 | select(.metadata.deletionTimestamp == null) | .metadata.name')"
  [ -z "$pods" ] && { fail "$svc: нет ни одного пода"; continue; }

  # ВСЕ реплики, а не первая попавшаяся: наполовину перекатившийся Deployment
  # (одна реплика в production, другая ещё в dev) обязан быть красным.
  for p in $pods; do
    if ! line="$(boot_line "$p")"; then
      fail "$svc/$p: НЕТ строки «boot security posture» в логе текущего контейнера."
      echo "      Гейт fail-closed: без самоотчёта живого процесса посадка НЕ ДОКАЗАНА."
      echo "      Причины: сервис её не пишет (добавить в composition root после Validate),"
      echo "      либо под живёт так долго, что строка вытеснена ротацией (перекатить деплой)."
      continue
    fi

    # need_fwd передаётся в jq, чтобы требование по кругу отправителей
    # применялось только к сервисам, у которых это измерение есть.
    if needs_forwarder_narrowing "$svc"; then need_fwd=true; else need_fwd=false; fi

    # `shown` вместо `//`: оператор `//` считает отсутствующим И `null`, И `false`,
    # поэтому измерение, о котором сервис ЧЕСТНО доложил `false`, печаталось как
    # «<нет>» — то есть текст отказа говорил «сервис ничего не сообщил» ровно там,
    # где сервис сообщил «выключено». Различие не косметическое: «не доложил»
    # чинится в composition root сервиса (строка самоотчёта неполна), «доложил
    # false» — в настройках стенда. Оператор гейта шёл не туда. Ключ проверяем
    # через has(), поэтому false печатается как false, а «<нет>» остаётся ровно
    # за отсутствием ключа.
    # Профиль читается ВНУТРИ программы, через $ENV, а не вторым --argjson.
    # Причина не косметическая: соседние проверки (tests/helm/{iam,registry}-
    # trusted-forwarder-test.sh) ВЫНИМАЮТ эту программу из файла — чтобы копия не
    # разъехалась с оригиналом — и вызывают её, связывая ровно один параметр.
    # Второй --argjson оставил бы их с несвязанной переменной, то есть сломал бы
    # ровно те проверки, которые следят за этим вердиктом. Переменной в окружении
    # у них нет → `// "production"` → градуируются все измерения, как они и ждут.
    #
    # Расслабляемые dev'ом поля выключаются ЦЕЛИКОМ, а не «мягко»: полу-
    # градуированный гейт печатал бы отказ на каждом dev-стенде, и его перестали
    # бы читать.
    verdict="$(printf '%s' "$line" | jq -r --argjson need_fwd "$need_fwd" '
      def shown($k): if has($k) then (.[$k] | tostring) else "<нет>" end;
      (($ENV.POSTURE_PROFILE // "production") != "dev") as $all_dims |
      [ (if ($all_dims | not) or ((.auth_mode // "") | test("^production(-strict)?$")) then empty
         else "auth_mode=\(shown("auth_mode"))" end),
        (if ($all_dims | not) or ((.db_sslmode // "") | test("^(require|verify-ca|verify-full|n/a)$")) then empty
         else "db_sslmode=\(shown("db_sslmode"))" end),
        (if ($all_dims | not) or (.public_mtls   == true) then empty else "public_mtls=\(shown("public_mtls"))"   end),
        # ВНУТРЕННИЙ ЛИСТЕНЕР — ТРИ СОСТОЯНИЯ, А НЕ ДВА (задача #1024).
        #
        # Измерение стало СТРОКОВЫМ, как соседние db_sslmode и identity_provider,
        # потому что «листенера нет вовсе» обязано быть отличимо от «листенер есть
        # и не защищён». Схлопнуть их значило бы разрешить незащищённый листенер
        # молчанием — снять контроль, а не сузить его предмет.
        #
        #   "true" — листенер есть и проверяет клиентские сертификаты → проход;
        #   "n/a"  — листенера НЕТ НИ ОДНОГО: входной поверхности не существует,
        #            защищать нечего (край с задачи #1024) → проход;
        #   "false"— листенер есть и работает без mTLS → ОТКАЗ, как и прежде;
        #   ""     — величина не из трёх объявленных (нулевое значение структуры,
        #            то есть процесс поле не заполнил) → ОТКАЗ;
        #   ключа нет → ОТКАЗ: так выглядит СТАРЫЙ образ, и молчание тут читалось
        #            бы как согласие.
        #
        # Сравнение РАВЕНСТВОМ, а не test(): (а) булево `true` старого образа не
        # равно строке "true", поэтому наполовину перекатившийся флот виден, а не
        # засчитан; (б) test() на булевом значении не вернул бы ложь, а УРОНИЛ бы
        # программу вердикта целиком — гейт печатал бы ошибку jq вместо находки.
        (if ($all_dims | not) or (.internal_mtls == "true") or (.internal_mtls == "n/a") then empty
         else "internal_mtls=\(shown("internal_mtls"))" end),
        (if ($all_dims | not) or (.authz_check   == true) then empty else "authz_check=\(shown("authz_check"))"   end),
        (if ($need_fwd | not) or (.trusted_forwarders == true) then empty
         else "trusted_forwarders=\(shown("trusted_forwarders"))" end),
        # ПОСАДКА ЛИЧНОСТИ (задача #1125). Судится НЕ «какая именно» — стенд
        # вправе быть на любой из двух полос, — а «объявлена ли она вообще».
        # Незаданная посадка означает, что процесс не выбрал, чем проверять
        # человека, и требования старта по ней не предъявлялись ни одни.
        #
        # Сервис без этого измерения печатает "n/a" и проходит: у него нет
        # человека, которого можно проверять. Отсутствие поля целиком —
        # ОТКАЗ, а не пропуск: так выглядит процесс со СТАРЫМ образом, и
        # молчание тут читалось бы как согласие.
        (if (.identity_provider // "" | test("^(external|own|n/a)$")) then empty
         else "identity_provider=\(shown("identity_provider"))" end)
      ] | join(", ")')"

    if [ -n "$verdict" ]; then
      if [ "$POSTURE_PROFILE" = dev ]; then
        fail "$svc/$p НЕ СУЗИЛ КРУГ ОТПРАВИТЕЛЕЙ ЧУЖОЙ ЛИЧНОСТИ: $verdict"
        echo "      живой самоотчёт: $line"
        echo "      под стартовал:   $(kubectl -n "$NS" get pod "$p" -o jsonpath='{.status.startTime}' 2>/dev/null)"
        echo "      Пустой список НЕ означает «не принимаем ни от кого»: транспорт"
        echo "      сужает круг только на непустом списке, а на пустом принимает"
        echo "      переданную личность от ЛЮБОГО пира с сертификатом внутреннего CA."
        echo "      Заполнить круг в профиле стенда по ФАКТИЧЕСКИМ отправителям"
        echo "      (граф импортов сервиса), затем перекатить под — ручка читается"
        echo "      один раз, на старте процесса."
      else
        fail "$svc/$p ИСПОЛНЯЕТСЯ НЕ В PRODUCTION-ПОСАДКЕ: $verdict"
        echo "      живой самоотчёт: $line"
        echo "      под стартовал:   $(kubectl -n "$NS" get pod "$p" -o jsonpath='{.status.startTime}' 2>/dev/null)"
        echo "      ВНИМАНИЕ: 'Ready' у такого пода ничего не опровергает — он не"
        echo "      перезапускался, поэтому и не падал. Настройки могли уже быть"
        echo "      переписаны в production, но процесс живёт со СТАРЫМ окружением."
      fi
    else
      # Та же `shown`, что и в отказе: успешная строка обязана печатать измерение
      # ровно так же, как печатал бы отказ, иначе два текста об одном и том же
      # поле расходятся (а `trusted_forwarders` здесь законно отсутствует —
      # у сервисов без этого измерения).
      ok "$svc/$p $(printf '%s' "$line" | jq -r '
        def shown($k): if has($k) then (.[$k] | tostring) else "<нет>" end;
        "auth_mode=\(shown("auth_mode")) db_sslmode=\(shown("db_sslmode")) public_mtls=\(shown("public_mtls")) internal_mtls=\(shown("internal_mtls")) authz_check=\(shown("authz_check")) trusted_forwarders=\(shown("trusted_forwarders")) identity_provider=\(shown("identity_provider"))"')"
    fi
  done
done

# ─────────────────────────────────────────────────────────────────────────────
# B. НЕЗАВИСИМЫЙ СВИДЕТЕЛЬ: сама БД сообщает, шифрованы ли соединения.
#
# Только боевой профиль: dev-профиль осознанно держит pg без TLS
# (sslmode=disable у каждого сервиса), поэтому здесь нечего доказывать.
# ─────────────────────────────────────────────────────────────────────────────
if [ "$POSTURE_PROFILE" = production ]; then
echo "=== B. независимое подтверждение: pg_stat_ssl (свидетельствует СУБД) ==="

for row in $SERVICES; do
  dep="${row%%|*}"; rest="${row#*|}"; svc="${rest%%|*}"; pg="${rest#*|}"
  skipped "$svc" && continue
  [ -z "$pg" ] && { echo "  — $svc: своей БД нет"; continue; }

  if ! kubectl -n "$NS" get pod "${pg}-0" >/dev/null 2>&1; then
    fail "$svc: statefulset ${pg} НЕ РАЗВЁРНУТ — шифрование его соединений НЕ ПОДТВЕРЖДЕНО"
    continue
  fi

  # Только TCP-соединения (client_addr IS NOT NULL). Локальный unix-socket
  # суперпользователя законно не шифруется — это служебные подключения чарта,
  # клиентский sslmode на них не распространяется.
  plain="$(kubectl -n "$NS" exec "${pg}-0" -c postgresql -- bash -c \
    'PGPASSWORD=$POSTGRES_POSTGRES_PASSWORD psql -U postgres -tAF"|" -c \
     "SELECT a.usename, a.client_addr, count(*) FROM pg_stat_activity a \
      JOIN pg_stat_ssl s USING (pid) \
      WHERE a.backend_type = '"'"'client backend'"'"' \
        AND a.client_addr IS NOT NULL AND NOT s.ssl \
      GROUP BY 1,2;"' 2>/dev/null | grep -v '^$' || true)"

  if [ -n "$plain" ]; then
    fail "$svc: у ${pg} есть НЕШИФРОВАННЫЕ TCP-соединения — sslmode НЕ ДЕЙСТВУЕТ:"
    echo "$plain" | sed 's/^/        /'
    echo "      Это свидетельство САМОЙ БАЗЫ, а не сервиса: настройки могут читаться"
    echo "      'require', но живой процесс говорит по плейнтексту."
  else
    enc="$(kubectl -n "$NS" exec "${pg}-0" -c postgresql -- bash -c \
      'PGPASSWORD=$POSTGRES_POSTGRES_PASSWORD psql -U postgres -tAc \
       "SELECT count(*) FROM pg_stat_activity a JOIN pg_stat_ssl s USING (pid) \
        WHERE a.backend_type='"'"'client backend'"'"' AND a.client_addr IS NOT NULL AND s.ssl;"' \
      2>/dev/null | tr -d '[:space:]')"
    if [ "${enc:-0}" -eq 0 ] 2>/dev/null; then
      fail "$svc: у ${pg} НЕТ НИ ОДНОГО TCP-соединения — доказывать нечего (сервис не подключён?)"
    else
      ok "$svc: все TCP-бэкенды ${pg} шифрованы (${enc} соединений на TLS)"
    fi
  fi
done
fi  # POSTURE_PROFILE=production — секция B

# ─────────────────────────────────────────────────────────────────────────────
# C. СОХРАНЁННЫЕ НАСТРОЙКИ — ПО ВСЕМ ПУТЯМ ДОСТАВКИ.
#
# Только боевой профиль: секция ищет `mode: dev` и `sslmode=disable`, которые
# dev-профиль несёт ЗАКОННО.
# ─────────────────────────────────────────────────────────────────────────────
if [ "$POSTURE_PROFILE" = production ]; then
echo "=== C. сохранённые настройки: все пути доставки (backstop намерения) ==="

# Разрешаем env КАЖДОГО контейнера из ВСЕХ источников сразу:
#   env[].value                        — инлайн
#   env[].valueFrom.configMapKeyRef    — точечная ссылка
#   envFrom[].configMapRef             — МАССОВЫЙ ИМПОРТ (прежнее слепое пятно)
# и печатаем «pod<TAB>ИМЯ=значение».
# ВАЖНО — данные передаются в jq ФАЙЛАМИ, а не через --argjson: ConfigMap'ы
# стенда легко перерастают лимит argv («Argument list too long»), и тогда jq
# падает, выдаёт ПУСТОЙ вывод, а проверка «нет плохих значений» радостно
# зеленеет. Это ровно тот класс дефекта, ради которого гейт переписан: пустой
# результат ОБЯЗАН отличаться от «проверка не выполнилась». Поэтому ниже у
# КАЖДОГО jq проверяется код возврата, и сбой — красный.
TMPD="$(mktemp -d)"
trap 'rm -rf "$TMPD"' EXIT

if ! kubectl -n "$NS" get configmap -o json >"$TMPD/cm.json" 2>/dev/null; then
  fail "не удалось прочитать ConfigMap'ы — проверка сохранённых настроек НЕ ВЫПОЛНЕНА"
  : >"$TMPD/cm.json"
fi
if ! kubectl -n "$NS" get pods -o json >"$TMPD/pods.json" 2>/dev/null; then
  fail "не удалось прочитать поды — проверка сохранённых настроек НЕ ВЫПОЛНЕНА"
  : >"$TMPD/pods.json"
fi

if ! bad_env="$(jq -r --slurpfile cms "$TMPD/cm.json" '
  ($cms[0].items | map({key: .metadata.name, value: (.data // {})}) | from_entries) as $cm
  | .items[]
  | select(.metadata.deletionTimestamp == null)
  | .metadata.name as $pod
  | .spec.containers[]?, .spec.initContainers[]?
  | ( [ .env[]? | select(.value != null)        | {name, value} ]
    + [ .env[]? | select(.valueFrom.configMapKeyRef != null)
        | {name, value: ($cm[.valueFrom.configMapKeyRef.name][.valueFrom.configMapKeyRef.key] // "")} ]
    + [ .envFrom[]? | select(.configMapRef != null)
        | ($cm[.configMapRef.name] // {}) | to_entries[] | {name: .key, value} ]
    )[]
  | select(
      ((.name | test("AUTH_?N?_MODE$")) and (.value | ascii_downcase) == "dev")
      or
      ((.name | test("SSLMODE$")) and ((.value | ascii_downcase) | test("^(disable|allow|prefer)$")))
    )
  | "\($pod)\t\(.name)=\(.value)"' "$TMPD/pods.json" | sort -u)"; then
  fail "разбор окружения подов СОРВАЛСЯ — проверка НЕ ВЫПОЛНЕНА (пустой вывод здесь не значит «чисто»)"
elif [ -n "$bad_env" ]; then
  fail "insecure значения в окружении подов (учитывая МАССОВЫЙ ИМПОРТ envFrom):"
  echo "$bad_env" | sed 's/^/        /'
else
  ok "ни одной insecure переменной ни по одному пути доставки (inline / keyRef / envFrom)"
fi

# Конфиг-ФАЙЛЫ: vpc/nlb/iam отдают режим и sslmode через config.yaml в ConfigMap.
if ! bad_cm="$(jq -r '
  .items[] | .metadata.name as $n | (.data // {}) | to_entries[]
  | .key as $k | (.value | split("\n"))[]
  | select(test("^[[:space:]]*(mode|authn-mode)[[:space:]]*:[[:space:]]*\"?dev\"?[[:space:]]*$")
        or test("^[[:space:]]*ssl-?mode[[:space:]]*:[[:space:]]*\"?(disable|allow|prefer)\"?[[:space:]]*$")
        or test("[?&]sslmode=(disable|allow|prefer)([&\"]|$)"))
  | "\($n)/\($k): \(. | gsub("^[[:space:]]+";""))"' "$TMPD/cm.json" | sort -u)"; then
  fail "разбор конфиг-файлов СОРВАЛСЯ — проверка НЕ ВЫПОЛНЕНА (пустой вывод здесь не значит «чисто»)"
elif [ -n "$bad_cm" ]; then
  fail "insecure значения в конфиг-файлах ConfigMap'ов:"
  echo "$bad_cm" | sed 's/^/        /'
else
  ok "ни одного insecure значения в конфиг-файлах ConfigMap'ов"
fi
fi  # POSTURE_PROFILE=production — секция C

# ─────────────────────────────────────────────────────────────────────────────
# D. АДМИНИСТРАТИВНЫЙ ПЕРЕХОД К ПРОВАЙДЕРУ ЛИЧНОСТИ.
#
# По этому переходу на КАЖДОМ промахе кэша интроспекции едет живой предъявитель
# конечного пользователя, а API, который он открывает, не аутентифицирует
# никого. Секция вынесена в отдельный файл: её предикаты вердикта обязаны быть
# вызываемы БЕЗ кластера (самопроверка на синтетических наблюдениях), а здешний
# файл целиком кластерный.
#
# Тот же принцип, что и у секции B: о шифровании сообщает ДАЛЬНИЙ КОНЕЦ
# соединения, а не значение в настройках стороны под оценкой.
# ─────────────────────────────────────────────────────────────────────────────
if [ "$POSTURE_PROFILE" = production ]; then
echo
if ! bash "$(dirname "$0")/assert-admin-hop-transport.sh"; then
  fail "административный переход к провайдеру не подтверждён — см. ✗ выше"
fi
fi  # POSTURE_PROFILE=production — секция D

echo
if [ "$POSTURE_PROFILE" = dev ]; then
  if [ "$FAILED" -ne 0 ]; then
    echo "!!! КРУГ ОТПРАВИТЕЛЕЙ НЕ СУЖЕН — см. ✗ выше."
    echo "    Это измерение от профиля НЕ зависит: пустой круг принимает переданную"
    echo "    личность от любого пира с сертификатом внутреннего CA, а mTLS включён"
    echo "    и на dev-стенде. auth_mode=dev лишь снимает стражу старта, которая на"
    echo "    боевом профиле не дала бы такому поду подняться."
    exit 1
  fi
  echo "круг отправителей сужен у КАЖДОГО сервиса, который принимает переданную личность"
  echo "  (профиль dev: градуировано ОДНО измерение — trusted_forwarders;"
  echo "   auth_mode/db_sslmode/mTLS здесь НЕ проверялись и НЕ доказаны)"
  exit 0
fi

if [ "$FAILED" -ne 0 ]; then
  echo "!!! PRODUCTION-POSTURE НЕ ПОДТВЕРЖДЕНА — см. ✗ выше."
  echo "    Напоминание: готовность подов ('Ready') НЕ является доказательством"
  echo "    посадки. Под, который не перекатился, не падал — и поэтому здоров."
  exit 1
fi
echo "production posture ПОДТВЕРЖДЕНА на живых процессах:"
echo "  A. каждый сервис сам отчитался production-посадкой (лог текущего контейнера)"
echo "  B. каждая БД подтвердила, что все её TCP-соединения шифрованы (pg_stat_ssl)"
echo "  C. сохранённые настройки чисты по ВСЕМ путям доставки (включая envFrom)"
echo "  D. административный переход шифрован — подтверждено пробником без настроек"
echo "     продукта и журналом дальнего конца соединения"
