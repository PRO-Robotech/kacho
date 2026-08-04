// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// defaults.go — единственное место, где задаются дефолты config'а
// (defaults не в struct-tags, не в main, а в config-пакете).
//
// Все ключи — viper-paths с делимитером `.` (binding в `KACHO_NLB_…__…`
// через `SetEnvKeyReplacer(".", "__")` в `load.go`).
package config

import "github.com/spf13/viper"

// RegisterDefaults биндит дефолты для всех опциональных полей. Required-поля
// (`repository.postgres.url`; в production-mode — ещё и адреса peer-рёбер,
// включая `extapi.iam.internal-addr`, по которому идёт per-RPC Check) НЕ
// дефолтятся — их отсутствие ловит `Config.Validate`.
func RegisterDefaults(v *viper.Viper) {
	// Mode: production — fail-closed default (security.md «Любой деплой —
	// production-mode»). Пропущенный/пустой `mode` НЕ снимает молча
	// authN/authZ/mTLS-гварды (CWE-1188 insecure default); dev — ЯВНЫЙ opt-in
	// (`mode: dev`), только для локальных фикстур. Паритет с kacho-vpc
	// (`authn.mode`=production по умолчанию).
	v.SetDefault("mode", "production")

	// Logger
	v.SetDefault("logger.level", "DEBUG")

	// API-server
	v.SetDefault("api-server.endpoint", "tcp://0.0.0.0:9090")
	v.SetDefault("api-server.internal-endpoint", "tcp://0.0.0.0:9091")
	v.SetDefault("api-server.graceful-shutdown", "10s")
	v.SetDefault("api-server.grpc-gw-enable", false)

	// Metrics / Healthcheck. metrics.address — cluster-internal diagnostic
	// HTTP-listener (metrics + /healthz + /readyz); :9101 совпадает с
	// deploy ports.metrics + ServiceMonitor scrape-таргетом.
	v.SetDefault("metrics.enable", true)
	v.SetDefault("metrics.address", ":9101")
	v.SetDefault("healthcheck.enable", true)

	// Repository
	v.SetDefault("repository.type", "POSTGRES")
	v.SetDefault("repository.postgres.max-conns", int32(0))
	v.SetDefault("repository.postgres.conn-lifetime", "0s")
	// Empty defaults для required-полей: нужны, чтобы viper.AutomaticEnv
	// нашёл соответствующие ENV ключи (`KACHO_NLB_REPOSITORY__POSTGRES__URL`).
	// Validate ловит пустые после Unmarshal.
	v.SetDefault("repository.postgres.url", "")

	// Authn (transport TLS)
	v.SetDefault("authn.type", "none")
	v.SetDefault("authn.tls.client.verify", "skip")

	// ExtAPI (peer gRPC clients)
	v.SetDefault("extapi.def-dial-duration", "10s")
	// КАЖДЫЙ leaf-ключ регистрируется (пусть и нулевым значением) — иначе
	// viper.AutomaticEnv о нём не знает и не биндит соответствующую
	// KACHO_NLB_EXTAPI__<PEER>__<NAME> (env-bind получают только ключи, которые
	// viper видел через SetDefault/BindEnv). Без регистрации адреса пиров
	// приходили ИСКЛЮЧИТЕЛЬНО из YAML, а задокументированный ENV-override молча
	// не срабатывал — та же причина, по которой ниже поимённо перечислены все
	// leaf-ключи mtls.
	for _, peer := range []string{"vpc", "compute", "iam", "geo"} {
		v.SetDefault("extapi."+peer+".addr", "")
		v.SetDefault("extapi."+peer+".internal-addr", "")
		v.SetDefault("extapi."+peer+".dial-duration", "0s") // 0 → берёт def-dial-duration
		v.SetDefault("extapi."+peer+".tls", false)
	}
	// extapi.geo.addr — kacho-geo endpoint (kacho-geo). Помимо
	// стандартного KACHO_NLB_EXTAPI__GEO__ADDR биндим ЯВНУЮ ENV
	// `KACHO_NLB_GEO_GRPC_ADDR` (короткое каноническое имя geo-эндпоинта,
	// согласованное с deploy). BindEnv с явным именем обходит prefix/replacer.
	_ = v.BindEnv("extapi.geo.addr", "KACHO_NLB_GEO_GRPC_ADDR")

	// Authz (FGA Check + cache + listen-invalidator)
	// Адреса здесь НЕТ: соединение, по которому идёт Check, строится из
	// `extapi.iam.internal-addr` (conn `iam-internal`).
	v.SetDefault("authz.iam.dial-deadline", "3s")
	v.SetDefault("authz.iam.request-timeout", "500ms")
	v.SetDefault("authz.cache.enable", true)
	v.SetDefault("authz.cache.ttl", "5s") // ≤10s
	v.SetDefault("authz.listen-invalidator.enable", false)
	v.SetDefault("authz.listen-invalidator.channel", "kacho_iam_subjects")
	// RBAC (issue): per-object filtered List. Default ON
	// («применяется во всех доменах»); fail-closed (security.md). Endpoint —
	// iam.AuthorizeService (reuse iam conn; mTLS via mtls.iam-register).
	// ENV: KACHO_NLB_AUTHZ__LIST_FILTER__ENABLED / __TIMEOUT / __CACHE_TTL / etc.
	v.SetDefault("authz.list-filter.enabled", true)
	// timeout — per-call дедлайн ОДНОГО BatchCheck, НЕ бюджет всей фильтрации
	// (бюджет операции выводится в authzfilter.NewFGAFilter). 1s, а не прежние
	// 500ms: батчи страницы идут ограниченным fan-out'ом
	// (authzfilter.defaultParallelism = 5), поэтому worst-case глубина на
	// предельной странице (page_size=1000) — 4 волны, а не 20 последовательных
	// хопов, и 4×1s помещается в выведенный бюджет (6s) с запасом 33%. Прежние
	// 500ms были подогнаны не под латентность здорового пира, а под число
	// последовательных хопов — загруженный iam ронял позитивный List в 503.
	v.SetDefault("authz.list-filter.timeout", "1s")
	v.SetDefault("authz.list-filter.cache-ttl", "5s")
	v.SetDefault("authz.list-filter.cache-max-entries", 10000)
	v.SetDefault("authz.list-filter.fail-open", false)
	v.SetDefault("authz.breakglass", false)
	// trusted-forwarder-sans: allow-list cert-identity SAN'ов доверенных форвардеров
	// (api-gateway). Пусто (default) → любой mTLS-verified peer доверен (back-compat).
	// ENV KACHO_NLB_AUTHZ__TRUSTED_FORWARDER_SANS (comma-separated).
	v.SetDefault("authz.trusted-forwarder-sans", []string{})

	// FGA register-drainer (Вариант A).: default-on — drainer
	// is an in-process goroutine; without it created resources never get an
	// owner-tuple (worse than the former best-effort path). mTLS on the
	// drainer→iam edge is a separate per-edge flag (mtls.iam-register, default off).
	v.SetDefault("fga.register-drainer.enable", true)
	v.SetDefault("fga.register-drainer.batch-size", 32)
	v.SetDefault("fga.register-drainer.poll-fallback", "30s")
	v.SetDefault("fga.register-drainer.max-attempts", 10)
	v.SetDefault("fga.register-drainer.backoff-min", "1s")
	v.SetDefault("fga.register-drainer.backoff-max", "30s")
	// fail-closed boot-gate. Default off (dev back-compat);
	// production sets KACHO_NLB_REQUIRE_IAM=true. Explicit BindEnv → the canonical
	// short env name (KACHO_<SVC>_REQUIRE_IAM) shared across the fleet.
	v.SetDefault("fga.require-iam", false)
	_ = v.BindEnv("fga.require-iam", "KACHO_NLB_REQUIRE_IAM")

	// mTLS (opt-in, per-edge). Default OFF on every edge → insecure
	// (dev backward-compat). corelib field names lowercased by
	// mapstructure: enable / certfile / keyfile / clientcafiles / cafiles / servername.
	//
	// Every leaf key is registered (even with a zero default) so viper's
	// AutomaticEnv knows the key exists and binds the corresponding
	// KACHO_NLB_MTLS__<EDGE>__<NAME> env var on Unmarshal (viper only env-binds
	// keys it has seen via SetDefault/BindEnv).
	v.SetDefault("mtls.server.enable", false)
	v.SetDefault("mtls.server.certfile", "")
	v.SetDefault("mtls.server.keyfile", "")
	v.SetDefault("mtls.server.clientcafiles", []string{})
	for _, edge := range []string{"iam-register", "iam-project", "vpc", "compute", "geo"} {
		v.SetDefault("mtls."+edge+".enable", false)
		v.SetDefault("mtls."+edge+".certfile", "")
		v.SetDefault("mtls."+edge+".keyfile", "")
		v.SetDefault("mtls."+edge+".cafiles", []string{})
		v.SetDefault("mtls."+edge+".servername", "")
	}

	// Jobs (background workers)
	// target-drain: двухфазный drain runner. 10s — компромисс
	// между latency удаления expired targets и нагрузкой на БД.
	v.SetDefault("jobs.target-drain.interval", "10s")
	// free-ip: реконсиляция застрявших БАЛАНСИРОВЩИКОВ (не листенеров — VIP
	// консолидирован на LoadBalancer, колонки адреса у листенера дропнуты
	// миграцией 0028). 30s — сироты редки; порог 5m исключает гонку со штатным
	// in-flight create/delete.
	v.SetDefault("jobs.free-ip.interval", "30s")
	v.SetDefault("jobs.free-ip.age-threshold", "5m")

	// InternalLifecycle (stream к kacho-iam).
	// 32 одновременных стрима — достаточно для одного-двух iam-pod'ов (по
	// одному стриму на pod, обычно), с запасом на дублирование при rollout
	// и admin-tooling. Каждый стрим = +1 dedicated pgx.Conn к Postgres.
	v.SetDefault("internal-lifecycle.max-streams", 32)
}
