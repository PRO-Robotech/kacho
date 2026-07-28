// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strings"
)

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
//   - per-object фильтр публичного List — cfg.ListFilterEnabled;
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

	if len(problems) > 0 {
		return fmt.Errorf("%s mode refuses insecure config: %s", mode, strings.Join(problems, "; "))
	}
	return nil
}
