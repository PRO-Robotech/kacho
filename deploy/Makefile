# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
.PHONY: dev-up dev-down dev-prod-up generated-scripts-parse assert-rollout-ready assert-jobs-healthy assert-production-posture \
	assert-metrics-surfaces-answer \
        assert-forwarder-narrowing assert-outbox-autovacuum helm-manifest-test \
        reload-svc logs-svc psql preflight e2e-test helm-lint \
        loadtest-address-allocate loadtest-address-allocate-clean \
        reload-svc-iam psql-iam logs-iam build-services fga-model-embed \
        guard-kind-context guard-destructive gate-self-test injection-proofs waiter-discipline \
        report-reader-discipline alt-fixture-discipline posture-branch-discipline \
        verdict-aggregator-discipline ceremony-declaration-discipline \
        dialled-transport-discipline delete-step-discipline \
        reload-svc-nlb psql-nlb logs-nlb seed-nlb seed-geo seed-storage seed-vpc-pools \
        seed-vpc-pools-check e2e-newman stand-provenance stand-provenance-self-test \
        managed-image-pins managed-image-pins-check stack-up

SHELL := bash

# СТАТУС КОНВЕЙЕРА НЕ ТЕРЯЕТСЯ. Без этой строки `helm template … | kubectl apply`
# и `kubectl create … | kubectl apply` отдают make код возврата ТОЛЬКО правой
# команды: сорванный рендер применяет ПУСТОЙ ввод и цель зеленеет. Объявляется
# файлом, а не построчно, чтобы защита распространялась и на конвейеры, которых
# тут ещё нет. `SHELL := bash` выше — обязательная половина: умолчание make
# (`/bin/sh`, на Debian dash) ручки `-o pipefail` не знает вовсе.
# Гейт класса — internal/repohygiene/pipefailguard_test.go.
.SHELLFLAGS := -o pipefail -c

# CLUSTER_NAME / DEV_UP_VALUES — параметры, а не константы.
#
# Разнесение e2e по раннерам (deploy/e2e-shards.json) поднимает НЕСКОЛЬКО kind-кластеров,
# каждый со своим набором компонентов. Совпадение имён означало бы, что снос кластера в
# одном job'е убивает чужой; одинаковый набор компонентов свёл бы разнесение на нет.
# Умолчания прежние — локальный `make dev-up` без аргументов ведёт себя как раньше.
#
#   make dev-up CLUSTER_NAME=kacho-vpc DEV_UP_VALUES='--set compute.enabled=false ...'
#
# DEV_UP_VALUES дописывается ко ВСЕМ трём фазам helm: иначе фаза 3 (production-посадка)
# вернула бы выключенные компоненты обратно, и сужение стенда прожило бы ровно до неё —
# ручка, которая принимается и отменяется следующим шагом.
CLUSTER_NAME := kacho
DEV_UP_VALUES ?=

# STACK_ARGS — цепочка `-f` стенда, ПРОЧИТАННАЯ из единственной таблицы дерева
# (deploy/stacks.txt), а не выписанная в рецепте. Рецепт и проверки обязаны
# поднимать один и тот же состав: пока цепочка стояла здесь своей копией,
# проверка могла честно судить стенд, которого рецепт не поднимает.
# `$(shell …)` вычисляется при разборе Makefile — отказ чтения таблицы виден
# сразу и до первого действия над кластером.
STACK_ARGS = $(shell bash tests/helm/stacks.sh --args $(1) ./helm/umbrella)
# Параметры цели stack-up. STACK умолчания НЕ имеет намеренно: цель меняет
# кластер, и «наверное, dev» выбором стенда не является.
STACK ?=
STACK_RELEASE ?= kacho-umbrella
STACK_NAMESPACE ?= kacho
STACK_TIMEOUT ?= 15m
STACK_EXTRA_ARGS ?=
# POSTURE_SKIP — сервисы, которых на этом стенде НЕТ по решению оператора.
# Пусто = стенд полный (умолчание, локальный `make dev-up`).
POSTURE_SKIP ?=

# KAC-228: build + kind-load all backend service images. The dev deployments
# reference local `kacho-<svc>:dev` images (values.dev.yaml), which kind cannot
# pull from a registry → they MUST be built locally before helm install, else
# pods sit in ImagePullBackOff and `helm --wait` times out. Single-repo builds: each
# Dockerfile is `COPY . .` + `go mod download` (versioned GitHub deps), so the
# build context is the service's OWN dir (kacho-$svc), NOT the parent — a
# workspace-root context would `COPY . .` a rootless tree → `go: no modules`.
SERVICES := iam vpc compute api-gateway nlb geo storage registry

# IMAGES — какие образы РЕАЛЬНО собирать. Умолчание — все восемь (локальный стенд
# поднимает всё). Шард e2e поднимает подмножество компонентов, и сборка образа,
# который на этом кластере не разворачивается, — чистая трата минут раннера:
# восемь `docker build` подряд стоят дороже, чем весь остальной bring-up.
# Шаблоны подов читают идентификатор образа через `dig … "unset"`, поэтому
# отсутствующая запись даёт аннотацию "unset", а не сорванный рендер.
IMAGES ?= $(SERVICES)

# BUILD_UI — собирать ли образы консоли. Умолчание 1: локальный `make dev-up` без
# аргументов ведёт себя как раньше и поднимает продукт целиком, вместе с
# интерфейсом.
#
# Ручка заведена потому, что отказаться от сборки консоли было НЕЧЕМ. `IMAGES`
# сужает только бэкенд (перечень SERVICES), а модули консоли выводятся из дерева
# и потому всегда полны — девять `docker build` независимо от того, нужна ли
# консоль этому стенду. На стендах e2e-newman она не нужна ни одному запросу
# (предикат — в deploy/e2e-shards.json §_uif), и это стоило около 31 ранер-минуты
# за прогон: девять образов × пять шардов, ни один из которых их не спрашивает.
#
# ЗНАЧЕНИЕ ПРИХОДИТ ИЗ ТОЙ ЖЕ СТРОКИ, ЧТО И ВЫКЛЮЧЕНИЕ ПОДЧАРТА: конвейер выводит
# его из состава компонентов шарда (`uif` в gates и не назван никем ⇒ и
# `--set uif.enabled=false`, и `BUILD_UI=0`). Второго объявления нет намеренно —
# два места об одном предмете разъехались бы молча, и худшая из двух форм
# расхождения тихая: стенд без консоли, а девять образов всё равно собираются.
BUILD_UI ?= 1

# PROVENANCE — every locally built image records the commit it came from.
#
# Every service image is tagged `:dev` and nothing else: no label, no embedded
# version, no sha anywhere. So "which code is this stand running?" had no direct
# answer and had to be reconstructed from `docker images` timestamps — which
# answers "when was it built", a different question, and one that goes wrong the
# moment two builds happen in a session or a rebuild is skipped.
#
# Величина передаётся `--build-arg` (см. IMAGE_BUILD_ARGS ниже), а Dockerfile из
# неё делает ДВА носителя: клеймо образа и файл /etc/kacho/image-revision.
# Читает их `make stand-provenance` — у РАБОТАЮЩИХ подов, а не у намеренных.
#
# ЗДЕСЬ СТОЯЛО «клейма ставятся из командной строки, править Dockerfile не надо».
# Довод был верен ровно до вопроса «кто это прочтёт»: клеймо у работающего
# контейнера не достать ничем, кроме демона хоста или реестра. Демона хоста у
# управляемого кластера нет, а образы стенда в реестр не публикуются — то есть
# ответ существовал только там, где его и так можно было получить иначе.
#
# NB: бинари Go объявляют `buildVersion`/`buildCommit` (четыре сервиса, и кормят
# ими метрику `*_build_info`), а комментарий рядом обещает подстановку через
# `-ldflags -X`. Ни одна сборка дерева этого не делает, поэтому метрики везде
# сообщают version="dev" commit="unknown". Это ОТДЕЛЬНЫЙ предмет и он не закрыт
# здесь: провенанс стенда отвечает на «какое дерево исполняется», а метрика — на
# «что бинарь о себе говорит», и второе по-прежнему говорит неправду.
#
# ОТМЕТКИ ВРЕМЕНИ СБОРКИ ЗДЕСЬ НЕТ — И ЭТО РЕШЕНИЕ, А НЕ ПРОПУСК.
#
# Метка входит в конфигурацию образа, а идентификатор содержимого — это дайджест
# конфигурации. Значит метка, вычисляемая заново в каждом под-make (`date -u`),
# делает идентификатор новым ДАЖЕ при стопроцентном попадании в кэш сборки: слои
# те же, конфигурация другая. Дальше срабатывает привязка ниже
# ($(IMAGE_IDS_VALUES)) — она работает верно и делает ровно то, ради чего
# заведена: новый идентификатор ⇒ новый шаблон пода ⇒ перекат. Итог: на
# НЕИЗМЕННОМ дереве `make dev-up` перекатывал все восемь подов и ждал это двумя
# `helm --wait` с потолками 10 и 12 минут.
#
# Предикат, которым это проверяется: два `make dev-up` подряд без единой правки
# обязаны дать ОДИН И ТОТ ЖЕ `docker image inspect --format '{{.Id}}'`.
#
# Чем заменён ответ на «когда это собрано». Тем же вопросом к демону:
# `docker image inspect --format '{{.Created}}'` — он записывает отметку сам, и
# при попадании в кэш она указывает на сборку, произведшую ЭТО содержимое, что и
# есть правильный ответ. Метка отвечала на другой вопрос — «когда мы в последний
# раз позвали docker build», — и ценой ответу был перекат стенда.
#
# «Какой код на стенде» остаётся за `revision`/`version`: они функция ДЕРЕВА, а не
# часов, поэтому на неизменном дереве постоянны и `stand-provenance` читает их
# по-прежнему. Читателей у снятой метки в дереве нет ни одного (единственное
# совпадение — клиент zot, читающий одноимённую OCI-аннотацию у образов
# арендатора; к меткам наших образов оно отношения не имеет).
# Величина провенанса объявлена ОДИН раз на дерево — иначе девять рецептов,
# собирающих `kacho-<svc>:dev`, несли бы девять копий одного предиката.
include ../provenance.mk

# IMAGE_IDS_VALUES — карта «сервис → идентификатор СОДЕРЖИМОГО его образа».
#
# Зачем. Все локальные образы носят один неизменный тег `:dev`. Пересборка под тем
# же тегом не меняет шаблон пода, поэтому Kubernetes не видит причины перекатывать
# под — и процесс продолжает работать СТАРЫМ бинарником, пока тег утверждает
# обратное. На стенде это до сих пор скрывалось: `dev-up` двухфазный и меняет
# настройки дважды, так что поды перекатывались ПОБОЧНО, из-за смены настроек. Один
# `helm upgrade` при неизменных настройках их бы не перекатил.
#
# Это тот же разрыв между намерением и исходом, что и у настроек, приезжающих
# массовым импортом (инцидент 2026-07-25): хранимое обновилось, исполняемое — нет,
# и «под Ready» этого не опровергает.
#
# Файл генерируется build-services и НЕ хранится в git (значения машинно-локальны).
IMAGE_IDS_VALUES := helm/umbrella/values.image-ids.yaml

build-services:
	@echo "=== provenance: $(GIT_REF) @ $(GIT_COMMIT)$(GIT_DIRTY) ==="
	@for svc in $(IMAGES); do \
	  echo "=== build kacho-$$svc:dev ==="; \
	  d=$$(test "$$svc" = api-gateway && echo gateway || echo services/$$svc); \
	  ( cd .. && docker build -f $$d/Dockerfile $(IMAGE_BUILD_ARGS) -t kacho-$$svc:dev . ) || exit 1; \
	  kind load docker-image kacho-$$svc:dev --name $(CLUSTER_NAME) || exit 1; \
	done
	@# Идентификатор содержимого берётся у демона ПОСЛЕ сборки: это то, что реально
	@# лежит под тегом, а не то, что мы намеревались собрать. Пустой ответ — отказ:
	@# «не смогли определить» не должно превращаться в «привязка не нужна».
	@echo "# generated by 'make build-services' — не редактировать, не коммитить" > $(IMAGE_IDS_VALUES)
	@echo "global:" >> $(IMAGE_IDS_VALUES)
	@echo "  kachoImageIds:" >> $(IMAGE_IDS_VALUES)
	@for svc in $(IMAGES); do \
	  id=$$(docker image inspect --format '{{.Id}}' kacho-$$svc:dev 2>/dev/null); \
	  if [ -z "$$id" ]; then \
	    echo "ERROR: не определён идентификатор содержимого образа kacho-$$svc:dev"; exit 1; \
	  fi; \
	  echo "    $$svc: \"$$id\"" >> $(IMAGE_IDS_VALUES); \
	done
	@echo "=== образы привязаны к шаблонам подов: $(IMAGE_IDS_VALUES) ==="

