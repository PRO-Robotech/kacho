// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// trusted_forwarders_test.go — boot-guard на измерение «кому разрешено передавать
// чужую личность».
//
// Что защищаем. Оба листенера storage строят цепочку
// CertIdentityExtract → TrustedPrincipalExtract(WithTrustedForwarders(список)).
// Контракт corelib (pkg/grpcsrv/cert_identity.go, principalIsTrusted): круг
// отправителей сужается ТОЛЬКО когда список непуст; на пустом списке функция
// возвращает «доверяем» для любого пира, прошедшего проверку сертификата. То есть
// пустой список — это не «никому», а «кому угодно внутри периметра»: сосед со своим
// законным сертификатом присылает заголовки личности жертвы, и решение о правах
// принимается от её имени.
//
// Поэтому в боевом режиме пустой список обязан ОТКАЗЫВАТЬ В СТАРТЕ — ровно так же,
// как отказывают plaintext-соединение с БД, снятый mTLS и отсутствующий адрес
// проверки прав. Стража общая на все семь сервисов — Config.Validate зовёт
// grpcsrv.TrustedForwarders.Require, — поэтому исход и текст отказа у них
// одинаковы, различаются только имена ручек.
//
// Тесты идут через LoadInto (реальный путь загрузки окружения), а не через литерал
// структуры: измерение обязано доехать «переменная окружения → конфиг → стража»
// целиком, иначе ручка была бы объявлена и не прочитана.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

// gatewaySANEnv — каноническая личность отправителя, законно передающего личность
// конечного пользователя на публичный листенер (значение из values.prod).
const gatewaySANEnv = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

// computeSANEnv — второй законный отправитель: compute передаёт личность
// пользователя на ВНУТРЕННИЙ листенер storage (привязка/отвязка тома идёт под
// личностью того, кто её инициировал). Пропустить его в списке — сломать привязку.
const computeSANEnv = "spiffe://kacho.cloud/ns/kacho/sa/kacho-compute"

// prodEnv — минимальное окружение, при котором боевой режим стартует: каждое уже
// гейтящееся измерение выставлено безопасно. Каждый тест ослабляет РОВНО ОДНО
// измерение — список отправителей.
func prodEnv(forwarders string) map[string]string {
	return map[string]string{
		"KACHO_STORAGE_DB_PASSWORD":                 "secret",
		"KACHO_STORAGE_AUTH_MODE":                   "production",
		"KACHO_STORAGE_DB_SSLMODE":                  "require",
		"KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR":         "kacho-iam-internal:9091",
		"KACHO_STORAGE_LIST_FILTER_ENABLED":         "true",
		"KACHO_STORAGE_PUBLIC_SERVER_MTLS_ENABLE":   "true",
		"KACHO_STORAGE_INTERNAL_SERVER_MTLS_ENABLE": "true",
		// Транспорт ребра storage→iam: с тех пор как страж требует его в боевом
		// режиме, окружение без этой ручки боевым не является. Фикстура,
		// снисходительнее продукта, делает невидимым ровно тот дефект, ради
		// которого её подставляют, — поэтому измерение выставлено и здесь, а
		// ослабляется только в своей пробе (peer_transport_test.go).
		"KACHO_STORAGE_IAM_CLIENT_MTLS_ENABLE":       "true",
		"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": forwarders,
	}
}

func loadEnv(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	var c config.Config
	if err := config.LoadInto(&c, env); err != nil {
		t.Fatalf("LoadInto err = %v", err)
	}
	return c
}

// TestValidate_Production_RefusesEmptyForwarderAllowList — сердце правки: боевой
// режим не стартует, пока круг отправителей чужой личности не сужен.
//
// RED до правки: Validate() возвращает nil — процесс поднимается и принимает
// переданную личность от любого пира с сертификатом.
func TestValidate_Production_RefusesEmptyForwarderAllowList(t *testing.T) {
	err := loadEnv(t, prodEnv("")).Validate()
	if err == nil {
		t.Fatal("production mode started with an EMPTY trusted-forwarder allow-list: " +
			"corelib narrows the circle only when the list is non-empty, so an empty list " +
			"lets ANY certificate-verified peer forward someone else's identity")
	}
	if !strings.Contains(err.Error(), "KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS") {
		t.Fatalf("the refusal must name the knob the operator has to set, got: %v", err)
	}
}

