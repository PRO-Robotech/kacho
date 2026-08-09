// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
)

// scopeFilteredRPCs — методы карты прав, с которых снят per-RPC Check: их
// авторизация целиком переехала на данные (per-object сужение прочитанной
// страницы). Список отсортирован — текст отказа наблюдаем оператором и не должен
// перетасовываться от прогона к прогону (обход map'ы не детерминирован).
//
// Карта читается ЗДЕСЬ, а не передаётся композиционным корнем: страж, которому
// список нужно вручить, no-op'ится на пустом аргументе, и «забыли передать» внешне
// неотличимо от «нечего защищать». Пакет check зависимостей на config не имеет,
// поэтому цикла нет.
func scopeFilteredRPCs() []string {
	var out []string
	for fullMethod, e := range check.PermissionMap() {
		if e.ScopeFiltered {
			out = append(out, fullMethod)
		}
	}
	sort.Strings(out)
	return out
}

// Validate — secure-by-default boot-guard: в production/production-strict операции
// без mTLS, per-RPC authz Check и с plaintext-DB ЗАПРЕЩЕНЫ (refuse-to-start). Раньше
// AuthMode был dead-code (объявлен, никогда не читался) → storage единственным из
// сервисов boot'ился insecure в «production» с одним WARN. Validate восстанавливает
// инвариант security.md «AuthN+AuthZ ВЕЗДЕ + любой деплой — production fail-closed»,
// зеркаля vpc.Config.Validate / geo.validateSecurityConfig.
//
// dev осознанно терпит insecure-дефолты (WARN эмитит serve.go) — только для локальных
// фикстур и dev-профиля стенда, НИКОГДА на кластере (KACHO_STORAGE_AUTH_MODE=dev на
// проде — security-долг под снос).
//
// Гейтит ровно те измерения, которые serve.go реально wire'ит по конфигу:
//   - mTLS листенеров — cfg.PublicServerMTLS.Enable / cfg.InternalServerMTLS.Enable;
//   - per-RPC authz Check — подключается ⟺ непустой cfg.AuthZIAMGRPCAddr;
//   - DB-транспорт — cfg.DBSSLMode в DSN;
//   - per-object фильтр видимости — cfg.ListFilterEnabled (есть ли он вообще) и
//     cfg.ListFilterFailOpen (не выключается ли он сам на первой ошибке iam);
//   - круг отправителей чужой личности — cfg.TrustedForwarders() (ровно то
//     значение, что уезжает в grpcsrv.WithTrustedForwarders на обоих листенерах).
//
// Поэтому «Validate прошёл в production» ⟺ «serve поднимется secure» by construction.
// Перечень обязан оставаться полным: измерение, которое serve.go настраивает, но
// Validate не проверяет, — это как раз тот класс, которым сюда попал список
// доверенных отправителей (проводка была, ручки и стражи не было).
func (c Config) Validate() error {
	mode, err := parseMode(c.AuthMode)
	if err != nil {
		return fmt.Errorf("KACHO_STORAGE_AUTH_MODE: %w", err)
	}
	if !mode.IsProduction() {
		// dev — insecure-дефолты допустимы (WARN в serve.go, не fatal).
		return nil
	}

	var problems []string

	// ── DB-транспорт: plaintext до БД в проде запрещён ──────────────────────
	switch mode {
	case ModeProduction:
		// Конкретный TLS-режим (require|verify-ca|verify-full) — на усмотрение
		// оператора; строгую проверку сертификата требует production-strict ниже.
		if c.DBSSLMode == "" || c.DBSSLMode == "disable" {
			problems = append(problems, fmt.Sprintf(
				"KACHO_STORAGE_DB_SSLMODE must not be %q (use require|verify-ca|verify-full)", c.DBSSLMode))
		}
	case ModeProductionStrict:
		switch c.DBSSLMode {
		case "require", "verify-ca", "verify-full":
		default:
			problems = append(problems, fmt.Sprintf(
				"KACHO_STORAGE_DB_SSLMODE must be one of require|verify-ca|verify-full (got %q)", c.DBSSLMode))
		}
	}

	// ── mTLS обязателен на ОБОИХ листенерах (internal :9091 НЕ освобождён) ──
	if !c.PublicServerMTLS.Enable || !c.InternalServerMTLS.Enable {
		problems = append(problems,
			"mTLS required on both listeners: set KACHO_STORAGE_PUBLIC_SERVER_MTLS_ENABLE and KACHO_STORAGE_INTERNAL_SERVER_MTLS_ENABLE=true")
	}

	// ── per-RPC authz Check обязателен (иначе serve пропускает интерсептор) ──
	if c.AuthZIAMGRPCAddr == "" {
		problems = append(problems,
			"per-RPC authz Check required on both listeners: set KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR")
	}

	// ── per-object list-filter обязателен ───────────────────────────────────
	// Per-RPC Check гейтит публичный List лишь на project-tier `viewer`: он
	// отвечает «этот caller вправе листать ЭТОТ проект», но НЕ сужает страницу до
	// объектов, на которые есть грант. Сужение делает ТОЛЬКО list-filter
	// (per-object `viewer` батчем по прочитанной странице — то же отношение, что
	// энфорсит Get). Выключенный фильтр = любой член проекта видит КАЖДЫЙ
	// том/снимок/образ проекта (over-show / BOLA-lite, CWE-862 / OWASP A01) —
	// ровно та дыра, ради которой фильтр и появился.
	//
	// Для InternalVolumeService/ListAttachments (:9091) фильтр — не второй слой, а
	// ЕДИНСТВЕННЫЙ: этот RPC помечен ScopeFiltered (единичного объекта, про который
	// можно спросить заранее, у него нет — инстансы называет вызывающий), поэтому
	// per-RPC Check за него не задаётся вовсе. Замок на связь марки и этого гейта —
	// check.TestScopeFilteredRPCs_AreBackedByTheProductionBootGuard.
	//
	// Адрес authorize-эндпоинта отдельно не проверяем: он и есть AuthZIAMGRPCAddr,
	// уже потребованный выше.
	if !c.ListFilterEnabled {
		problems = append(problems,
			"per-object List filter required: set KACHO_STORAGE_LIST_FILTER_ENABLED=true "+
				"(false → public List bypasses the per-object FGA filter, so a project-tier viewer sees "+
				"every volume/snapshot/image; and the internal attachment listing, which has no per-RPC "+
				"check at all, would lose its only gate)")
	}

	// ── degraded-mode ручка того же фильтра: fail-open ──────────────────────
	// Включённый фильтр, который на ошибке iam отдаёт страницу целиком, защищает
	// не больше выключенного — он просто выключается позже и по чужой аварии.
	// Пока в карте прав есть хоть одна запись ScopeFiltered, за этой ручкой не
	// остаётся НИЧЕГО: у такого RPC per-RPC Check не задаётся вовсе (интерсептор
	// отдаёт DecisionInternal), поэтому фильтр там не второй слой, а единственный,
	// и деградация означает выдачу привязок любых названных инстансов — из чужих
	// проектов и чужих аккаунтов.
	//
	// Для публичных List (за ними project-tier Check остаётся) fail-open — это
	// over-show внутри проекта; для ScopeFiltered — межтенантный. Гейт условный
	// намеренно: он сам снимется, если ScopeFiltered-марки уйдут из карты, и не
	// требует помнить о себе при этом.
	//
	// Осознанный размен: стенд, намеренно выставивший fail-open, перестанет
	// подниматься. Это и есть цель — иначе защиту снимает одна ручка, молча.
	if c.ListFilterFailOpen {
		if scopeFiltered := scopeFilteredRPCs(); len(scopeFiltered) > 0 {
			problems = append(problems, fmt.Sprintf(
				"fail-closed List filter required: set KACHO_STORAGE_LIST_FILTER_FAIL_OPEN=false "+
					"(true → on ANY iam error the page is returned UNFILTERED; %d RPC(s) carry no per-RPC "+
					"Check at all and rely on this filter as their only gate: %s)",
				len(scopeFiltered), strings.Join(scopeFiltered, ", ")))
		}
	}

	// ── круг отправителей чужой личности обязан быть сужен ──────────────────
	// Оба листенера строят цепочку CertIdentityExtract →
	// TrustedPrincipalExtract(WithTrustedForwarders(cfg.TrustedForwarders())).
	// Контракт corelib (pkg/grpcsrv principalIsTrusted) сужает круг ТОЛЬКО на
	// непустом списке; на пустом он отвечает «доверяем» ЛЮБОМУ пиру, прошедшему
	// проверку сертификата, и переданная в метаданных личность становится субъектом
	// проверки прав (pkg/authz subject_extract). То есть на пустом списке сосед со
	// своим законным сертификатом (compute, nlb, vpc, registry, оператор) читает,
	// меняет и удаляет чужие тома, снимки и образы от имени жертвы, а на внутреннем
	// листенере ещё и привязывает/отвязывает их. Внутренний периметр у нас объявлен
	// НЕдоверенным, и слой TLS имена не сверяет — сужает только этот список.
	//
	// Проверяем результат TrustedForwarders(), а не длину сырого поля: там же, где
	// сужение реально произойдёт, отбрасываются пустые записи, поэтому `SANS=","`
	// не может пройти гейт и вернуть дыру (у compute и nlb этот кейс считается
	// len() и проходит).
	//
	// dev осознанно терпит пусто — но только в in-process фикстурах: на РАЗВЁРНУТОМ
	// стенде dev-посадка запрещена отдельным правилом (production-mode ВЕЗДЕ).
	if len(c.TrustedForwarders()) == 0 {
		problems = append(problems,
			"trusted-forwarder allow-list required: set KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS "+
				"(empty → any certificate-verified peer may forward an end-user identity, so a neighbouring "+
				"service can act as any tenant; pin the api-gateway SAN and the compute SAN)")
	}

	// ── транспорт ИСХОДЯЩИХ рёбер ───────────────────────────────────────────
	// Выше проверены листенеры — то, КАК с нами говорят. Здесь — то, как говорим
	// мы: клиентская сторона рёбер storage→iam и storage→geo.
	//
	// Почему это отдельное измерение, а не следствие проверки листенеров.
	// Невзведённая ручка клиента не даёт ошибки сама по себе:
	// grpcclient.TLSClientCreds на Enable=false возвращает insecure-creds БЕЗ
	// ошибки, поэтому процесс поднимается, печатает «peer edge configured» и
	// «authz interceptor enabled», а каждый Check уходит по открытому каналу.
	// Контроль, от которого зависит решение о доступе, при этом присутствует и
	// не отказывает ни разу за свою жизнь.
	//
	// Предикат активности — ТОТ ЖЕ, что читает проводка: composition root зовёт
	// dialPeer(addr, creds, …), и dialPeer поднимает соединение ровно при
	// непустом адресе (cmd/storage/serve.go). Поэтому «страж увидел ребро» ⟺
	// «ребро дилится»: незаданный адрес не порождает требования к транспорту, а
	// заданный — порождает всегда. Связь стража с проводкой заперта
	// cmd/storage/peer_transport_wiring_test.go.
	//
	// Ручка на все три ребра к iam одна (IAMClientMTLS) — это записанное
	// отступление storage от соседей, см. docs/architecture/known-divergences.md
	// §1. Страж от него не зависит: он требует того же предиката, который читает
	// проводка, каким бы ни было число ручек.
	if c.AuthZIAMGRPCAddr != "" || c.IAMGRPCAddr != "" {
		if !c.IAMClientMTLS.Enable {
			problems = append(problems,
				"verified transport required on the storage→iam edges: set KACHO_STORAGE_IAM_CLIENT_MTLS_ENABLE=true "+
					"(with cert/key/CA) — the per-RPC authorization Check, the per-object List filter, the owner-tuple "+
					"registration and the project existence lookup all travel over this connection, and unarmed client "+
					"credentials degrade to cleartext silently, so the process starts and reports authorization as enabled")
		}
	}
	if c.GeoGRPCAddr != "" && !c.GeoClientMTLS.Enable {
		problems = append(problems,
			"verified transport required on the storage→geo edge: set KACHO_STORAGE_GEO_CLIENT_MTLS_ENABLE=true "+
				"(with cert/key/CA) — zone existence and the volume/image placement coherence check are decided on "+
				"this connection, and unarmed client credentials degrade to cleartext silently")
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s mode refuses insecure config: %s", mode, strings.Join(problems, "; "))
	}
	return nil
}
