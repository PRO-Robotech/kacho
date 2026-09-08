// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"time"

	"github.com/spf13/viper"
)

// RegisterDefaults устанавливает default-значения всех конфиг-ключей
// (defaults в одном месте, не в struct-tags).
//
// Карта «legacy ENV → новый ключ» (значения покрывают dev-стенд без values.yaml):
//
//	legacy env → new key
//	-------------------------------------------------------------------------
//	KACHO_VPC_DB_HOST=localhost                       → repository.postgres.url (compose)
//	KACHO_VPC_DB_PORT=5432
//	KACHO_VPC_DB_USER=vpc
//	KACHO_VPC_DB_NAME=kacho_vpc
//	KACHO_VPC_DB_SSLMODE=disable                      → repository.postgres.ssl-mode
//	KACHO_VPC_DB_MAX_CONNS=0                          → repository.postgres.max-conns
//	KACHO_VPC_GRPC_PORT=9090                          → api-server.endpoint=tcp://0.0.0.0:9090
//	KACHO_VPC_INTERNAL_PORT=9091                      → api-server.internal-endpoint=tcp://0.0.0.0:9091
//	KACHO_VPC_IAM_GRPC_ADDR=...                       → extapi.iam.endpoint
//	KACHO_VPC_IAM_TLS=false                           → extapi.iam.tls.enable
//	KACHO_VPC_IAM_DNS_LB=false                        → extapi.iam.dns-lb
//	KACHO_VPC_GEO_GRPC_ADDR=...                       → extapi.geo.endpoint
//	KACHO_VPC_GEO_TLS=false                           → extapi.geo.tls.enable
//	KACHO_VPC_PROJECT_CACHE_TTL=30s                    → network.project-cache.positive-ttl
//	KACHO_VPC_PROJECT_CACHE_NEGATIVE_TTL=5s            → network.project-cache.negative-ttl
//	KACHO_VPC_PROJECT_CACHE_SIZE=10000                 → network.project-cache.max-size
//	KACHO_VPC_AUTH_MODE=dev                           → authn.mode
//
// DB-пароль остается read-from-env (см. PostgresConfig.PasswordFromEnv).
func RegisterDefaults(v *viper.Viper) {
	// logger
	v.SetDefault("logger.level", "INFO")

	// api-server
	v.SetDefault("api-server.endpoint", "tcp://0.0.0.0:9090")
	v.SetDefault("api-server.internal-endpoint", "tcp://0.0.0.0:9091")
	v.SetDefault("api-server.graceful-shutdown", 10*time.Second)
	// request-timeout — server-side deadline на один RPC (защита от bounded-pool
	// exhaustion / deadline-less запросов, CWE-770). 0 → без границы.
	v.SetDefault("api-server.request-timeout", 30*time.Second)

	// Подписка на изменения (`pkg/subscription`). Величины — посадочные, поэтому
	// стоят здесь, а не в объявлении журнала: журнал говорит, ГДЕ он лежит, а не
	// сколько живёт поток и сколько их бывает разом.
	v.SetDefault("api-server.subscription-stream-budget", time.Hour)
	v.SetDefault("api-server.subscription-max-streams", 16)
	v.SetDefault("api-server.subscription-idle-poll", 2*time.Second)

	// api-server.rate-limit — темп и одновременность запросов НА ВЫЗЫВАЮЩЕГО,
	// отдельно для каждого листенера (см. ratelimit.go).
	//
	// Умолчание — НУЛИ, то есть «не объявлено», и полярность выбрана осознанно
	// против «удобной»: величины, вписанные сюда, описывали бы один стенд (предел
	// исполняется ведром В ПРОЦЕССЕ, поэтому при N репликах эффективная величина
	// равна N × объявленного) и выглядели бы работающей защитой на всех
	// остальных. Нулевые величины ничего не ограничивают, поэтому боевая посадка
	// на них НЕ ПОДНИМАЕТСЯ (ValidateRequestRateLimits называет ручку в отказе), а
	// значение объявляет чарт.
	//
	// Ключи объявлены здесь ещё и затем, чтобы их видел ENV-override: viper
	// подхватывает переменную окружения только для ИЗВЕСТНОГО ключа, поэтому без
	// SetDefault `KACHO_VPC_API_SERVER__RATE_LIMIT__*` не доехал бы до поля вовсе.
	// Объявление домена величин. Умолчание — ПУСТАЯ строка, то есть «оператор не
	// выбрал», и боевая посадка на ней не поднимается: у ручки ровно два законных
	// значения, и незаданное среди них не значится.
	//
	// Ключ объявлен здесь ещё и затем, чтобы его видел ENV-override: viper
	// подхватывает переменную окружения только для ИЗВЕСТНОГО ключа, поэтому без
	// SetDefault `KACHO_VPC_QUOTA__AUTHORITY` не доехал бы до поля ВОВСЕ — ручка
	// принималась бы профилем и не читалась процессом.
	v.SetDefault("quota.authority", "")

	v.SetDefault("api-server.rate-limit.public.read-per-sec", 0.0)
	v.SetDefault("api-server.rate-limit.public.mutation-per-sec", 0.0)
	v.SetDefault("api-server.rate-limit.public.burst-factor", 0.0)
	v.SetDefault("api-server.rate-limit.public.in-flight", 0)
	v.SetDefault("api-server.rate-limit.internal.read-per-sec", 0.0)
	v.SetDefault("api-server.rate-limit.internal.mutation-per-sec", 0.0)
	v.SetDefault("api-server.rate-limit.internal.burst-factor", 0.0)
	v.SetDefault("api-server.rate-limit.internal.in-flight", 0)

	// metrics / healthcheck — cluster-internal diagnostic listener (/metrics +
	// /healthz + /readyz). endpoint=:9095 зеркалит kaname; enable=false ИЛИ
	// пустой endpoint → listener не поднимается.
	v.SetDefault("metrics.enable", true)
	v.SetDefault("metrics.endpoint", ":9095")
	v.SetDefault("healthcheck.enable", true)

	// repository
	v.SetDefault("repository.type", "POSTGRES")
	// URL по умолчанию покрывает локальный goose / `make test` без values.yaml.
	// Пароль подставляется из ENV (см. password-from-env ниже).
	v.SetDefault("repository.postgres.url", "postgres://vpc@localhost:5432/kacho_vpc")
	// slave-url — опц. DSN read-replica. Пустая строка → Reader-TX идут на master
	// (fallback). Когда деплой добавит реплику — выставляется через
	// values.yaml / ENV KACHO_VPC_REPOSITORY__POSTGRES__SLAVE_URL.
	v.SetDefault("repository.postgres.slave-url", "")
	v.SetDefault("repository.postgres.max-conns", 0)
	// serving-only DB-тайм-ауты (в MigrateDSN не попадают): ограничивают время
	// одного запроса / ожидания блокировки, чтобы зависший запрос не держал
	// pooled-connection бесконечно (CWE-770/400). 0 → Postgres default (без лимита).
	v.SetDefault("repository.postgres.statement-timeout", 30*time.Second)
	v.SetDefault("repository.postgres.lock-timeout", 15*time.Second)
	v.SetDefault("repository.postgres.ssl-mode", "disable")
	v.SetDefault("repository.postgres.password-from-env", "KACHO_VPC_DB_PASSWORD")

	// authn — secure-by-default: production (anonymous → fail-closed). Локальный
	// режим без AuthN (anonymous как admin) включается явно: authn.mode=dev
	// (values-dev.yaml / KACHO_VPC_AUTH_MODE=dev) — только для dev-стенда и тестов.
	v.SetDefault("authn.mode", "production")
	// trusted-forwarder — fail-closed default. В production (non-strict) публичный
	// :9090 listener требует ЛИБО server-mTLS, ЛИБО явного trusted-forwarder=true
	// (оператор подтверждает, что listener стоит за аутентифицированным
	// forwarder'ом/mesh). Без одного из двух production-старт отвергается
	// (ValidateServerMTLS) — client-asserted principal по plaintext недопустим.
	v.SetDefault("authn.trusted-forwarder", false)

	// extapi
	// project-existence peer — kaname (ProjectService.Get).
	v.SetDefault("extapi.def-dial-duration", 10*time.Second)
	v.SetDefault("extapi.iam.endpoint", "iam.kacho.svc:9090")
	v.SetDefault("extapi.iam.tls.enable", false)
	v.SetDefault("extapi.iam.dns-lb", false)
	// zone_id валидируется через kacho-geo (leaf-домен Geography), а не
	// kacho-compute. Dial-host = geo k8s Service `kacho-geo` на public :9090
	// listener (ZoneService.Get/List); host совпадает с server-cert SAN
	// (kacho-geo.* / kacho-geo-internal.*).
	v.SetDefault("extapi.geo.endpoint", "kacho-geo.kacho.svc:9090")
	v.SetDefault("extapi.geo.tls.enable", false)

	// authz. По умолчанию iam-endpoint пустой → interceptor не навешивается;
	// включается через values.yaml / ENV. В dev-стенде — values-dev.yaml
	// выставит iam-endpoint=kaname.kacho.svc:9091.
	v.SetDefault("authz.iam-endpoint", "")
	v.SetDefault("authz.iam-tls.enable", false)
	v.SetDefault("authz.check-timeout", 2*time.Second)
	v.SetDefault("authz.deny-rate-limit-per-sec", 100.0)
	v.SetDefault("authz.cache-ttl", 5*time.Second)
	// trusted-forwarder-sans — круг отправителей, которым разрешено передавать
	// личность конечного пользователя. Пусто по умолчанию, и это НЕ «никому»:
	// contract corelib сужает круг лишь на непустом списке. Поэтому боевая посадка
	// на пустом списке не стартует (Validate), а значение задаёт чарт.
	// ENV KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS (через запятую).
	v.SetDefault("authz.trusted-forwarder-sans", []string{})
	// trust-domain — домен доверия установки. Пусто по умолчанию, и это самое
	// строгое прочтение: по необъявленному домену не опознаётся ни один
	// предъявитель, а боевая посадка на нём не стартует (Validate). Величину
	// задаёт чарт, а не сборка.
	//
	// Ключ объявлен ЗДЕСЬ не ради умолчания, а ради ПРИВЯЗКИ: viper связывает с
	// переменной окружения только ключи, которые он уже видел. Померено, а не
	// предположено: без этой строки `KACHO_VPC_AUTHZ__TRUST_DOMAIN` не доезжает
	// ни в одном написании, и имя переменной в тексте отказа было бы обещанием
	// возможности, которой нет.
	v.SetDefault("authz.trust-domain", "")

	// per-object list-filter (per-page BatchCheck-filtered List).
	// Default enabled=true: List<Resource> возвращает только доступные объекты
	// (read==enforce, no-leak). endpoint/mTLS — через values.yaml (deploy);
	// пустой authorize-endpoint → fallback на iam-endpoint. Anonymous/no-subject
	// → fail-closed (use-case passthrough только для system-principal).
	v.SetDefault("authz.list-filter.enabled", true)
	v.SetDefault("authz.list-filter.authorize-endpoint", "")
	v.SetDefault("authz.list-filter.authorize-tls.enable", false)
	// timeout-ms — per-call дедлайн ОДНОГО BatchCheck, НЕ бюджет всей фильтрации.
	// 1000, а не прежние 500: батчи страницы идут ограниченным fan-out'ом
	// (authzfilter.defaultParallelism = 5), поэтому worst-case глубина на
	// предельной странице (page_size=1000) — 4 волны, а не 20 последовательных
	// хопов, и 4×1s помещается в выведенный бюджет операции (6s) с запасом 33%.
	// Прежние 500ms были подогнаны не под латентность здорового пира, а под число
	// последовательных хопов — загруженный iam ронял позитивный List в 503.
	v.SetDefault("authz.list-filter.timeout-ms", 1000)
	v.SetDefault("authz.list-filter.cache-ttl", 5*time.Second)
	v.SetDefault("authz.list-filter.max-entries", 10000)
	v.SetDefault("authz.list-filter.fail-open", false)
	// Умолчания аварийного пропуска страницы здесь нет: ручка снята целиком (её
	// имя — в `retired_knobs_test.go`). «Модели прав нет» разрешением не бывает, а
	// посадка без модели не поднимается вовсе — отказ даёт `ValidateListFilter` на
	// любой посадке, поэтому объявлять было нечего.

	// iam — интеграция с kaname. require — fail-closed boot-gate (default off:
	// dev/Create разрешён, только Warn). register-drainer-enabled — default-on
	// (owner-tuple publisher). Ранее оба читались os.LookupEnv в cmd/; теперь —
	// типизированные ключи со строгой bool-валидацией на decode.
	v.SetDefault("iam.require", false)
	v.SetDefault("iam.register-drainer-enabled", true)

	// network (VPC-domain)
	v.SetDefault("network.project-cache.positive-ttl", 30*time.Second)
	v.SetDefault("network.project-cache.negative-ttl", 5*time.Second)
	v.SetDefault("network.project-cache.max-size", 10000)

	// dataplane.executor — что посадка ЗАЯВЛЯЕТ об исполнителе, которому контур
	// отдаёт принятое от арендатора (см. dataplane.go).
	//
	// Умолчание — «НЕ объявлено» по каждому признаку, и полярность выбрана
	// осознанно: незаявленный исполнитель не считается способным. Обратное
	// умолчание было бы удобнее чарту и неверно по существу — посадка, забывшая
	// объявить профиль, получала бы «умеет всё» молча. Боевая посадка на пустом
	// профиле не поднимается (ValidateExecutorProfile), значение задаёт чарт.
	//
	// Ключи объявлены здесь ещё и для того, чтобы их видел ENV-override: viper
	// подхватывает переменную окружения только для ИЗВЕСТНОГО ключа, поэтому без
	// SetDefault `KACHO_VPC_DATAPLANE__EXECUTOR__*` не доехал бы до поля вовсе.
	v.SetDefault("dataplane.executor.overlapping-tenant-addresses", false)
	v.SetDefault("dataplane.executor.state-tracking-families", []string{})
	v.SetDefault("dataplane.executor.named-set-reference-in-rule", false)
	v.SetDefault("dataplane.executor.guaranteed-payload-bytes", 0)
	v.SetDefault("dataplane.executor.guaranteed-bandwidth-per-interface-mbps", 0)
	v.SetDefault("dataplane.executor.connection-limit-per-interface", 0)
	// Умолчание ОБЯЗАТЕЛЬНО, даже когда оно нулевое: разбор настроек резолвит
	// переменную окружения только для ключа, который ему УЖЕ известен, а известен
	// он из умолчаний. Ключ, объявленный лишь полем структуры, из окружения не
	// приезжает — молча, и величина остаётся нулём при заданной оператором
	// переменной. Полярность нуля та же, что у соседей: ноль означает ОТСУТСТВИЕ
	// объявления, а не «ограничения нет», и боевую посадку с ним страж не поднимает.
	v.SetDefault("dataplane.executor.connection-rate-limit-per-interface-per-second", 0)
	v.SetDefault("dataplane.executor.connection-rate-burst-per-interface", 0)
	v.SetDefault("dataplane.executor.tenant-settable-bandwidth-limit", false)

	// dataplane.reserved-prefixes — адресные диапазоны, которые платформа держит
	// ЗА СОБОЙ (служебные адреса узлов, адреса служб внутри подсети, точка
	// получения метаданных экземпляра).
	//
	// Умолчание — ПУСТО, и это осознанно противоположно «удобному»: перечень,
	// вписанный сюда, описывал бы один стенд и выглядел бы работающей защитой на
	// всех остальных. Пустой перечень при этом не резервирует ничего, поэтому
	// боевая посадка на нём НЕ ПОДНИМАЕТСЯ (ValidateReservedPrefixes называет
	// ручку в отказе), а значение объявляет чарт.
	//
	// Ключ объявлен здесь ещё и для того, чтобы его видел ENV-override: viper
	// подхватывает переменную окружения только для ИЗВЕСТНОГО ключа, поэтому без
	// SetDefault `KACHO_VPC_DATAPLANE__RESERVED_PREFIXES` не доехал бы до поля вовсе.
	v.SetDefault("dataplane.reserved-prefixes", []string{})
}