# UI_PROJECTS — модули консоли. ВЫВОДЯТСЯ ИЗ ДЕРЕВА, а не выписываются: перечень
# уже живёт в матрице `ui.yml`, и вторая рукописная копия разошлась бы с ней молча
# (ровно так в полирепо из матрицы выпали compute и storage — их образы не
# проверялись вообще). Признак модуля — наличие своего Dockerfile, тот же, по
# которому строит конвейер.
UI_PROJECTS := $(sort $(notdir $(patsubst %/Dockerfile,%,$(wildcard ../ui-future/*/Dockerfile))))

## build-ui — собрать образы консоли и загрузить их в kind.
##
## Консоль — микрофронтенд: оболочка плюс модули по одному на домен. До заведения
## этой цели её образы не строились для стенда НИКАКОЙ целью: конвейер `ui.yml`
## строит их тегом `:ci` и никуда не публикует, `build-services` собирает только
## бэкенд, а профили подчарт консоли не включают. То есть консоль не
## разворачивалась ни на одном стенде — при том что чарт для неё существует.
build-ui:
	@test -n "$(UI_PROJECTS)" || { \
	  echo "ERROR: в ../ui-future не найдено ни одного модуля с Dockerfile — перечень выводится"; \
	  echo "       из дерева, и пустой означает, что предмет цели исчез, а не что собирать нечего"; \
	  exit 1; }
	@echo "=== provenance: $(GIT_REF) @ $(GIT_COMMIT)$(GIT_DIRTY) ==="
	@echo "=== модулей консоли: $(words $(UI_PROJECTS)) [$(UI_PROJECTS)] ==="
	@for p in $(UI_PROJECTS); do \
	  echo "=== build kacho-ui-future-$$p:dev ==="; \
	  ( cd ../ui-future && docker build -f $$p/Dockerfile $(IMAGE_BUILD_ARGS) \
	      -t kacho-ui-future-$$p:dev . ) || exit 1; \
	  kind load docker-image kacho-ui-future-$$p:dev --name $(CLUSTER_NAME) || exit 1; \
	done
	@echo "=== консоль собрана и загружена в kind ==="

# stand-provenance — какую РЕВИЗИЮ дерева исполняет каждый контейнер стенда.
#
# Логика вынесена в deploy/scripts/stand-provenance.sh целиком, и это не вкус:
# рецепт make нельзя ни прогнать без кластера, ни доказать инъекцией. Скрипт
# несёт `--self-test`, подменяющий kubectl и docker заглушками, и потому
# способность цели ПОКРАСНЕТЬ проверяема без стенда.
#
# Коды возврата различают три исхода (0 — у всех установлена, 1 — у части нет,
# 2 — предпосылка исчезла). Прежняя редакция на недоступном кластере печатала
# заголовок и выходила успехом.
# ТРИ ИСХОДА СКРИПТА СХЛОПЫВАЮТСЯ В ОДИН КОД MAKE — ПОЭТОМУ ОНИ НАЗВАНЫ СЛОВАМИ.
# make выходит кодом 2 на ЛЮБОМ отказе рецепта, поэтому различие «у части не
# установлена» (1) и «стенд не опрошен» (2) через него не проходит. Читающему
# машинно — звать scripts/stand-provenance.sh напрямую; читающему глазами — строка
# ВЕРДИКТ ниже.
stand-provenance:
	@echo "рабочая копия: $(GIT_REF) @ $(GIT_COMMIT)$(GIT_DIRTY)"
	@bash scripts/stand-provenance.sh --namespace kacho; rc=$$?; \
	  case $$rc in \
	    0) echo "ВЕРДИКТ: ревизия установлена у каждого осмотренного контейнера (код 0)" ;; \
	    1) echo "ВЕРДИКТ: у части контейнеров ревизия НЕ установлена (код 1) — они названы выше" ;; \
	    2) echo "ВЕРДИКТ: стенд НЕ опрошен, предпосылка исчезла (код 2) — это не «сходится»" ;; \
	    *) echo "ВЕРДИКТ: неожиданный код $$rc" ;; \
	  esac; \
	  exit $$rc

## stand-provenance-self-test — доказательство инъекцией, кластера не требует.
stand-provenance-self-test:
	@bash scripts/stand-provenance.sh --self-test

# KAC-127: CI docker-compose stack (ci-images / ci-up / ci-seed / ci-down /
# ci-logs) удалён — он был построен на упразднённом kacho-resource-manager
# (ci/docker-compose.yml + ci/seed.sh удалены). newman E2E гоняется против
# kind+helm dev-stand (`make dev-up`).

preflight:
	@command -v docker >/dev/null || { echo "ERROR: docker not installed"; exit 1; }
	@command -v kind >/dev/null || { echo "ERROR: kind not installed (install from https://kind.sigs.k8s.io/)"; exit 1; }
	@command -v kubectl >/dev/null || { echo "ERROR: kubectl not installed"; exit 1; }
	@command -v helm >/dev/null || { echo "ERROR: helm not installed"; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "ERROR: docker daemon is not running"; exit 1; }
	@grep -q "api.kacho.local" /etc/hosts || echo "note: 'api.kacho.local' is not in /etc/hosts. Nothing in the test path needs it — the e2e harness reaches every listener over kubectl port-forward, including the advertised external TLS one (:8443 -> localhost:18443). The entry is only useful for driving the Ingress by hand, and even then: kind publishes node:80 -> host:28080 (443 is not published at all) and that Ingress is backend-protocol GRPCS, so REST through it answers 502."
	@echo "preflight OK"

# guard-kind-context — АВАРИЙНЫЙ СТОП, если активный контекст ведёт НЕ в наш kind-кластер.
#
# Зачем (реальный near-miss 2026-07-15): kind-кластер `kacho` существовал, а его контекст
# в kubeconfig отсутствовал (KUBECONFIG был подменён на прод-кластер). `dev-up` шёл
# `; \`-цепочкой, поэтому упавший `kubectl config use-context kind-kacho` НЕ прерывал
# цель — и весь стенд (namespace, labels, helm upgrade --install kacho-umbrella) поехал
# на ТЕКУЩИЙ контекст, то есть на ПРОД. Прод спасла лишь случайность: helm отверг
# апгрейд из-за pre-created секретов (ownership check) и не создал ревизию.
#
# ПРОВЕРЯЕТСЯ КЛАСТЕР, А НЕ ЕГО ИМЯ. Прежняя версия сверяла только ИМЯ активного
# контекста с `kind-<CLUSTER_NAME>`. Имя контекста выбирает тот, кто написал
# kubeconfig, — совпадением имён не доказывается ничего. Измерено 2026-07-29 на
# рабочей машине: kubeconfig соседнего управляемого кластера содержал ДВА контекста —
# свой и одноимённый нашему, — активным был второй, а его apiserver отличался от
# живого kind-кластера. Гейт на таком kubeconfig выходил с кодом 0. То есть защита
# от подменённого KUBECONFIG не срабатывала ровно в том сценарии, ради которого
# написана: подменённый файл её проходил.
#
# Поэтому решает совпадение АДРЕСА APISERVER'А активного контекста с адресом,
# который выдаёт сам kind для кластера $(CLUSTER_NAME). `kind get kubeconfig` читает
# проброшенный порт локального docker-контейнера, к кластеру не ходит и стоит ~0.15 c —
# это авторитет, который нельзя подделать переименованием контекста.
#
# Имя контекста проверяется по-прежнему — как дешёвый первый отсев с внятным советом.
guard-kind-context:
	@ctx="$$(kubectl config current-context 2>/dev/null)"; \
	case "$$ctx" in \
	  kind-$(CLUSTER_NAME)) ;; \
	  *) echo "ABORT: активный kube-контекст '$$ctx' — НЕ kind-$(CLUSTER_NAME)."; \
	     echo "       dev-стенд создаёт/меняет ресурсы и МОГ БЫ поехать на чужой (в т.ч. прод) кластер."; \
	     echo "       Переключись явно:  kubectl config use-context kind-$(CLUSTER_NAME)"; \
	     exit 1 ;; \
	esac; \
	want="$$(kind get kubeconfig --name $(CLUSTER_NAME) 2>/dev/null | sed -n 's/^ *server: *//p' | head -1)"; \
	have="$$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"; \
	if [ -z "$$want" ]; then \
	  echo "ABORT: kind не знает кластера '$(CLUSTER_NAME)' — сверить адрес apiserver'а не с чем."; \
	  echo "       «не с чем сверить» здесь означает «не проверили», а не «всё хорошо»."; \
	  echo "       Подними стенд (make dev-up) либо восстанови kubeconfig:"; \
	  echo "         kind export kubeconfig --name $(CLUSTER_NAME)"; \
	  exit 1; \
	fi; \
	if [ "$$want" != "$$have" ]; then \
	  echo "ABORT: контекст называется kind-$(CLUSTER_NAME), но ведёт НЕ в этот кластер."; \
	  echo "       активный контекст ведёт на    → $$have"; \
	  echo "       kind-$(CLUSTER_NAME) на самом деле → $$want"; \
	  echo "       Имя контекста выбирает автор kubeconfig; совпадение имён ничего не доказывает."; \
	  echo "       Восстанови:  kind export kubeconfig --name $(CLUSTER_NAME)"; \
	  exit 1; \
	fi

# guard-declared-context — НЕИНТЕРАКТИВНЫЙ страж цели: писать можно ТОЛЬКО в тот
# кластер, который вызывающий НАЗВАЛ.
#
# ТРЕТИЙ СТРАЖ ЗАВЕДЁН ПОТОМУ, ЧТО ДВУХ ПРЕЖНИХ НЕ ХВАТАЕТ BY CONSTRUCTION, и это
# измерено, а не предположено. Цель `module-manifests-configmap` применяет объект
# в кластер активного контекста, и ни один из двух прежних стражей на ней не
# держится:
#
#   • `guard-kind-context` ОТВЕРГАЕТ всякий контекст, кроме нашего локального, —
#     значит цель, несущая его, неисполнима на боевых путях выкатки (`stack-up`,
#     `helm/umbrella/cutover-fe3455.sh`). Ровно за это он с неё и был снят
#     (kacho#1901), и свойство это держит гейт
#     `TestManifestProducerIsCallableFromEveryBringUpPath`;
#   • `guard-destructive` требует ТЕРМИНАЛА («подтверждение ввести некому») —
#     а `make dev-up` идёт в конвейере без stdin-терминала. Он сломал бы каждый
#     прогон e2e-newman, консоли и посадки.
#
# Снятие первого оставило цель БЕЗ гейта вовсе, и это нашёл гейт каталога
# `deploy/tests/helm/makefile-destructive-guarded-test.sh`: цель пишет в кластер,
# гейта цели не несёт. Оба вердикта верны, и оба сохранены — вместо того чтобы
# выбирать между ними, словарь стражей вырос на третий член.
#
# ЧТО ОН ПРОВЕРЯЕТ: контекст РАЗРЕШЁН живым kubectl и СОВПАДАЕТ с объявленным.
# Незаданное объявление — отказ, а не «наверное, тот самый»: у цели, пишущей в
# кластер активного контекста, умолчания быть не может (тот же довод, по которому
# `stack-up` требует `STACK`, а `guard-destructive` отказывает на неразрешённой цели).
#
# ГРАНИЦА НАЗВАНА ЧЕСТНО, ЧТОБЫ ЕЁ НЕ ПРИНЯЛИ ЗА ВТОРУЮ ПРОВЕРКУ. Его предмет —
# ПРЯМОЙ вызов цели человеком, у которого подтверждения нет вовсе: там объявление
# и есть единственный гейт. Там, где вызывающий уже подтвердил цель СВОИМ стражем
# (`dev-up` → `guard-kind-context`, `stack-up` → `guard-destructive`, cutover →
# сверка адреса apiserver'а), объявление ПОВТОРЯЕТ подтверждённое. Исключение —
# `dev-up`: он объявляет имя литералом, и там это полноценная проверка.
guard-declared-context:
	@ctx="$$(kubectl config current-context 2>/dev/null)"; \
	if [ -z "$$ctx" ]; then \
	  echo "ABORT: активную цель разрешить не удалось (kubectl не отдал контекст)."; \
	  echo "       Неназванная цель — это отказ, а не «наверное, локально»."; \
	  exit 1; \
	fi; \
	if [ -z "$(EXPECT_CONTEXT)" ]; then \
	  echo "ABORT: EXPECT_CONTEXT не задан, а цель пишет в кластер активного контекста '$$ctx'."; \
	  echo "       Умолчания здесь быть не может. Зови через путь выкатки, который несёт"; \
	  echo "       своего стража (dev-up / stack-up / helm/umbrella/cutover-fe3455.sh),"; \
	  echo "       либо назови кластер явно:  make <цель> EXPECT_CONTEXT=$$ctx"; \
	  exit 1; \
	fi; \
	if [ "$(EXPECT_CONTEXT)" != "$$ctx" ]; then \
	  echo "ABORT: объявлен контекст '$(EXPECT_CONTEXT)', активен '$$ctx'."; \
	  echo "       Цель применяет объекты в кластер АКТИВНОГО контекста — они уехали бы не туда."; \
	  echo "       Переключись явно:  kubectl config use-context $(EXPECT_CONTEXT)"; \
	  exit 1; \
	fi

# guard-destructive — подтверждение вводом для операции, которая что-то РУШИТ,
# с называнием РЕАЛЬНОЙ цели.
#
# Зачем отдельно от guard-kind-context. Тот пинит цель к нашему kind-кластеру и
# годится всему, что предназначено ТОЛЬКО стенду. Но часть операций по замыслу
# исполняется и на настоящем кластере (аварийный runbook SPIRE) — им нельзя
# запретить чужой контекст; им нужно, чтобы человек УВИДЕЛ, куда именно сейчас
# поедет команда, и подтвердил это вводом.
#
# ЦЕЛЬ РАЗРЕШАЕТСЯ ЖИВЫМ kubectl, а не берётся из переменной окружения. Прежнее
# предупреждение печатало значение KUBECONFIG, которое при дефолтном ~/.kube/config
# ПУСТО — то есть в самом частом случае предупреждение не называло ничего, и
# читатель видел строку без цели. Печатаем контекст, кластер, apiserver и
# namespace — то, что kubectl реально применит.
#
# Вызывается ИЗ РЕЦЕПТА (нужны параметры, а зависимости их не принимают):
#   $(MAKE) --no-print-directory guard-destructive OP=… TOKEN=… WHAT=…
#
# Ввести подтверждение некому (не-tty, конвейер, CI) → ОТКАЗ. Разрушительная
# операция не имеет права исполняться оттого, что спросить было не у кого.
guard-destructive:
	@op="$(OP)"; token="$(TOKEN)"; what="$(WHAT)"; \
	if [ -z "$$op" ] || [ -z "$$token" ]; then \
	  echo "guard-destructive: OP и TOKEN обязательны (ошибка вызова, не пользователя)"; exit 2; \
	fi; \
	ctx="$$(kubectl config current-context 2>/dev/null)"; \
	cluster="$$(kubectl config view --minify -o jsonpath='{.contexts[0].context.cluster}' 2>/dev/null)"; \
	server="$$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"; \
	ns="$$(kubectl config view --minify -o jsonpath='{.contexts[0].context.namespace}' 2>/dev/null)"; \
	if [ -z "$$ctx" ] || [ -z "$$server" ]; then \
	  echo "ABORT: активную цель разрешить не удалось (kubectl не отдал контекст/адрес)."; \
	  echo "       Неназванная цель — это отказ, а не «наверное, локально»."; \
	  exit 1; \
	fi; \
	echo "==================================================================="; \
	echo " РАЗРУШИТЕЛЬНАЯ ОПЕРАЦИЯ: $$op"; \
	echo " что произойдёт: $$what"; \
	echo; \
	echo " ЦЕЛЬ (разрешена живым kubectl, не из переменной окружения):"; \
	echo "   контекст   $$ctx"; \
	echo "   кластер    $$cluster"; \
	echo "   apiserver  $$server"; \
	echo "   namespace  $${ns:-<задаётся самой командой>}"; \
	echo "==================================================================="; \
	if [ ! -t 0 ]; then \
	  echo "ABORT: подтверждение ввести некому (stdin не терминал)."; \
	  echo "       Разрушительная операция не исполняется без подтверждения человеком."; \
	  exit 1; \
	fi; \
	printf "Введите '%s' для подтверждения: " "$$token"; \
	read -r confirm; \
	if [ "$$confirm" != "$$token" ]; then echo "Отменено."; exit 1; fi

# CERT_MANAGER_RELEASE / CERT_MANAGER_CHART — cert-manager ставится ОТДЕЛЬНЫМ
# релизом, до продукта.
#
# ПОЧЕМУ. Раньше подъём был двухфазным: сперва весь продукт ставился с
# `$(MTLS_OFF)` — ради того, чтобы появились CRD cert-manager, — и только потом
# перекатывался с mTLS. То есть на КАЖДОМ подъёме чистого кластера продукт
# какое-то время работал в посадке, которую ban #16 запрещает на поднятом
# кластере: mTLS снят у всех подчартов И включён аварийный обход авторизации
# реестра (`registry.authz.breakglass=true`).
#
# Причина двухфазности была структурной, а не небрежностью: Certificate нельзя
# применить, пока нет CRD, а CRD приезжают с cert-manager, который лежал внутри
# той же умбреллы. Разрывается это не флагами, а порядком: cert-manager ни от
# чего нашего не зависит, поэтому ставится своим релизом первым, а продукт —
# ОДНИМ прогоном и сразу в боевой посадке.
#
# Радиус переименования измерен: релизное имя cert-manager называл ровно ОДИН
# файл дерева — этот.
CERT_MANAGER_RELEASE := kacho-cert-manager
CERT_MANAGER_CHART   := ./helm/umbrella/charts/cert-manager-v1.16.5.tgz

# UMBRELLA_OPTS — то, что обязан нести КАЖДЫЙ вызов helm по умбрелле.
#
# Вынесено в переменную, а не дописано к вызовам, по цене одной ошибки: правя
# подъём, я поставил `cert-manager.enabled=false` на один вызов из трёх и получил
# отказ на третьем — helm попытался присвоить CRD, которыми уже владеет отдельный
# релиз («invalid ownership metadata»). Отказ был громкий и потому дешёвый, но
# следующий вызов умбреллы забыл бы флаг так же, и заметить это можно было бы
# уже на чужом кластере. Переменная делает забывание невозможным.
UMBRELLA_OPTS := --set cert-manager.enabled=false

# ФАЗЫ 1 БОЛЬШЕ НЕТ — и вместе с ней снята вся её оснастка.
#
# Здесь стояли `MTLS_SUBCHARTS`, `MTLS_OFF` и цель `check-mtls-off-complete`.
# Их предметом была ровно одна вещь: первый helm-прогон обязан был обойтись без
# Certificate, потому что CRD приезжали вместе с cert-manager — сабчартом той же
# умбреллы. Ради этого продукт поднимался с mTLS, снятым у ВСЕХ подчартов, и с
# включённым аварийным обходом авторизации реестра
# (`registry.authz.breakglass=true`), то есть в посадке, которую ban #16
# запрещает на поднятом кластере. На каждом подъёме чистого кластера.
#
# cert-manager вынесен в собственный релиз ДО продукта (см. CERT_MANAGER_RELEASE
# выше), поэтому CRD существуют раньше первого объекта продукта, а продукт
# ставится ОДНИМ прогоном и сразу в боевой посадке. Незащищённого окна больше
# нет — не «оно короткое», а его не существует.
#
# Снято, а не оставлено «на всякий»: `$(MTLS_OFF)` после правки не имел ни одной
# подстановки, а гейт `check-mtls-off-complete` защищал полноту списка для фазы,
# которой нет. Исключение живёт, пока у него есть предмет.

# `set -e` — ОБЯЗАТЕЛЕН, а не косметика. Рецепт — одна `; \`-цепочка, т.е. упавшая
# команда НЕ прерывает цель: следующая просто выполняется. Реально наблюдалось
# 2026-07-16 в CI — `helm phase 2` упал с UPGRADE FAILED, а цель дошла до конца и
# напечатала «dev-up complete», отдав exit 0. Стенд был собран наполовину (mTLS-фаза не
# применена), а CI считал его поднятым и шёл гонять newman —
# краснота вылезла позже и совсем в другом месте.
# Тот же класс, что near-miss 2026-07-15 (см. guard-kind-context ниже): упавший
# `use-context` не прервал цепочку, и стенд поехал на ПРОД.
#
# И `set -e` ЗАКРЫВАЕТ НЕ ВСЁ: он не прерывает выполнение, когда падает команда
# ВНУТРИ `&&`-списка и после неё есть ещё одно звено — правило bash освобождает от
# выхода все звенья, кроме последнего. Проверяется однострочником:
#   bash -c 'set -e; false && echo x && echo y; echo ПОСЛЕ'  → печатает ПОСЛЕ, rc=0.
# Наблюдалось здесь 2026-08-05 на подкачке подчартов: `helm dep update` не смог
# скачать зависимость (сетевой отказ), выход не сработал — и цель пошла дальше,
# оставшись в helm/umbrella, потому что возврат `cd ../..` был третьим звеном и не
# исполнился. Следующая команда упала с `path "./helm/umbrella" not found` —
# сообщением, которое уводит от причины: каталог на месте, не на месте был рабочий
# каталог. Поэтому такие пары пишутся ПОДОБОЛОЧКОЙ `( cd X && cmd )`: её отказ —
# отказ statement-уровня (set -e срабатывает в точке причины), а рабочий каталог
# не может утечь в следующие звенья. Та же форма уже стояла в helm-manifest-test.
dev-up: preflight
	@set -e; start=$$(date +%s); \
	grep -qx "$(CLUSTER_NAME)" <<<"$$(kind get clusters)" || CLUSTER_NAME=$(CLUSTER_NAME) ./kind/create-cluster.sh; \
	kubectl config use-context kind-$(CLUSTER_NAME) || { \
	  echo "ERROR: нет kubectl-контекста kind-$(CLUSTER_NAME)."; \
	  echo "       Кластер kind может существовать, а контекст в kubeconfig — отсутствовать"; \
	  echo "       (напр. KUBECONFIG подменён на другой кластер). Восстанови:"; \
	  echo "         kind export kubeconfig --name $(CLUSTER_NAME)"; \
	  exit 1; \
	}; \
	$(MAKE) --no-print-directory guard-kind-context; \
	kubectl create namespace kacho --dry-run=client -o yaml | kubectl apply -f - >/dev/null; \
	kubectl label namespace kacho \
	  pod-security.kubernetes.io/warn=restricted pod-security.kubernetes.io/warn-version=latest \
	  pod-security.kubernetes.io/audit=restricted pod-security.kubernetes.io/audit-version=latest \
	  --overwrite >/dev/null; \
	$(MAKE) build-services; \
	if [ "$(BUILD_UI)" = "0" ]; then \
	  echo "=== консоль НЕ собирается (BUILD_UI=0) — подчарт uif на этом стенде выключен ==="; \
	else \
	  $(MAKE) build-ui; \
	fi; \
	./scripts/gen-tls-cert.sh; \
	bash scripts/helm-umbrella-deps.sh; \
	echo "=== cert-manager: ОТДЕЛЬНЫЙ релиз, до продукта ==="; \
	helm upgrade --install $(CERT_MANAGER_RELEASE) $(CERT_MANAGER_CHART) -n kacho --create-namespace \
	  --set crds.enabled=true \
	  --wait --timeout 5m; \
	echo "=== ждём вебхук cert-manager (до применения Issuer/Certificate) ==="; \
	kubectl -n kacho rollout status deploy/$(CERT_MANAGER_RELEASE)-webhook --timeout=180s; \
	echo "=== предусловные секреты: ПЕРЕД первым прогоном helm, а не между прогонами ==="; \
	echo "    Общий секрет обратных вызовов провайдер берёт ОБЯЗАТЕЛЬНОЙ ссылкой уже в"; \
	echo "    базовом профиле: необязательная ссылка на отсутствующий секрет дала бы"; \
	echo "    пустую величину, полосу отвергли бы на первом обращении, а стенд выглядел"; \
	echo "    бы поднятым (задача #948). Значит секрет — предусловие, а не следствие."; \
	./scripts/dev-prod-secrets.sh; \
	echo "=== манифесты модулей: ПРЕДУСЛОВИЕ, а не следствие (задача #1901) ==="; \
	echo "    Под монтирует именованный ConfigMap с манифестами модулей, а процесс"; \
	echo "    читает смонтированный каталог на старте и ОТКАЗЫВАЕТСЯ подниматься на"; \
	echo "    пустом: отсутствие манифеста снятием модуля не является. Значит объект"; \
	echo "    обязан существовать ДО первого прогона helm, а не появиться после."; \
	$(MAKE) --no-print-directory module-manifests-configmap MODULE_MANIFESTS_STACK=dev \
	  EXPECT_CONTEXT=kind-$(CLUSTER_NAME); \
	echo "=== продукт: ОДИН helm-прогон, сразу в боевой посадке (mTLS включён) ==="; \
	helm upgrade --install kacho-umbrella ./helm/umbrella -n kacho --create-namespace \
	  $(call STACK_ARGS,dev) \
	  -f ./$(IMAGE_IDS_VALUES) \
	  $(UMBRELLA_OPTS) $(DEV_UP_VALUES) \
	  --wait --timeout 10m; \
	echo "Waiting for ingress-nginx admission webhook..."; \
	kubectl -n kacho wait --for=condition=ready pod -l app.kubernetes.io/component=controller --timeout=60s; \
	echo "=== helm phase 3: PRODUCTION posture (values.dev-prod.yaml поверх dev) ==="; \
	helm upgrade kacho-umbrella ./helm/umbrella -n kacho \
	  $(call STACK_ARGS,dev-prod) \
	  -f ./$(IMAGE_IDS_VALUES) \
	  $(UMBRELLA_OPTS) $(DEV_UP_VALUES) \
	  --wait --timeout 12m; \
	kubectl -n kacho rollout status deploy/kacho-iam --timeout=180s || true; \
	kubectl -n kacho rollout status deploy/api-gateway --timeout=180s || true; \
	$(MAKE) --no-print-directory assert-forwarder-narrowing; \
	echo "=== базовый каталог geo (Region/Zone) ==="; \
	echo "    Каталог geo стартует ПУСТЫМ, и пока он пуст, КАЖДОЕ создание зонального"; \
	echo "    или регионального ресурса отвечает «зона/регион не найдены» (peer-validate"; \
	echo "    к geo на пути запроса, fail-closed). Поднятый стенд без этого шага"; \
	echo "    непригоден, и ни один «под Ready» этого не показывает."; \
	$(MAKE) --no-print-directory seed-geo; \
	echo "=== базовый каталог хранения (кластер данных, классы, привязки) ==="; \
	echo "    Каталог классов тоже стартует ПУСТЫМ: класс не предлагается раньше,"; \
	echo "    чем объявлено, ЧЕМ он обслуживается. Пока он пуст, каждое создание тома"; \
	echo "    отвечает «класс не найден». Зоны привязок спрашиваются у geo, поэтому"; \
	echo "    шаг идёт СТРОГО после посева каталога размещения."; \
	$(MAKE) --no-print-directory seed-storage; \
	echo "=== базовый каталог внешних адресов (полоса аникаста) ==="; \
	echo "    Внешнее размещение бывает только региональным, значит внешний балансировщик"; \
	echo "    зоне-независим by construction и берёт публичный адрес из полосы zone_id IS NULL."; \
	echo "    Пока эта полоса пуста, КАЖДОЕ его создание отвечает «could not allocate load"; \
	echo "    balancer address», сага сносит handle компенсацией, а GET по названному"; \
	echo "    идентификатору отдаёт настоящий 404. Ни один «под Ready» этого не показывает."; \
	$(MAKE) --no-print-directory seed-vpc-pools; \
	end=$$(date +%s); \
	echo "dev-up complete in $$((end-start))s"; \
	echo; \
	echo "API endpoint:"; \
	echo "  REST   http://localhost:18080         (kubectl port-forward svc/api-gateway 18080:8080)"; \
	echo "  TLS    https://localhost:18443        (kubectl port-forward svc/api-gateway 18443:8443 — the advertised external listener)"; \
	echo "  Ingress http://api.kacho.local:28080  (needs '127.0.0.1 api.kacho.local' in /etc/hosts; gRPC only — backend-protocol is GRPCS, REST answers 502, and 443 is not published by kind)"

dev-down:
	kind delete cluster --name $(CLUSTER_NAME) || true

# ─────────────────────────────────────────────────────────────────────────────
# dev-prod-up — стенд в PRODUCTION-security-posture + СТРОГАЯ проверка, что он
# действительно поднялся.
#
# ЗАЧЕМ ОТДЕЛЬНАЯ ЦЕЛЬ. `helm lint`/`helm template` НЕ являются доказательством
# работоспособности production-профиля: production boot-guard'ы (Config.Validate →
# refuse-to-start при insecure config) срабатывают в РАНТАЙМЕ, а шаблон при этом
# рендерится идеально. Реальный инцидент: chart api-gateway рендерился зелёным, но
# не нёс knob'а для mTLS+SPIFFE на internal :9091 → под уходил в crash-loop ровно на
# values.prod. Единственный способ это поймать — поднять и дождаться rollout.
#
# ЧТО ЭТО ДОКАЗЫВАЕТ: каждый сервис СТАРТУЕТ с production-конфигом (authMode=
# production/-strict, mTLS везде, sslmode=require, breakglass off) и доходит до
# Ready. То есть чарты несут все knob'ы, которых требуют их boot-guard'ы.
# ЧЕГО НЕ ДОКАЗЫВАЕТ: функциональную корректность authz/токенов — это e2e-newman.
#
# ПОЧЕМУ ПОВЕРХ dev-up, а не отдельный fresh-install: bootstrap чистого кластера
# несёт свой порядок (cert-manager отдельным релизом до продукта, затем один
# helm-прогон продукта, затем секреты).
# Дублировать этот рецепт значит завести вторую копию, которая
# разъедется с оригиналом. Здесь мы переиспользуем проверенный bring-up; сам перевод
# в production-posture (секреты + фаза 3) теперь ЧАСТЬ `dev-up`, а эта цель осталась
# ГЕЙТОМ поверх него — она утверждает, что каждый под перезапустился с production-
# конфигом и снова стал Ready. Именно это и есть boot-guard-тест.
#
# ПОЧЕМУ ФАЗА 3 ПЕРЕЕХАЛА В `dev-up`. Пока она жила только здесь, `make dev-up`
# оставлял стенд в отдельной, более слабой посадке — то есть у продукта было две
# разные посадки, и «зелёный dev» ничего не говорил про ту, которую мы поставляем.
# Норма (`security.md` §«Production-mode — ОБЯЗАТЕЛЕН ВЕЗДЕ») требует обратного:
# ЛЮБОЙ поднятый стенд несёт боевую посадку. Порядок шагов при этом не менялся —
# он ровно тот, который эта цель исполняла и раньше, просто на шаг раньше по файлу.
#
# ПРЕДУСЛОВИЕ — СЕКРЕТЫ, И ОНО ИСПОЛНЯЕТСЯ, А НЕ ОПИСЫВАЕТСЯ. На
# `kacho-iam-hook-token` ОБЯЗАТЕЛЬНОЙ ссылкой (secretKeyRef без optional) из пода
# провайдера и его automigrate-контейнера ссылается уже БАЗОВЫЙ профиль
# values.dev.yaml — с задачи #948, потому что необязательная ссылка дала бы пустую
# величину обратного вызова и полосу отвергли бы на первом обращении; профиль
# боевой посадки её лишь подтверждает. Он же приносит подписной ключ
# бутстрап-выдачи, без которого харнесс не получит НИ ОДНОГО токена (форжить ему
# больше нечем). Ни один шаблон чарта эти секреты не создаёт — их заводит только
# scripts/dev-prod-secrets.sh, и вызов стоит в рецепте `dev-up` ПЕРЕД первым
# прогоном helm.
# ГДЕ СТОИТ СТРАЖ КОНТЕКСТА, И ПОЧЕМУ НЕ В ЗАВИСИМОСТЯХ. Страж обязан стоять
# МЕЖДУ созданием кластера и действиями на нём. Записанный зависимостью рядом с
# `dev-up`, он исполнялся РАНЬШЕ создания (make проходит зависимости до рецепта, а
# при -j вообще без гарантии порядка): контекста ещё нет, kind ещё не знает
# кластера — страж отказывал, и цель не доходила до создания вовсе. То есть
# страж запрещал ровно то, что охранял. Единственная законная форма — вызов из
# рецепта: рецепт идёт после всех зависимостей, значит после создания. Место
# стража проверяется tests/helm/makefile-destructive-guarded-test.sh.
dev-prod-up: dev-up
	@set -e; \
	$(MAKE) --no-print-directory guard-kind-context; \
	$(MAKE) --no-print-directory assert-rollout-ready; \
	$(MAKE) --no-print-directory assert-production-posture; \
	$(MAKE) --no-print-directory assert-metrics-surfaces-answer; \
	$(MAKE) --no-print-directory assert-outbox-autovacuum; \
	echo "production-posture stand is UP and READY"

# ─────────────────────────────────────────────────────────────────────────────
# stack-up — раскатать ОБЪЯВЛЕННЫЙ стенд, а не набранную рукой команду.
#
# ПРЕДМЕТ. Состав стенда объявлен в единственной таблице (stacks.txt), но
# раскатывали её не все. У prod, prorobotech и a8f60d команда выкатки жила
# КОММЕНТАРИЕМ в шапке собственного профиля — то есть её набирали рукой.
# Комментарий исполнить нельзя, и проверки его не читают намеренно, поэтому
# ручка, объявленная в профиле, доезжала до кластера только если раскатывающий
# о ней вспомнит. Наблюдалось вживую: не вспомнили — страж старта отказал,
# сервис остался работать на ПРЕДЫДУЩЕМ образе, а стенд выглядел выкаченным
# полностью и не был им.
#
# Цепочка здесь НЕ ВЫПИСАНА: она читается из таблицы тем же единственным
# читателем, что и у всех остальных. Другого источника `-f` у этой цели нет,
# поэтому пройти мимо объявленного состава ею нельзя by construction.
#
# guard-destructive, а НЕ guard-kind-context: цель по замыслу исполняется на
# НАСТОЯЩЕМ кластере (prod / prorobotech / a8f60d — управляемые), и запрещать
# чужой контекст ей нельзя. Можно и нужно требовать, чтобы человек УВИДЕЛ
# названную цель — контекст, кластер, адрес apiserver'а — и подтвердил её.
#
# Цепочка читается В РЕЦЕПТЕ, а не через $(shell): раскрытие $(shell) случается
# при разборе рецепта — то есть и на отказном пути, где стенда ещё нет, — и
# печатало отказ чтения таблицы поверх собственного сообщения цели.
#
#   make -C deploy stack-up STACK=a8f60d
#   make -C deploy stack-up STACK=prod STACK_EXTRA_ARGS='-f ./helm/umbrella/values.digests.yaml'
stack-up:
	@[ -n "$(STACK)" ] || { \
	  echo "ABORT: STACK не задан — раскатывать нечего."; \
	  echo "       Умолчания здесь быть не может: цель меняет кластер, а"; \
	  echo "       «наверное, dev» — не выбор стенда."; \
	  echo "       Объявленные стенды:"; \
	  bash tests/helm/stacks.sh --names | sed 's/^/         /'; \
	  exit 2; }
	@known="$$(bash tests/helm/stacks.sh --names)"; \
	case " $$(echo $$known) " in \
	  *" $(STACK) "*) ;; \
	  *) echo "ABORT: стенда '$(STACK)' в таблице stacks.txt нет."; \
	     echo "       Состав стенда объявляет таблица; имя вне её значит, что цепочку"; \
	     echo "       взять неоткуда, а не что её можно досочинить здесь."; \
	     echo "       Объявленные стенды:"; \
	     echo "$$known" | sed 's/^/         /'; \
	     exit 2 ;; \
	esac
	@if [ "$(STACK)" = "fe3455" ]; then \
	  echo "ABORT: fe3455 раскатывается своим скриптом, а не этой целью."; \
	  echo "       Его цепочка требует слоя учётных данных площадки, который лежит"; \
	  echo "       ВНЕ дерева и в таблицу попасть не может. Эта цель добавить его не"; \
	  echo "       умеет — стенд поднялся бы без учётных данных, и это было бы видно"; \
	  echo "       не сразу."; \
	  echo "       Раскатывать:  bash helm/umbrella/cutover-fe3455.sh"; \
	  exit 2; \
	fi
	@$(MAKE) --no-print-directory guard-destructive OP="stack-up STACK=$(STACK)" TOKEN="UP-$(STACK)" \
	  WHAT="состав кластера будет приведён к объявленной цепочке стенда $(STACK) из stacks.txt"
	@set -e; \
	args="$$(bash tests/helm/stacks.sh --args "$(STACK)" ./helm/umbrella)"; \
	[ -n "$$args" ] || { \
	  echo "ABORT: цепочка стенда '$(STACK)' прочиталась ПУСТОЙ."; \
	  echo "       Это отказ чтения таблицы, а НЕ стенд без профилей: helm без единого"; \
	  echo "       -f сядет на умолчания чарта, то есть на посадку РАЗРАБОТКИ"; \
	  echo "       (анонимный доступ, plaintext БД). Почини stacks.txt."; \
	  exit 1; }; \
	echo "=== стенд $(STACK): цепочка ПРОЧИТАНА из stacks.txt ==="; \
	printf '  %s\n' $$args; \
	ctx="$$(kubectl config current-context 2>/dev/null || true)"; \
	$(MAKE) --no-print-directory module-manifests-configmap \
	  MODULE_MANIFESTS_STACK=$(STACK) STACK_NAMESPACE=$(STACK_NAMESPACE) \
	  EXPECT_CONTEXT="$$ctx"; \
	set -x; \
	helm upgrade --install $(STACK_RELEASE) ./helm/umbrella -n $(STACK_NAMESPACE) --create-namespace \
	  $$args $(UMBRELLA_OPTS) $(STACK_EXTRA_ARGS) --wait --timeout $(STACK_TIMEOUT)


# assert-rollout-ready — КАЖДЫЙ workload в ns kacho обязан быть rollout-complete.
#
# Без `|| true`, в отличие от финальных проверок dev-up: там это удобство (стенд для
# человека), здесь — гейт. Недобранный rollout ОБЯЗАН валить цель, иначе «зелёный»
# прогон означал бы ровно то, что мы и ловим — сервис не поднялся в production.
# На провале печатаем диагностику (pods + describe + логи не-Ready подов), чтобы
# красный CI был actionable, а не «timed out».
assert-rollout-ready:
	@set -e; \
	fail=0; seen=0; \
	for kind in deployment statefulset; do \
	  if ! list=$$(kubectl -n kacho get $$kind -o name 2>&1); then \
	    echo "!!! СПИСОК $$kind НЕ ПРОЧИТАН — осматривать было нечего:"; \
	    echo "$$list" | sed 's/^/    /'; \
	    echo "    Пустой ответ здесь НЕ означает «всё выкатилось»: он означает,"; \
	    echo "    что вопрос не задан (нет контекста/кластера/прав)."; \
	    exit 1; \
	  fi; \
	  for w in $$list; do \
	    seen=$$((seen+1)); \
	    echo "--- rollout: $$w"; \
	    kubectl -n kacho rollout status "$$w" --timeout=300s || fail=1; \
	  done; \
	done; \
	if [ "$$seen" -eq 0 ]; then \
	  echo "!!! В пространстве имён kacho НЕТ НИ ОДНОГО Deployment/StatefulSet."; \
	  echo "    «Ноль незавершённых выкаток» из НУЛЯ осмотренных — это не готовность."; \
	  exit 1; \
	fi; \
	if [ "$$fail" -ne 0 ]; then \
	  echo; echo "!!! ROLLOUT INCOMPLETE — production posture did NOT come up"; \
	  echo "=== pods ==="; kubectl -n kacho get pods -o wide; \
	  echo "=== not-ready pods: describe + logs ==="; \
	  for p in $$(kubectl -n kacho get pods -o json \
	      | jq -r '.items[] | select(((.status.containerStatuses // []) | length) == 0 or ((.status.containerStatuses // []) | map(.ready) | all | not)) | .metadata.name'); do \
	    echo "--- describe $$p"; kubectl -n kacho describe pod "$$p" | tail -40; \
	    echo "--- logs $$p (все контейнеры, last 60)"; \
	    kubectl -n kacho logs "$$p" --all-containers --tail=60 --prefix 2>&1 | tail -80 || true; \
	    echo "--- logs $$p (previous — boot-guard Fatalf виден именно тут)"; \
	    kubectl -n kacho logs "$$p" --all-containers --previous --tail=60 --prefix 2>&1 | tail -80 || true; \
	  done; \
	  exit 1; \
	fi; \
	echo "all workloads rollout-complete (осмотрено workload'ов: $$seen)"; \
	$(MAKE) --no-print-directory assert-jobs-healthy

# assert-jobs-healthy — периодические/разовые задания тоже часть готовности стенда.
#
# Зачем (реальный случай: `kacho-iam-jwks-rotator` падал в 100% запусков —
# CronJob звал несуществующий subcommand `kacho-iam jwks rotate`): гейт смотрел
# ТОЛЬКО на Deployment/StatefulSet, поэтому сорванное задание не красило его
# вообще никак — стенд считался готовым, а задание тихо копило по одному
# упавшему Job в день. Ровно та невидимость, ради которой эта цель и заведена.
#
# Проверяем два НЕЗАВИСИМЫХ признака; второй ловит случай, когда история упавших
# Job'ов уже вытеснена `failedJobsHistoryLimit` и по Job'ам улик не осталось:
#   1. любой Job с condition Failed=True (BackoffLimitExceeded / DeadlineExceeded);
#   2. CronJob, который УЖЕ запускался (есть lastScheduleTime), но НИ РАЗУ не
#      отработал успешно (нет lastSuccessfulTime) и сейчас не выполняется.
#
# ВАЖНО — «ещё ни разу не выполнялся» это НЕ провал. Свежесозданный CronJob
# (lastScheduleTime отсутствует) обязан быть зелёным, иначе гейт краснел бы на
# каждом только что поднятом стенде до первого тика по расписанию. Ровно так же
# `active`-джоб в полёте не считается провалом — у него нет condition Failed.
assert-jobs-healthy:
	@set -e; \
	if ! jobs_json=$$(kubectl -n kacho get jobs -o json 2>&1); then \
	  echo "!!! СПИСОК Job'ов НЕ ПРОЧИТАН — «ноль сорванных» тут значило бы «ноль осмотренных»:"; \
	  echo "$$jobs_json" | sed 's/^/    /'; exit 1; \
	fi; \
	if ! cron_json=$$(kubectl -n kacho get cronjobs -o json 2>&1); then \
	  echo "!!! СПИСОК CronJob'ов НЕ ПРОЧИТАН — «ноль сорванных» тут значило бы «ноль осмотренных»:"; \
	  echo "$$cron_json" | sed 's/^/    /'; exit 1; \
	fi; \
	failed=$$(jq -r '.items[] | select((.status.conditions // []) | map(select(.type=="Failed" and .status=="True")) | length > 0) | .metadata.name' <<<"$$jobs_json"); \
	stuck=$$(jq -r '.items[] | select(.status.lastScheduleTime != null and .status.lastSuccessfulTime == null and ((.status.active // []) | length) == 0) | .metadata.name' <<<"$$cron_json"); \
	seen=$$(jq -r '.items | length' <<<"$$jobs_json"); \
	seen_cron=$$(jq -r '.items | length' <<<"$$cron_json"); \
	if [ -n "$$failed" ] || [ -n "$$stuck" ]; then \
	  echo; echo "!!! JOBS UNHEALTHY — периодические задания сорваны"; \
	  for j in $$failed; do \
	    echo "--- Job провалился: $$j"; \
	    kubectl -n kacho get job "$$j" -o wide 2>&1 || true; \
	    kubectl -n kacho get job "$$j" -o json 2>/dev/null \
	      | jq -r '.status.conditions[]? | select(.type=="Failed") | "    reason: \(.reason)\n    message: \(.message // "-")"' || true; \
	    echo "--- logs $$j (last 40; под мог быть уже вытеснен — тогда пусто)"; \
	    kubectl -n kacho logs -l batch.kubernetes.io/job-name="$$j" --all-containers --tail=40 --prefix 2>&1 | tail -60 || true; \
	  done; \
	  for c in $$stuck; do \
	    echo "--- CronJob запускался, но НИ РАЗУ не отработал успешно: $$c"; \
	    kubectl -n kacho get cronjob "$$c" -o wide 2>&1 || true; \
	  done; \
	  exit 1; \
	fi; \
	echo "all jobs/cronjobs healthy (осмотрено Job'ов $$seen, CronJob'ов $$seen_cron; ещё не запускавшиеся — не провал)"

# assert-production-posture — стенд обязан РАБОТАТЬ в production-режиме.
#
# Реализация вынесена в scripts/assert-production-posture.sh — там же подробный
# разбор, почему прежняя inline-версия пропустила kacho-storage (2026-07-25).
# Коротко: она читала СОХРАНЁННЫЕ НАСТРОЙКИ (env подов + ConfigMap'ы), то есть
# проверяла НАМЕРЕНИЕ, а не ИСХОД, и вдобавок обходила только индивидуально
# объявленные переменные — массовый импорт `envFrom` был ей не виден вовсе.
# Сервис работал `dev`+plaintext-DB, его настройки читались `production`+`require`,
# и гейт был зелёным.
#
# Новая версия проверяет ЖИВОЙ ПРОЦЕСС: (A) самоотчёт о посадке из лога текущего
# контейнера, (B) независимое подтверждение шифрования от САМОЙ БАЗЫ
# (pg_stat_ssl), (C) сохранённые настройки по ВСЕМ путям доставки как backstop.
# Устойчива к тому, что под не перезапускался — «Ready» больше ничего не «доказывает».
#
# guard-kind-context: гейт читает логи и exec'ается в поды — на чужом (в т.ч.
# прод) контексте это как минимум утечка диагностики.
# POSTURE_PROFILE пиньтся ЗДЕСЬ, а не наследуется из окружения: иначе
# `POSTURE_PROFILE=dev make assert-production-posture` сузил бы боевой гейт до
# одного измерения и увёл бы его в зелёное — ручка, снимающая защиту, которую
# никто не проверяет. Профиль выбирает ЦЕЛЬ, а не тот, кто её зовёт.
assert-production-posture: guard-kind-context
	@POSTURE_PROFILE=production bash scripts/assert-production-posture.sh

# assert-metrics-surfaces-answer — диагностическая поверхность КАЖДОГО процесса
# отвечает, а не только объявлена.
#
# Отдельная цель, а не измерение гейта посадки: посадка отвечает на вопрос «в
# каком режиме работает процесс», а эта — на вопрос «доходят ли его величины до
# того, кто придёт смотреть». Второй вопрос от посадки не зависит и задаётся на
# любом стенде.
#
# Перечень опрашиваемых процессов ВЫВОДИТСЯ из дерева, а не выписан в скрипте:
# девятый сервис попадёт под опрос в момент появления обработчика экспозиции.
assert-metrics-surfaces-answer: guard-kind-context
	@bash scripts/assert-metrics-surfaces-answer.sh

# assert-forwarder-narrowing — тот же гейт, но по ОДНОМУ измерению, и он ходит
# на КАЖДЫЙ стенд, а не только на боевой.
#
# Зачем отдельная цель: гейт посадки целиком описывает боевой профиль, поэтому
# исполнялся только из dev-prod-up. Одно его измерение от посадки не зависит —
# круг отправителей, которым разрешено говорить за пользователя. Пустой круг
# принимает переданную личность от ЛЮБОГО пира с сертификатом внутреннего CA, а
# mTLS на dev-стенде включён. Итог: dev-профиль был единственным местом, где
# несужённый круг мог жить, и единственным, куда гейт не приходил — так kacho-nlb
# и работал на dev с пустым кругом, пока шесть его соседей были сужены.
#
# Вызывается из dev-up (и, как часть полного гейта, из dev-prod-up). Профиль
# задан ЗДЕСЬ, а не в окружении вызывающего, и боевая цель выше пинит свой
# так же явно — иначе `POSTURE_PROFILE=dev make assert-production-posture`
# сузил бы боевой гейт до одного измерения. Область выбирает ЦЕЛЬ.
# POSTURE_SKIP пробрасывается, потому что гейт посадки намеренно держит ЖЁСТКИЙ
# список ожидаемых сервисов: не развернувшийся сервис обязан быть красным, а не
# незамеченным. Стенд шарда поднимает подмножество компонентов ОСОЗНАННО, и
# осознанность выражается ровно тем способом, который гейт для этого и завёл, —
# явным исключением по имени, а не молчаливым отсутствием в кластере.
assert-forwarder-narrowing: guard-kind-context
	@POSTURE_PROFILE=dev POSTURE_SKIP="$(POSTURE_SKIP)" bash scripts/assert-production-posture.sh

# assert-outbox-autovacuum — базы с очередями обязаны РАБОТАТЬ со свежей
# статистикой, а не просто быть так описаны в values.
#
# Дренаж клеймит строки по `sent_at IS NULL`, и план для этого предиката берётся
# из последнего ANALYZE. Очередь почти всегда пуста → последний ANALYZE снят на
# пустом backlog'е → во всплеск планировщик входит с оценкой `rows=1`, отбрасывает
# частичные индексы клейма, и claim дорожает с ~0.2 мс до ~125 мс. Держат свежесть
# ДВЕ настройки: табличные autovacuum_analyze_* (миграции) и кластерный
# autovacuum_naptime (чарт) — при дефолтной минуте именно он ограничивающий, и это
# остаточный минутный всплеск в замерах «после» claim-throughput фикса.
#
# Гейт спрашивает ЖИВОЙ postmaster (`pg_settings`), а не ConfigMap: настройка,
# не доехавшая до процесса, — не настройка (см. инцидент kacho-storage 2026-07-25).
# guard-kind-context — как и у соседнего гейта: exec в поды на чужом контексте
# недопустим.
assert-outbox-autovacuum: guard-kind-context
	@bash scripts/assert-outbox-autovacuum.sh

# admin-hop-injection — ЖИВАЯ инъекция дефектов в административный переход.
#
# НЕ входит в dev-prod-up и НЕ входит в гейт посадки: шаг МУТИРУЕТ стенд и требует
# монопольного доступа. Он доказывает то, чего офлайновая самопроверка доказать не
# может — что предикаты вердикта подключены к живому стенду теми проводами, о
# которых думает автор.
#
# Главное, что он проверяет: с неработающим терминатором ВСЕ отрицательные
# наблюдения выглядят как «всё запрещено правильно». Гейт обязан на этом
# ПОКРАСНЕТЬ, а не позеленеть. Если бы правило пар было комментарием, а не кодом,
# именно этот прогон был бы зелёным над мёртвым переходом.
#
# Шаг сам заявляет себя единственным писателем (замок в namespace) и отказывается
# стартовать, если стенд занят ИЛИ уже нездоров до инъекции: чужой прогон делает
# результат НЕДЕЙСТВИТЕЛЬНЫМ, а не красным. Восстановление ПОДТВЕРЖДАЕТСЯ
# повторным полным гейтом посадки, а не заявляется.
admin-hop-injection: guard-kind-context
	@bash scripts/inject-admin-hop-defects.sh

helm-lint:
	bash scripts/helm-umbrella-deps.sh
	cd helm/umbrella && helm lint -f values.dev.yaml

# ─────────────────────────────────────────────────────────────────────────────
# ПИНЫ ОБРАЗОВ ПРОФИЛЯ УПРАВЛЯЕМОГО КЛАСТЕРА — ПОРОЖДАЮТСЯ, А НЕ ПИШУТСЯ РУКОЙ.
#
# Профиль объявлял себя порождаемым, а порождающего в дереве не было ни одного:
# указание «правится порождающий» без координаты правку не переадресует, а
# отменяет. Теперь координата есть, и она исполняемая.
#
#   make managed-image-pins REF=<коммит main>   — переписать пины от коммита
#   make managed-image-pins-check               — пины сходятся с записью профиля
#
# REF по умолчанию HEAD. Порождающий ОТКАЗЫВАЕТ, если коммит не входит в ветку,
# чьё имя несёт тег: образ с таким тегом публикует сборка этой ветки, и для
# стороннего коммита его нет — отказ здесь дешевле, чем ImagePullBackOff через
# минуты после успешного «helm upgrade».
#
# PROFILE — какой профиль порождать/сверять. Умолчание — профиль посадки на
# управляемый кластер; остальные профили с пинами называются явно, потому что
# переписать чужой стенд по невнимательности дороже, чем набрать путь.
REF ?= HEAD
PROFILE ?= helm/umbrella/values.a8f60d.yaml

managed-image-pins:
	@bash scripts/gen-managed-image-pins.sh --profile $(PROFILE) --ref $(REF)

managed-image-pins-check:
	@bash scripts/gen-managed-image-pins.sh --profile $(PROFILE) --check

# Offline helm manifest-assertion suite (tests/helm/). Asserts kept-stack wiring
# against the rendered umbrella — e.g. hydra-jwks-url-test.sh checks that
# api-gateway points at the in-cluster Hydra JWKS endpoint. No kind cluster needed.
#
# ЦЕЛЬ ВЫЗЫВАЕТСЯ ИЗ CI (.github/workflows/ci.yaml, job `helm`). До этого её не
# звал никто — ни один workflow, ни одна другая цель: полтора десятка проверок и
# около двух тысяч строк не исполнялись НИ РАЗУ. Прогон «как есть» это подтвердил
# — две проверки утверждали неправду (путь чарта шлюза от прежнего polyrepo;
# небезопасное умолчание authMode у geo, которое чарт с тех пор осознанно
# сменил на production).
#
# ТРИ ВЕЩИ ДЕЛАЕТ САМА ЦЕЛЬ, а не «предполагает у запускающего».
#
# 1. Собирает зависимости чарта. Проверки рендерят умбреллу, а её `charts/*.tgz`
#    в git не лежат (deploy/.gitignore). На свежем checkout'е рендер падал у
#    всех, кроме одной проверки, которая материализовала их сама — то есть
#    результат зависел от АЛФАВИТНОГО ПОРЯДКА имён файлов. Собираем один раз,
#    заранее, для всех — через ЕДИНСТВЕННОГО владельца
#    (scripts/helm-umbrella-deps.sh). «Один раз» стало правдой только с ним:
#    прежде подкачку звали шесть мест в дереве, за прогон job'а она уходила в
#    сеть до десяти раз, и отказ ЛЮБОГО из походов красил весь прогон.
#
# 2. Проверяет, что `yq` — тот самый. В PATH бывают две разные программы с этим
#    именем: mikefarah yq (v4, YAML) и python-yq (обёртка над jq). Фильтры
#    проверок написаны под первую; вторая на них молча отдаёт ПУСТО, и
#    утверждения проходят, ничего не сверив. Одна проверка это ловила сама и
#    отказывалась работать — остальные тринадцать нет. Отказ здесь общий.
#
# 3. Прогоняет ВСЕ и печатает сводку. Прежний `|| exit 1` обрывался на первой
#    же красной, и правка вслепую превращалась в серию прогонов по одной находке.
#
# gate-self-test — самопроверки гейтов ОБЯЗАНЫ исполняться.
#
# Самопроверка (`--self-test`) — это доказательство, что гейт умеет краснеть на
# внесённом дефекте и молчать на законной конструкции той же формы. Написаны они
# были, а не вызывались ниоткуда: цель ниже запускала скрипты БЕЗ аргументов,
# в workflow'ах их не было вовсе. Доказательство существовало как текст.
#
# Скрипт не ведёт список руками: он находит самопроверки по разбору аргумента и
# требует совпадения найденного с объявленным в ОБЕ стороны — забытая новая
# самопроверка и запись без предмета одинаково роняют прогон.
#
# --verify-discovery — ДОКАЗАТЕЛЬСТВО САМОГО ОБХОДА: что предикат читает разбор
# аргумента, а не слово в прозе; что находит КАЖДУЮ законную форму разбора в
# любом каталоге; что не находит сам себя; и что расхождение состава видно в ОБЕ
# стороны. Сколько форм законно — знает САМ обход (SELFTEST_PARSE в
# scripts/run-gate-self-tests.sh) и печатает числом; здесь это число не
# дублируется: копия отставала от него молча и уже отстала однажды. Он был написан и не вызывался ниоткуда — то есть гейт над гейтами
# доказывал всё, кроме себя. Идёт ПЕРВЫМ: пустой или ослепший обход делает
# зелёный основного прохода бессмысленным.
gate-self-test:
	@bash scripts/run-gate-self-tests.sh --verify-discovery
	@bash scripts/run-gate-self-tests.sh

# injection-proofs — доказательства инъекцией ОТДЕЛЬНЫМ ФАЙЛОМ обязаны исполняться.
#
# Вторая форма того же доказательства: у `gate-self-test` предмет — ветка
# `--self-test` внутри гейта, здесь — скрипт `<тема>-inject.sh` рядом с ним. Форм
# две, обходчика два, норма одна; сводить их в один предикат нельзя — признаки
# разные (разбор аргумента против формы имени) и запуск разный.
#
# Их не звал НИКТО: десять скриптов доказательств лежали в дереве и не
# встречались ни в одной джобе конвейера. Доказательство существовало как текст —
# ровно то, ради борьбы с чем оно и пишется. Класс наблюдался дважды за сутки
# (гейт зелёный и лживый; гейт, верный лишь из-за узости популяции), и оба раза
# нашёлся потому, что инъекцию прогнали руками.
#
# Перечень ВЫВОДИТСЯ из дерева (git ls-files), а не выписывается: выписанный
# оставил бы следующее заведённое доказательство вне вызова. Соглашение об
# именовании держится с двух сторон — зеркальная перепись требует, чтобы файл со
# словом «inject» в имени был опознан явно.
injection-proofs:
	@bash scripts/run-injection-proofs.sh --self-test
	@bash scripts/run-injection-proofs.sh

# alt-fixture-discipline — «другая» фикстура обязана БЫТЬ другой и иметь
# производителя.
#
# Значение, неотличимое от своей пары, превращает проверку «чужое отвергнуто» в
# проверку «своё принято»: продукт о запрещённом сочетании не спрашивают вовсе,
# а кейс зеленеет. Гейт лежал в дереве и запускался только собственной
# самопроверкой, при этом кейс nlb в комментарии ссылался на него как на
# действующую защиту.
alt-fixture-discipline:
	@python3 scripts/assert-alt-fixtures-are-another.py

# fixture-role-verbs — роль, которую заводит фикстура, построена из глаголов, которые
# модель прав ОБЪЯВЛЯЕТ.
#
# Сегмент глагола в Role.Create словарём не закрыт: роль с несуществующим глаголом
# создаётся успешно, а отношения `v_*` за таким глаголом реконсайлер пропускает —
# молча. Положительная половина кейса («субъект МОЖЕТ») тогда не может пройти ни при
# каком исправном продукте, а роль, которую никто не использует, не краснеет вовсе.
# Замерено 2026-08-03 на 4d025d08: 10 определений из 58 в 91 файле фикстур несли
# глагол вне модели — девять `read` и одно `start`/`stop`, снятое миграцией 0059.
fixture-role-verbs:
	@python3 scripts/assert-fixture-role-verbs-exist.py

# generated-scripts-parse — каждый скрипт сгенерированной коллекции обязан РАЗБИРАТЬСЯ.
#
# Шаг, чей скрипт не разбирается, не падает — он НЕ ИСПОЛНЯЕТСЯ, и newman считает это
# в канале, отдельном от `assertions.failed`. Поэтому сводка суиты может показывать
# «0 упавших» ровно там, где проверки не выполнялись вовсе. Замерено 2026-08-03: 10
# скриптов vpc не разбирались (заголовок с апострофом вклеивался в одинарные кавычки),
# и вдобавок 4 утверждения краснели на ЧУЖОЙ операции — снятый вместе со скриптом
# `pm.environment.unset('opId')` оставлял поллеру идентификатор от предыдущего шага.
#
# Гейт разбирает НАСТОЯЩИМ парсером, а не выражением. Первая перепись этого дефекта
# бралась регулярным выражением и дала ~100 задетых коллекций в семи сервисах; верное
# число — 10 в одном. Выражение не отличает сломанный литерал от корректно
# экранированного и не имеет контроля ни в одну сторону.
generated-scripts-parse:
	@node scripts/assert-generated-scripts-parse.js \
	  ../services/*/tests/newman/collections/*.json \
	  ../gateway/tests/newman/collections/*.json

# delete-step-discipline — шаг удаления обязан НЕСТИ УТВЕРЖДЕНИЕ.
#
# Замерено на дереве: 82 коллекции, 8233 шага, 1359 из них DELETE — и 457 без
# единого утверждения. Такой шаг читает 200, 403 и 500 одинаково и зеленеет на
# каждом. Тихим это не остаётся: у асинхронного удаления шаг захватывает `opId`
# из тела, а следующий опрашивает операцию по нему; отказ тела не несёт, `opId`
# остаётся ОТ ПРЕДЫДУЩЕЙ операции, и опрос подтверждает чужой завершённый успех.
# Кейс зеленеет по операции, которую не запускал, а ресурс живёт — фикстура
# течёт, ограниченный пул деградирует, списочные контракты плывут.
#
# Чинится в ГЕНЕРАТОРЕ (`step_to_postman` ставит утверждение по умолчанию), а не
# правкой коллекции: коллекции регенерируются побайтово.
delete-step-discipline:
	@python3 scripts/assert-delete-steps-are-asserted.py

# waiter-discipline — ожидание и остановка идут по ЗАХВАЧЕННОМУ идентификатору
# процесса, а не по шаблону командной строки.
#
# Поиск по шаблону сопоставляет его и с командной строкой ТОГО, КТО СПРАШИВАЕТ:
# сторожевая петля не завершается никогда (ждущий находит сам себя), а остановка
# по шаблону сносит ждущего вместе с ожидаемым. За один день класс сработал
# четыре раза у разных исполнителей и каждый раз выглядел как «висит непонятно
# почему» — то есть маскировался под нагрузку.
#
# Гейт превентивный: живых вхождений в дереве НЕТ (0 находок из 230 прочитанных
# файлов), и знание о классе не было записано нигде — поэтому каждый следующий
# писал ту же конструкцию с нуля. Перепись осмотренного печатается отдельно:
# «ноль находок» обязано отличаться от «ноль прочитанного».
waiter-discipline:
	@bash scripts/assert-waiters-name-their-target.sh

# report-reader-discipline — вердикт о прогоне берётся из СВОДКИ отчёта, а не из
# массива исполнений.
#
# Массив расходится со сводкой в обе стороны, и это измерено на боевом прогоне
# 2026-07-30: шаг, снявший собственный запрос из предзапросного скрипта, записи
# в массиве не получает вовсе (его утверждения при этом в сводке есть), а шаг,
# повторённый самопетлёй, получает несколько записей с ОДНИМ И ТЕМ ЖЕ набором
# утверждений. Инструмент, считающий по массиву, тихо занижает — а занижение
# выглядит как улучшение и потому само себя не выдаёт.
#
# Перепись осмотренного печатается, и отсутствие читателей ВООБЩЕ — находка, а
# не чистота: у запрета должен остаться предмет.
report-reader-discipline:
	@python3 scripts/assert-report-readers-use-the-summary.py

# posture-branch-discipline — ветвь на посадке посева обязана быть способной уйти
# в обе стороны.
#
# `tests/authz-fixtures/setup.sh` оставляет стоять РОВНО ОДНО значение посадки
# (`production`); `dev` отвергается, прочее не опознано, обе ветви завершают
# процесс. Два прогонщика newman тем не менее ветвились на прочитанном значении и
# падали в запасное `dev` — посадку, которую сам классификатор отвергает. Условие
# было ложно ВСЕГДА, обоими путями сразу (посев прошёл ⇒ в файле `production`;
# посев сорвался ⇒ прогон прекращён раньше чтения), и оба блока досева были
# недостижимы. Ровно тот класс, который осуждало сообщение коммита, их принёсшего.
#
# Гейт сам ИСТЕКАЕТ: появится второе производимое значение — запрет на сравнение
# замолкает, потому что ветвление станет осмысленным. Предпосылка проверяется:
# разбор, не нашедший ни одного производимого значения, — отказ, а не «чисто».
posture-branch-discipline:
	@python3 scripts/assert-posture-branches-can-be-taken.py

# verdict-aggregator-discipline — вердикт каждой суиты берётся из ЧИСЕЛ отчёта.
#
# Класс «прогонщик печатает GREEN при упавших проверках» в этом дереве уже ловили
# и чинили, но доказан фикс был у ОДНОГО агрегатора из восьми: инъекционная
# самопроверка есть только у vpc, а подгрузить функцию можно было у трёх — у
# остальных нет guard'а `BASH_SOURCE[0] == $0`. Семь копий держались на прочтении
# диффа. Гейт извлекает функцию из КАЖДОГО прогонщика и ИСПОЛНЯЕТ её на
# синтетических отчётах: чистый обязан дать нулевой вердикт, а упавшее
# утверждение / запрос без ответа / немой отчёт / отсутствующий отчёт /
# ненулевой код newman — ненулевой. Первый же прогон нашёл живой экземпляр в
# services/nlb/.../run-incremental.sh (не читались UNANSWERED и немой отчёт).
verdict-aggregator-discipline:
	@bash scripts/assert-verdict-aggregators-honest.sh

# ceremony-declaration-discipline — у каждой записи объявления церемонии есть предмет.
#
# Набор коллекций, которым нужен ЧЕЛОВЕЧЕСКИЙ вызывающий, выводится из дерева
# единственным объявлением (tests/authz-fixtures/ceremony_credentials.py), а не
# выписывается: рукописный перечень в этом репозитории уже расходился с деревом.
# Гейт требует, чтобы каждая запись объявления что-то отбирала, — запись, которой
# больше нечего отбирать, наследуется следующим читателем как действующее
# ограничение. Ноль прочитанных коллекций — отказ, а не «чисто».
ceremony-declaration-discipline:
	@python3 ../tests/authz-fixtures/ceremony_credentials.py --root .. --verify

# dialled-transport-discipline — у каждого адреса, который набирает волна, есть
# ПРОИЗВОДИТЕЛЬ в прогонщике.
#
# Волна церемонии включалась фактом о дереве, печатала свою строку и выводила
# набор коллекций — а её посев набирал четыре адреса, которых прогонщик не
# создавал. Проходило это ровно пока пробросы держал человек руками; на чистой
# машине волна не могла пройти НИ РАЗУ, и при этом всё в ней читалось как
# работающее. Тот же класс, что «гейт, у входа которого нет производителя», —
# только предметом здесь транспорт, а не поле запроса.
#
# Гейт идёт по модулям, ДОСТИЖИМЫМ из прогонщика, и требует, чтобы порт каждого
# адреса-умолчания либо пробрасывался, либо передавался прогонщиком явно. Адрес,
# который не набирают, а СРАВНИВАЮТ (объявленная издателем идентичность),
# объявлен отдельно и с причиной; запись, которой больше нечего исключать, — сама
# находка. Ноль прочитанных модулей и ноль вычитанных пробросов — отказ, а не
# «чисто».
dialled-transport-discipline:
	@python3 scripts/assert-dialled-transports-have-a-producer.py --root ..

# Локально: `make helm-manifest-test`; одну проверку — `bash tests/helm/<имя>.sh`.
#
# gate-self-test идёт ПЕРВЫМ и это осмысленный порядок, а не оформление: зелёный
# обычный проход гейта, который не доказал, что умеет краснеть, ничего не значит.
helm-manifest-test: gate-self-test
	@set -e; \
	if ! command -v yq >/dev/null 2>&1; then \
	  echo "ERROR: нужен yq (mikefarah v4) — фильтры проверок написаны под него."; \
	  echo "       https://github.com/mikefarah/yq/releases"; exit 1; \
	fi; \
	if ! grep -qE 'mikefarah|version v?4' <<<"$$(yq --version 2>&1)"; then \
	  echo "ERROR: в PATH не тот yq: $$(command -v yq) → $$(yq --version 2>&1)"; \
	  echo "       Нужен mikefarah yq v4. python-yq (обёртка над jq) на этих"; \
	  echo "       фильтрах молча отдаёт ПУСТО — утверждения пройдут, ничего не сверив."; \
	  exit 1; \
	fi; \
	echo "=== зависимости умбреллы (проверки рендерят её; charts/*.tgz не в git) ==="; \
	bash scripts/helm-umbrella-deps.sh; \
	rm -rf helm/umbrella/tmpcharts-*; \
	failed=""; unmet=""; total=0; \
	for t in tests/helm/*-test.sh; do \
	  total=$$((total+1)); \
	  echo "=== $$t ==="; \
	  bash "$$t" && rc=0 || rc=$$?; \
	  case "$$rc" in \
	    0) ;; \
	    2) unmet="$$unmet $$(basename $$t)" ;; \
	    *) failed="$$failed $$(basename $$t)" ;; \
	  esac; \
	done; \
	rm -rf helm/umbrella/tmpcharts-*; \
	if [ -n "$$unmet" ]; then \
	  echo; echo "!!! helm-manifest-test: УСЛОВИЕ НЕ СОЗДАНО$$unmet"; \
	  echo "    Код 2 — не вердикт о дереве (tests/helm/README.md §«Три исхода»):"; \
	  echo "    нет инструмента, не собраны зависимости умбреллы, нет профиля на диске."; \
	  echo "    В зачёт «прошло» это не идёт и находкой не является."; \
	fi; \
	if [ -n "$$failed" ]; then \
	  echo; echo "!!! helm-manifest-test: провалено$$failed (из $$total)"; exit 1; \
	fi; \
	if [ -n "$$unmet" ]; then exit 2; fi; \
	echo; echo "helm-manifest-test: all green ($$total проверок)"

# guard-kind-context: цель делает `kind load` (пинится к kind по имени) И
# `kubectl rollout restart` (едет в АКТИВНЫЙ контекст). Второе от первого не
# защищено: при живом kind-кластере и подменённом kubeconfig загрузка образа
# проходит, а перезапуск прилетает соседнему кластеру — тот же near-miss, что
# и у dev-up, только тише.
reload-svc: guard-kind-context
ifndef SVC
	$(error SVC variable is required, e.g. make reload-svc SVC=compute)
endif
	@if [ "$(SVC)" != "vpc" ] && [ "$(SVC)" != "compute" ] && [ "$(SVC)" != "storage" ] && [ "$(SVC)" != "loadbalancer" ] && [ "$(SVC)" != "api-gateway" ] && [ "$(SVC)" != "iam" ] && [ "$(SVC)" != "ui" ] && [ "$(SVC)" != "nlb" ]; then \
		echo "ERROR: unknown service '$(SVC)'"; exit 1; \
	fi; \
	DEPLOY_NAME=$(SVC); \
	if [ "$(SVC)" = "iam" ]; then DEPLOY_NAME=kacho-iam; fi; \
	if [ "$(SVC)" = "ui" ]; then DEPLOY_NAME=ui; fi; \
	if [ "$(SVC)" = "nlb" ]; then DEPLOY_NAME=kacho-nlb; fi; \
	if [ "$(SVC)" = "storage" ]; then DEPLOY_NAME=kacho-storage; fi; \
	if ! kubectl -n kacho get deploy $$DEPLOY_NAME >/dev/null 2>&1; then \
		echo "WARN: service '$(SVC)' (deployment '$$DEPLOY_NAME') is not deployed yet (planned for sub-phase 0.X — see roadmap)"; \
		exit 0; \
	fi; \
	if [ "$(SVC)" = "ui" ]; then \
		: ; \
	else \
		d=$$(test "$(SVC)" = api-gateway && echo gateway || echo services/$(SVC)); \
		( cd .. && docker build -f $$d/Dockerfile $(IMAGE_BUILD_ARGS) -t kacho-$(SVC):dev . ); \
	fi && \
	kind load docker-image kacho-$(SVC):dev --name $(CLUSTER_NAME) && \
	kubectl rollout restart -n kacho deployment/$$DEPLOY_NAME

logs-svc:
ifndef SVC
	$(error SVC variable is required)
endif
	kubectl logs -n kacho -f deploy/$(SVC)

# guard-kind-context: интерактивная psql-сессия — не чтение, а произвольная запись
# в базу активного контекста; учётные данные здесь стендовые.
psql: guard-kind-context
ifndef SVC
	$(error SVC variable is required)
endif
	kubectl exec -it -n kacho statefulset/pg-$(SVC) -- psql -U $(SVC) -d kacho_$(SVC)

e2e-test:
	@for sh in e2e/0.1/*.sh; do \
		echo "=== $$sh ==="; \
		bash "$$sh" || exit 1; \
	done

# e2e-newman — REPRODUCIBLE newman e2e: port-forward + seed authz fixtures
# (non-expiring dev JWTs, users, projects, cluster-admin, patched newman env) +
# run a service's newman suite against the running dev stand. Not a manual
# side-step — one command, deterministic. Requires: dev-up complete; kubectl,
# python3, newman, grpcurl in PATH.
#   make e2e-newman SVC=vpc
#   make e2e-newman SVC=vpc COLLECTION=internal-network
# guard-kind-context: харнесс сеет фикстуры (аккаунты, проекты, гранты) через
# port-forward в активный контекст — это запись, а не прогон в вакууме.
e2e-newman: guard-kind-context
	@bash ./scripts/newman-e2e.sh "$(SVC)" "$(COLLECTION)"

# ─── Load testing ────────────────────────────────────────────────

# guard-kind-context: цель СОЗДАЁТ в кластере Job, который льёт нагрузку на
# аллокацию адресов. На чужом кластере это нагрузочный тест по чужому продакшену.
loadtest-address-allocate: guard-kind-context
	@kubectl -n kacho delete job k6-address-allocate --ignore-not-found
	@kubectl -n kacho apply -f load-tests/k6-address-allocate.yaml
	@echo "→ Job created, waiting for completion (max 600s)…"
	@kubectl -n kacho wait --for=condition=complete job/k6-address-allocate --timeout=600s || \
	  kubectl -n kacho wait --for=condition=failed job/k6-address-allocate --timeout=10s
	@kubectl -n kacho logs -l job-name=k6-address-allocate --tail=-1

loadtest-address-allocate-clean: guard-kind-context
	@kubectl -n kacho delete job k6-address-allocate --ignore-not-found
	@kubectl -n kacho delete cm k6-address-allocate --ignore-not-found

# ─── KAC-105: IAM stack (kacho-iam; KAC-127: Zitadel → Ory) ──────────────────────

# alias для reload-svc SVC=iam — пересобрать и перезагрузить kacho-iam.
reload-svc-iam:
	@$(MAKE) reload-svc SVC=iam

# psql в kacho_iam-БД (база, пользователь, схема — все kacho_iam).
psql-iam: guard-kind-context
	kubectl exec -it -n kacho statefulset/kacho-umbrella-pg-iam -- psql -U iam -d kacho_iam

# KAC-125: greenfield wipe kacho_iam schema + drop goose state.
# Используется ДО helm upgrade с breaking migration (e.g. 0009_user_per_account_invite_kac125).
# Воспроизводимый replacement для manual `DROP SCHEMA … CASCADE`.
# ВНИМАНИЕ: уничтожает все данные IAM (accounts/projects/users/...). Только для dev.
#
# ДВА ГЕЙТА, И ОБА ОБЯЗАТЕЛЬНЫ.
#
# guard-kind-context — единственная разрушающая цель файла была ЕДИНСТВЕННОЙ, кто
# от него не зависел, при том что от него зависят все ЧИТАЮЩИЕ гейты рядом
# (assert-production-posture, assert-forwarder-narrowing, assert-outbox-autovacuum).
# Асимметрия ровно наоборот: имена namespace, пода и базы на управляемом кластере
# те же самые, поэтому промах контекстом здесь стоит не утечки диагностики, а
# безвозвратного сноса аккаунтов, проектов, пользователей и привязок.
#
# guard-destructive — подтверждение вводом, как у spire-rotate-trust-domain (та
# ничего не рушит безвозвратно, а подтверждение требует). И оно НАЗЫВАЕТ ЦЕЛЬ:
# прежняя строка печатала значение KUBECONFIG, пустое при дефолтном ~/.kube/config,
# то есть предупреждала, не сказав о чём.
wipe-iam-db: guard-kind-context
	@$(MAKE) --no-print-directory guard-destructive OP=wipe-iam-db TOKEN=WIPE-IAM \
	  WHAT="DROP SCHEMA kacho_iam CASCADE + снос состояния goose — все аккаунты, проекты, пользователи, группы, роли и привязки исчезнут безвозвратно"
	@kubectl exec -n kacho kacho-umbrella-pg-iam-0 -- \
	    env PGPASSWORD=dev-iam-password psql -U iam -d kacho_iam \
	    -c "DROP SCHEMA IF EXISTS kacho_iam CASCADE; CREATE SCHEMA kacho_iam;"
	@kubectl exec -n kacho kacho-umbrella-pg-iam-0 -- \
	    env PGPASSWORD=dev-iam-password psql -U iam -d kacho_iam \
	    -c "DROP TABLE IF EXISTS public.goose_db_version;" || true
	@echo "✓ IAM schema wiped; goose state dropped. Next: kubectl rollout restart deploy/kacho-iam"

# ─── МАНИФЕСТЫ МОДУЛЕЙ: ПРОИЗВОДИТЕЛЬ ConfigMap (kacho#1901) ─────────────────
#
# Доставка манифестов была заведена целиком (#1875) — чарт монтирует именованный
# ConfigMap, процесс читает смонтированный каталог, страж старта отказывает на
# сорванной доставке, — а НАПОЛНИТЬ этот ConfigMap было нечем: ни цели сборки, ни
# шага стенда, ни объявления внешнего конвейера. Здесь эта половина и заводится.
#
# ИМЯ ОБЪЕКТА ЧИТАЕТСЯ ИЗ ТЕХ ЖЕ ПРОФИЛЕЙ, ЧТО И У helm. Производитель получает
# цепочку стенда из stacks.txt и берёт `kacho-iam.manifests.configMapName`
# оттуда же, откуда её берёт чарт: второе объявление разошлось бы с первым молча,
# и каталог доставки приехал бы пустым — снаружи неотличимо от «модулей нет».
#
# ПЕРЕЧЕНЬ МАНИФЕСТОВ ВЫВОДИТСЯ ИЗ ДЕРЕВА (`services/*/manifest.yaml`), а служба
# БЕЗ манифеста называется переписью поимённо: доставка пяти манифестов из шести
# выглядит снаружи ровно так же, как доставка всех.
#
# ИСХОДОВ ЧЕТЫРЕ, И КОД ЧИТАЕТСЯ, А НЕ ВИД ВЫВОДА: 0 — объект применён; 3 —
# стенд доставку не объявляет (законный исход, идём дальше); 1 и 2 — находка и
# беспредметный обход, и оба обязаны валить цель. Инструмент СОБИРАЕТСЯ в
# двоичный файл, а не зовётся через `go run`: `go run` отдаёт свой код 1 на любом
# ненулевом коде программы, то есть «не объявлено» стало бы неотличимо от находки.
# СТРАЖ ПРИНАДЛЕЖИТ ВЫЗЫВАЮЩЕМУ, А НЕ ЭТОЙ ЦЕЛИ (kacho#1901).
#
# Здесь стояла предпосылка `guard-kind-context` — аварийный стоп, отвергающий
# всякий контекст, кроме нашего kind-кластера. Она делала цель НЕИСПОЛНИМОЙ на
# двух путях выкатки из трёх: `stack-up` по замыслу работает на настоящем
# кластере (прод / prorobotech / a8f60d — рецепт прямо это объявляет и потому
# гейтится `guard-destructive`), а `helm/umbrella/cutover-fe3455.sh` поднимает
# боевую площадку. Оба зовут производителя ПЕРЕД helm — и оба получали отказ до
# единой его команды.
#
# Цена измерена, а не предположена (подставной kubectl, настоящий рецепт):
# контекст `fe3455-prod` → «ABORT: активный kube-контекст … НЕ kind-kacho», код 2;
# контекст `kind-kacho` → «ConfigMap "kacho-module-manifests", ключей 6», код 0.
# То есть на боевом пути доставка не заводилась НИКОГДА, а `values.prod.yaml`
# объявляет `manifests.required: true` — служба на такой посадке не поднимается.
#
# Каждый путь выкатки уже пинит себя САМ, и стражем, подходящим ЕГО стенду:
# `dev-up` зовёт `guard-kind-context` первой же строкой рецепта; `stack-up` —
# `guard-destructive` (человек видит контекст, кластер, адрес apiserver'а и
# подтверждает их); cutover-скрипт сверяет активный apiserver с адресом площадки.
# Второй страж внутри этой цели ничего к ним не добавил бы, а один из них —
# kind-only — отменял бы два остальных.
#
# ВЗАМЕН СНЯТОГО СТРАЖА: СТЕНД ОБЯЗАН БЫТЬ НАЗВАН. Умолчания `?= dev` здесь быть
# не может — цель ПРИМЕНЯЕТ объект в кластер активного контекста, и «наверное,
# dev» не есть выбор стенда: прямой вызов без аргумента положил бы объекты стенда
# разработки в тот кластер, куда сейчас смотрит kubectl. Тот же довод, по которому
# `stack-up` требует `STACK`. Все три пути выкатки называют стенд явно, поэтому
# снятие умолчания не трогает ни одного из них.
module-manifests-configmap: guard-declared-context
	@[ -n "$(MODULE_MANIFESTS_STACK)" ] || { \
	  echo "ABORT: MODULE_MANIFESTS_STACK не задан — производить нечего и некуда."; \
	  echo "       Умолчания здесь быть не может: цель применяет объект в кластер"; \
	  echo "       активного контекста, а «наверное, dev» — не выбор стенда."; \
	  echo "       Объявленные стенды:"; \
	  bash tests/helm/stacks.sh --names | sed 's/^/         /'; \
	  exit 2; }
	@set -e; \
	chain="$$(bash tests/helm/stacks.sh --chain $(MODULE_MANIFESTS_STACK) ' ')"; \
	[ -n "$$chain" ] || { echo "ABORT: цепочка стенда '$(MODULE_MANIFESTS_STACK)' прочиталась пустой"; exit 1; }; \
	profiles=""; for f in $$chain; do profiles="$$profiles ./helm/umbrella/$$f"; done; \
	work="$$(mktemp -d)"; trap 'rm -rf "$$work"' EXIT; \
	go build -o "$$work/producer" ../tools/modulemanifests/cmd/module-manifests-configmap; \
	rc=0; "$$work/producer" -root .. $$profiles > "$$work/configmap.yaml" || rc=$$?; \
	case "$$rc" in \
	  0) kubectl -n $(STACK_NAMESPACE) apply -f "$$work/configmap.yaml"; \
	     echo "=== манифесты модулей доставлены стенду $(MODULE_MANIFESTS_STACK) ===" ;; \
	  3) echo "=== стенд $(MODULE_MANIFESTS_STACK) доставку манифестов не объявляет — ConfigMap не заводится ===" ;; \
	  *) echo "ABORT: производитель манифестов вернул $$rc — см. перепись выше"; exit "$$rc" ;; \
	esac


# Логи kacho-iam deployment.
logs-iam:
	kubectl logs -n kacho -f deploy/kacho-iam

# KAC-127: zitadel-admin target удалён — Zitadel заменён на Ory Kratos + Hydra.

# Копия канонической МОДЕЛИ прав, вшиваемая в двоичный файл iam: из неё
# реляционная форма вердикта выводит отношения (XC-12). Модель остаётся
# источником истины ФОРМЫ и после снятия внешнего движка (S6 эпика #747) —
# снято то, что требовало ЖИВОГО движка, а не то, что называет модель.
# Держится порождением плюс гейтом побайтового равенства
# (services/iam/internal/authzmodel/identity_test.go).
OPENFGA_CANONICAL_FGA ?= ../proto/kacho/cloud/iam/v1/fga_model.fga
FGA_MODEL_EMBED ?= ../services/iam/internal/authzmodel/fga_model.fga
fga-model-embed:
	@cp $(OPENFGA_CANONICAL_FGA) $(FGA_MODEL_EMBED)
	@echo "fga-model-embed: $(FGA_MODEL_EMBED) обновлён из $(OPENFGA_CANONICAL_FGA)"

# ─── KAC-127 Phase 10 — SPIRE / Cilium mesh / cosign ─────────────────────
# Targets для bootstrap, dry-run проверки, и emergency operations.

# Bootstrap SPIRE Server + Agent + регистрация всех Kachō SPIFFE-IDs.
# Применяется только когда umbrella values имеет spire-server.enabled=true.
#
# guard-destructive, а НЕ guard-kind-context: цель по замыслу исполняется и на
# настоящем кластере (values.prod.yaml прямо в рецепте), поэтому запрещать чужой
# контекст нельзя — можно и нужно требовать, чтобы человек увидел названную цель
# и подтвердил её. Создание пространства имён и применение манифестов PKI в чужой
# кластер — не то, что делают по опечатке в KUBECONFIG.
spire-bootstrap:
	@$(MAKE) --no-print-directory guard-destructive OP=spire-bootstrap TOKEN=BOOTSTRAP \
	  WHAT="создание namespace spire-system и применение манифестов SPIRE Server/Agent (корень доверия mesh)"
	@echo "Phase 10: bootstrapping SPIRE Server + Agent..."
	@kubectl create namespace spire-system --dry-run=client -o yaml | kubectl apply -f -
	@helm template kacho-umbrella ./helm/umbrella -f ./helm/umbrella/values.prod.yaml \
		--set spire-server.enabled=true \
		--set spire-agent.enabled=true \
		--show-only charts/spire-server/templates/* \
		--show-only charts/spire-agent/templates/* | \
		kubectl apply -n spire-system -f -
	@echo "Waiting for SPIRE Server replicas to be ready..."
	@kubectl -n spire-system rollout status statefulset/kacho-umbrella-spire-server --timeout=600s
	@kubectl -n spire-system rollout status daemonset/kacho-umbrella-spire-agent --timeout=600s
	@echo "✓ SPIRE bootstrap complete. Registration entries applied via post-install Job."

# Rotate trust domain — emergency runbook operation (P10-D19).
# Invalidates entire trust domain key material.
#
# Подтверждение здесь было и раньше — но оно не называло, КУДА поедет операция.
# Теперь его выдаёт guard-destructive, который разрешает контекст/кластер/apiserver
# живым kubectl и печатает их. guard-kind-context намеренно НЕ ставится: аварийная
# ротация по замыслу исполняется на настоящем кластере.
spire-rotate-trust-domain:
	@$(MAKE) --no-print-directory guard-destructive OP=spire-rotate-trust-domain TOKEN=ROTATE \
	  WHAT="перезапуск SPIRE Server — весь материал корня доверия аннулируется, все выданные SVID перестают быть действительными"
	@kubectl -n spire-system delete pod -l app.kubernetes.io/name=spire-server
	@echo "✓ SPIRE Server pods restarting; new root CA will be issued."

# Dry-run Cilium policies — render и валидация без apply.
cilium-policy-dry-run:
	@echo "Phase 10: rendering CiliumNetworkPolicy manifests..."
	@helm template kacho-umbrella ./helm/umbrella -f ./helm/umbrella/values.prod.yaml \
		--set cilium.enabled=true \
		--show-only charts/cilium/templates/network-policies.yaml \
		--show-only charts/cilium/templates/cilium-mtls-enforce.yaml > /tmp/cilium-policies.yaml
	@echo "✓ Rendered to /tmp/cilium-policies.yaml ($$(wc -l < /tmp/cilium-policies.yaml) lines)"
	@command -v kubeconform > /dev/null && \
		kubeconform -skip CiliumNetworkPolicy,CiliumClusterwideNetworkPolicy /tmp/cilium-policies.yaml \
		|| echo "(kubeconform not installed; install via: brew install kubeconform)"

# Lint все Phase 10 charts.
spire-lint:
	helm lint ./helm/umbrella/charts/spire-server
	helm lint ./helm/umbrella/charts/spire-agent
	helm lint ./helm/umbrella/charts/cilium
	helm lint ./helm/umbrella/charts/cosign-policy-controller
	helm lint ./helm/umbrella/charts/spiffe-csi-driver
	@echo "✓ All Phase 10 charts lint clean"

# ─── KAC-NLB: kacho-nlb (L4 NLB control-plane) ─────────────────────────
#
# Sub-chart at project/kacho-nlb/deploy/ — wired into umbrella via
# `file://../../../kacho-nlb/deploy` (helm/umbrella/Chart.yaml). Deployment
# name `kacho-nlb`, Postgres StatefulSet `kacho-umbrella-pg-nlb`.

# alias для reload-svc SVC=nlb — пересобрать и перезагрузить kacho-nlb.
reload-svc-nlb:
	@$(MAKE) reload-svc SVC=nlb

# psql прямо в kacho_nlb-БД. Bitnami pg-nlb StatefulSet exposes
# `kacho-umbrella-pg-nlb` Service; user / db = `kacho_nlb` (see
# helm/umbrella/values.dev.yaml pg-nlb.auth).
psql-nlb: guard-kind-context
	kubectl exec -it -n kacho statefulset/kacho-umbrella-pg-nlb -- \
	    env PGPASSWORD=dev-nlb-password psql -U kacho_nlb -d kacho_nlb

# Логи kacho-nlb deployment (main container — no migrator init-container
# logs unless the pod is failing; `kubectl logs … -c migrate` for that).
logs-nlb:
	kubectl logs -n kacho -f deploy/kacho-nlb

# Seed kacho-nlb test fixtures (subnet/instance/nic/external-address) into
# the running dev stack. Idempotent — re-runs reuse existing resources by
# name. Output: writes .seeded-ids.env at repo root for downstream newman.
#
# Requires: dev-up complete, api-gateway reachable at $$BASE_URL (default
# http://localhost:28080 for kind port-forward; CI uses
# http://api.kacho.local:28080).
seed-nlb:
	@BASE_URL="$${BASE_URL:-http://localhost:28080}" \
	 bash ./scripts/seed-nlb-fixtures.sh

# ─── Базовый каталог размещения kacho-geo (Region/Zone) ────────────────────
#
# ЗАЧЕМ ЭТО ОТДЕЛЬНАЯ ЦЕЛЬ И ПОЧЕМУ ОНА ВЫЗЫВАЕТСЯ ИЗ `dev-up`. Каталог geo
# стартует ПУСТЫМ — осознанное решение (`services/geo/internal/migrations/*.sql`
# не содержит ни одного INSERT: `git grep -c 'INSERT INTO' -- \
# 'services/geo/internal/migrations/*.sql'` → 0 файлов; Region/Zone заводит
# администратор). Пока каталог пуст, ЛЮБОЕ создание зонального или регионального
# ресурса отвечает «зона/регион не найдены»: vpc/compute/nlb/storage/registry
# валидируют placement peer-вызовом к geo на пути запроса, fail-closed. То есть
# свежеподнятый стенд непригоден, и это НЕ видно ни по одному «под Ready» —
# ровно тот класс, который ловит `assert-production-posture`.
#
# ПОЧЕМУ НЕ МИГРАЦИЕЙ: пустой старт каталога — решение (см. 0001_initial.sql), а
# применённую миграцию не правят. ПОЧЕМУ НЕ ХУКОМ ЧАРТА: единственный Job-хук
# geo — ОДНОРАЗОВЫЙ перенос из legacy-compute, у него другой предмет и он
# выключен во всех профилях. ПОЧЕМУ НЕ ЧЕРЕЗ ADMIN-RPC: этой дорогой каталог
# сеет приёмочная фикстура (нужен бутстрап-токен + Hydra + проброс портов);
# вторая копия той машинерии в Makefile разъехалась бы молча. Обоснование
# равнозначности SQL-посева — в шапке `scripts/geo-baseline.sql`.
#
# guard-kind-context: цель ПИШЕТ в базу активного контекста.
seed-geo: guard-kind-context
	@bash ./scripts/seed-geo-baseline.sh

# ─── Базовый каталог блочного хранения (кластер данных, классы, привязки) ──
#
# Тот же класс беды, что у пустого каталога geo, и то же решение. Каталог
# классов стартует пустым осознанно: класс без действующей ревизии привязки —
# обещание ёмкости, которой никто не дал, поэтому посев из миграции снят, а
# продукт поставляется пустым. Следствие: свежий стенд не создаёт ни одного
# тома, и «под Ready» этого не показывает.
#
# ПОРЯДОК ЗНАЧИМ: привязка называет зону, зоны заводит seed-geo. Отсюда цель
# стоит в `dev-up` строго после него, а сам скрипт спрашивает зоны у владельца
# и падает, если открытых нет, — а не пишет ноль привязок молча.
#
# ПОЧЕМУ НЕ АДМИН-RPC: та же причина, что у geo (бутстрап-токен + Hydra +
# проброс портов; вторая копия этой машинерии разъехалась бы с первой молча).
#
# guard-kind-context: цель ПИШЕТ в базу активного контекста.
seed-storage: guard-kind-context
	@POSTURE_SKIP="$(POSTURE_SKIP)" bash ./scripts/seed-storage-catalog.sh

# ─── Базовый каталог внешних адресов vpc (полоса аникаста) ─────────────────
#
# Тот же класс беды, что у пустых каталогов geo и хранения, и то же решение.
# Внешнее размещение бывает ТОЛЬКО региональным (`EXTERNAL_REGIONAL`), а
# региональный ресурс зоне-независим by construction, поэтому публичный адрес
# внешнего балансировщика выделяется БЕЗ зоны и резолвится в полосе
# `zone_id IS NULL` (GetDefaultForZone("")). Полоса стартует пустой: в
# `services/vpc/internal/migrations/*.sql` нет ни одного INSERT в address_pools —
# пулы заводит администратор. Пока она пуста, каждое создание внешнего
# балансировщика отвечает «could not allocate load balancer address», сага сносит
# durable-handle компенсацией, и GET по названному идентификатору отдаёт
# НАСТОЯЩИЙ 404. Стенд отчитывается поднятым и непригоден.
#
# ПОЧЕМУ НЕ ЧЕРЕЗ ADMIN-RPC: `InternalAddressPoolService` авторизуется на
# кластерном синглтоне, то есть требует cluster-admin RS256. Полоса консоли
# такого принципала не выпускает вовсе (её фикстура регистрирует свежего
# арендатора формой провайдера личности), и завести его там значило бы принести
# вторую копию машинерии приёмочных фикстур — та же причина, по которой SQL
# выбран для geo и для каталога хранения.
#
# ПОЧЕМУ НЕ МИГРАЦИЕЙ: пустой старт каталога — решение, а адресный план стенда в
# боевой поставке бессмыслен; применённую миграцию не правят (ban #5).
#
# guard-kind-context: цель ПИШЕТ в базу активного контекста.
seed-vpc-pools: guard-kind-context
	@POSTURE_SKIP="$(POSTURE_SKIP)" bash ./scripts/seed-vpc-address-pools.sh

# Только УТВЕРЖДЕНИЕ, без записи: «условие создано» — отдельный вопрос от
# «пробы зелёные», и волна консоли задаёт его отдельным шагом (e2e-flow.md §6).
# Предикат тот же самый и живёт в одном файле с посевом: вторая его копия
# разошлась бы с первой молча.
seed-vpc-pools-check:
	@POSTURE_SKIP="$(POSTURE_SKIP)" bash ./scripts/seed-vpc-address-pools.sh --check