// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Startup-validation tests for the revocation-path configuration implemented in
// revocation_validation.go.
//
// Свойство под проверкой: производственный край не поднимается с полосой отзыва,
// нацеленной в никуда. Адрес авторитета не выводится ни из чего, что край уже
// знает, поэтому «не задан» — решение оператора, и если он его не принял,
// процесс говорит об этом при старте, а не по одному отозванному токену за раз.
//
// ЭТОТ ФАЙЛ ВЛАДЕЕТ ОСЬЮ КЛАССА ОКРУЖЕНИЯ И ФОРМОЙ АДРЕСА. Прочие оси нашего
// авторитета — якорь доверия и клиентская пара — судятся в
// `own_lane_revocation_authority_test.go`: два места об одном предмете
// разъехались бы, и разъехалось бы то, где дефект ещё не нашли.
//
// ЗДЕСЬ СУДИЛАСЬ ОСЬ ЧУЖОГО ПОСТАВЩИКА — адрес его интроспекции, его
// административный адрес, якорь хопа к нему и посадка личности, разводившая
// требование второго. Ось снята вместе с самим поставщиком: требовать адресов
// там, где спрашивать некого, страж не вправе. Случаи не переписаны на новую
// ручку «чтобы сохранились» — каждый пересажен на ту ось, чей предмет жив, а
// то, чей предмет исчез (сверка ТОЧНОГО административного пути поставщика),
// снято вместе с ним.
package main

import (
	"strings"
	"testing"
)

const (
	// Адрес НАШЕГО авторитета отзыва — внутренний путь интроспекции службы
	// доступа. Годная форма: абсолютный, защищённый.
	tlsIntrospectURL = "https://kaname-internal.kacho.svc:9097/internal/tokens/introspect"
	// Тот же адрес открытым текстом — форма, которую производственный класс
	// обязан отвергать.
	plainIntrospectURL = "http://kaname-internal.kacho.svc:9097/internal/tokens/introspect"
)

// Производственный класс без адреса авторитета ОБЯЗАН быть отвергнут: без него
// край никогда не спрашивает, отозвано ли удостоверение, и отозванное живёт
// весь свой срок, что бы кто ни отзывал.
func TestProdRefusesUnsetRevocationAuthority(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{})
	if err == nil {
		t.Fatalf("expected refusal, got nil")
	}
	// Отказ обязан называть ручку И говорить, что именно выключено, а не просто
	// жаловаться на неразобранную строку: оператор, читающий журнал пода, обязан
	// знать, какое значение подать и почему оно важно.
	if !strings.Contains(err.Error(), platformRevocationURLKnob+" is empty") {
		t.Fatalf("the refusal must name the unset knob, got: %v", err)
	}
	if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("the refusal must say what stops working, got: %v", err)
	}
}

// Требование БЕЗУСЛОВНО, и это не ужесточение, а исчезновение второго ответа:
// прежде под посадкой `external` на вопрос об отзыве отвечал чужой поставщик.
// Его нет — разводить требование нечем, и посадка стражу больше не подаётся.
func TestTheDemandIsUnconditionalNotLaneScoped(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{})
	if err == nil {
		t.Fatal("незаданный авторитет отзыва обязан отвергать старт безусловно")
	}
	// Отказ не вправе ссылаться на посадку: развилки по ней больше нет, и
	// оператор, пошедший править посадку, правил бы не то.
	if strings.Contains(err.Error(), "=own") || strings.Contains(err.Error(), "=external") {
		t.Fatalf("отказ называет посадку, которой страж больше не судит: %v", err)
	}
}

// Пустая/незаданная метка окружения — производственный класс: забытая метка не
// вправе молча понижать стража (то же правило, что у соседнего стража прав).
func TestUnlabelledEnvIsProductionClass(t *testing.T) {
	if err := validateProductionRevocationConfig("", RevocationConfig{}); err == nil {
		t.Fatalf("an unset KACHO_APP_ENV must be treated as production-class, got nil")
	}
}

// Staging — тоже производственный класс.
func TestStagingIsProductionClass(t *testing.T) {
	if err := validateProductionRevocationConfig("staging", RevocationConfig{}); err == nil {
		t.Fatalf("expected refusal in staging, got nil")
	}
}

// Явные dev-метки терпят ненастроенную полосу отзыва — локальный стенд вправе
// подниматься без достижимого авторитета вовсе.
func TestDevClassToleratesUnsetAuthority(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		if err := validateProductionRevocationConfig(env, RevocationConfig{}); err != nil {
			t.Fatalf("%s: expected tolerance, got: %v", env, err)
		}
	}
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ВСЕГО СТРАЖА: на настроенной как надо полосе он обязан
// МОЛЧАТЬ. Страж, срабатывающий всегда, неотличим от срабатывающего наугад, и
// первый же ложный отказ его снимет.
func TestProdAcceptsAFullyConfiguredAuthority(t *testing.T) {
	err := validateProductionRevocationConfig("production", ourAuthorityWired())
	if err != nil {
		t.Fatalf("expected nil for a fully configured production revocation path, got: %v", err)
	}
}

// Дев-класс вправе иметь авторитет на открытом транспорте (нет внутреннего
// центра, проброс порта, сертификата нет вовсе). Требование к транспорту едет
// на том же дев-послаблении, что и остальной страж, — названо отдельным
// случаем, чтобы послабление было решением на бумаге, а не побочным эффектом.
func TestDevClassToleratesPlaintextHop(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		cfg := ourAuthorityWired()
		cfg.PlatformRevocationURL = plainIntrospectURL
		cfg.PlatformRevocationCAFile = ""
		if err := validateProductionRevocationConfig(env, cfg); err != nil {
			t.Fatalf("%s: expected tolerance for a plaintext hop, got: %v", env, err)
		}
	}
}

// Адрес без схемы отвергается: оператор, задавший ручке имя узла, узнаёт об
// этом при старте, а не на первом запросе.
func TestProdRefusesUnparseableAuthorityURL(t *testing.T) {
	cfg := ourAuthorityWired()
	cfg.PlatformRevocationURL = "kaname-internal.kacho.svc:9097/internal/tokens/introspect"
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatalf("expected refusal for a schemeless authority address, got nil")
	}
	if !strings.Contains(err.Error(), platformRevocationURLKnob) {
		t.Fatalf("the refusal must name the knob, got: %v", err)
	}
}

// ЗДЕСЬ СТОЯЛ СЛУЧАЙ «адрес целит в ПУБЛИЧНЫЙ API вместо административного».
// Он снят вместе со своим входом: сверка ТОЧНОГО административного пути была
// свойством адреса чужого поставщика, у нашего авторитета такого пути нет, и
// ветвь, вход которой непредставим, замолкает МОЛЧА — её отрицательный кейс
// зеленел бы на отказе соседа по форме адреса.