// TestValidate_ProductionStrict_RefusesEmptyForwarderAllowList — production-strict
// не мягче production (страже нельзя разъехаться по режимам).
func TestValidate_ProductionStrict_RefusesEmptyForwarderAllowList(t *testing.T) {
	env := prodEnv("")
	env["KACHO_STORAGE_AUTH_MODE"] = "production-strict"
	if err := loadEnv(t, env).Validate(); err == nil {
		t.Fatal("production-strict started with an EMPTY trusted-forwarder allow-list")
	}
}

// TestValidate_Production_RefusesBlankOnlyForwarderAllowList — список из одних
// пустых строк для corelib НЕ существует: WithTrustedForwarders отбрасывает "" и
// получает пустое множество, то есть снова «доверяем любому». Стража обязана
// считать так же, иначе `SANS=","` проходит гейт и молча возвращает дыру.
// (compute и nlb здесь считают len() и на этом кейсе слепы.)
func TestValidate_Production_RefusesBlankOnlyForwarderAllowList(t *testing.T) {
	if err := loadEnv(t, prodEnv(",")).Validate(); err == nil {
		t.Fatal("a list of blank entries passed the guard: corelib drops empty strings, " +
			"so the resulting allow-list is empty and trusts any verified peer")
	}
}

// TestValidate_Production_AcceptsPinnedForwarderAllowList — положительный путь: с
// закреплёнными отправителями боевой режим стартует. Держит стражу от вырождения в
// «отказывать всегда».
func TestValidate_Production_AcceptsPinnedForwarderAllowList(t *testing.T) {
	if err := loadEnv(t, prodEnv(gatewaySANEnv+","+computeSANEnv)).Validate(); err != nil {
		t.Fatalf("a pinned allow-list must boot, got refusal: %v", err)
	}
}

// devEnvWithEmptyCircle — dev-окружение, в котором insecure всё, включая круг.
func devEnvWithEmptyCircle() map[string]string {
	env := prodEnv("")
	env["KACHO_STORAGE_AUTH_MODE"] = "dev"
	env["KACHO_STORAGE_DB_SSLMODE"] = "disable"
	env["KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR"] = ""
	env["KACHO_STORAGE_PUBLIC_SERVER_MTLS_ENABLE"] = "false"
	env["KACHO_STORAGE_INTERNAL_SERVER_MTLS_ENABLE"] = "false"
	return env
}

// TestValidate_Dev_RefusesAnUnnarrowedCircleWithoutTheOptIn — стража круга
// срабатывает на ЛЮБОМ старте, а не только в боевом режиме: контроль, чья ветка
// на локальном стенде не исполняется ни разу, находит «забыл выставить круг»
// только на боевом профиле, где цена ошибки максимальна.
func TestValidate_Dev_RefusesAnUnnarrowedCircleWithoutTheOptIn(t *testing.T) {
	err := loadEnv(t, devEnvWithEmptyCircle()).Validate()
	if err == nil {
		t.Fatal("dev с несуженным кругом и без опт-ина обязан отказать в старте")
	}
	if !strings.Contains(err.Error(), "KACHO_STORAGE_AUTHZ_TRUST_ANY_FORWARDER") {
		t.Fatalf("отказ обязан назвать ручку опт-ина, иначе стенд не поднять: %v", err)
	}
}

// TestValidate_Dev_ToleratesAnUnnarrowedCircleWithTheExplicitOptIn —
// положительный контроль: без него отрицание выше зеленело бы и на «отказывать
// всегда».
func TestValidate_Dev_ToleratesAnUnnarrowedCircleWithTheExplicitOptIn(t *testing.T) {
	env := devEnvWithEmptyCircle()
	env["KACHO_STORAGE_AUTHZ_TRUST_ANY_FORWARDER"] = "true"
	if err := loadEnv(t, env).Validate(); err != nil {
		t.Fatalf("явный опт-ин обязан пропускать локальную фикстуру: %v", err)
	}
}
