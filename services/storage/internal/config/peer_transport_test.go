// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// peer_transport_test.go — поведенческий замок на транспорт ИСХОДЯЩИХ рёбер.
//
// Класс, который эти пробы закрывают: ручка транспорта у ребра есть, проводка её
// читает, а отказа в старте при невзведённой ручке нет. Клиентские creds на
// Enable=false вырождаются в insecure БЕЗ ошибки (pkg/grpcclient), поэтому
// процесс поднимается, отчитывается «authz interceptor enabled» — и каждый Check
// уходит по открытому каналу. Контроль присутствует и не отказывает ни разу за
// свою жизнь.
//
// Отрицания идут ПАРАМИ с положительным контролем: «отвергнуто» без «законное
// проходит» неотличимо от стража, который отвергает всё.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

// armedProd — production-Config, у которого ВСЕ исходящие рёбра и подняты
// (непустой адрес), и защищены. Отправная точка: каждый negative-кейс ослабляет
// ровно одно измерение. Отличается от secureProd() соседнего файла тем, что
// поднимает и рёбра geo/project — там они не заданы, и потому не проверяются.
func armedProd() config.Config {
	c := secureProd()
	c.IAMGRPCAddr = "kaname:9090"
	c.GeoGRPCAddr = "kacho-geo:9090"
	c.IAMClientMTLS = grpcclient.TLSClient{Enable: true}
	c.GeoClientMTLS = grpcclient.TLSClient{Enable: true}
	return c
}

// Положительный контроль: полностью взведённая production-посадка проходит.
// Без него любое отрицание ниже зеленело бы и на страже, отвергающем всё.
func TestValidate_ProductionAcceptsArmedPeerEdges(t *testing.T) {
	if err := armedProd().Validate(); err != nil {
		t.Fatalf("production config with every peer edge armed must validate, got err = %v", err)
	}
}

// Ребро, по которому принимается решение о доступе (per-RPC Check на обоих
// листенерах, фильтр видимости, регистрация владельца), обязано нести
// проверяемый транспорт. Отказ обязан называть ручку — его читает оператор,
// поднимающий стенд.
func TestValidate_ProductionRefusesUnverifiedIAMEdge(t *testing.T) {
	c := armedProd()
	c.IAMClientMTLS.Enable = false
	err := c.Validate()
	if err == nil {
		t.Fatal("production with the storage→iam edge dialled over cleartext must refuse to start, got nil")
	}
	if want := "KACHO_STORAGE_IAM_CLIENT_MTLS_ENABLE"; !strings.Contains(err.Error(), want) {
		t.Errorf("refusal must name the knob %q; got: %s", want, err)
	}
}

// К3 п.2: отказ обязан быть и в PLAIN production, не только в строгом режиме.
// Проверка именно строгого режима стоит отдельно, потому что «гейт только в
// strict» — это ровно та посадка, из которой класс и вырос: боевой стенд по
// умолчанию идёт в plain production.
func TestValidate_ProductionStrictRefusesUnverifiedIAMEdge(t *testing.T) {
	c := armedProd()
	c.AuthMode = "production-strict"
	c.IAMClientMTLS.Enable = false
	if err := c.Validate(); err == nil {
		t.Fatal("production-strict with the storage→iam edge dialled over cleartext must refuse to start, got nil")
	}
}

// Ребро storage→geo несёт валидацию размещения (зона тома/образа). Открытый
// канал здесь означает подменённое существование чужой зоны, то есть обход
// проверки когерентности — не «чуть слабее», а другое решение.
func TestValidate_ProductionRefusesUnverifiedGeoEdge(t *testing.T) {
	c := armedProd()
	c.GeoClientMTLS.Enable = false
	err := c.Validate()
	if err == nil {
		t.Fatal("production with the storage→geo edge dialled over cleartext must refuse to start, got nil")
	}
	if want := "KACHO_STORAGE_GEO_CLIENT_MTLS_ENABLE"; !strings.Contains(err.Error(), want) {
		t.Errorf("refusal must name the knob %q; got: %s", want, err)
	}
}

// «Страж видит ребро ⟺ ребро дилится»: незаданный адрес означает, что проводка
// (dialPeer) соединение не поднимает вовсе, — требовать на нём транспорт не о
// чем. Без этой пробы страж превратился бы в требование задать ручку ребра,
// которого нет, и профиль без geo перестал бы стартовать без причины.
func TestValidate_ProductionIgnoresPeerEdgeThatIsNotDialled(t *testing.T) {
	c := armedProd()
	c.GeoGRPCAddr = ""
	c.GeoClientMTLS.Enable = false
	if err := c.Validate(); err != nil {
		t.Fatalf("an edge with no address is never dialled; the guard must not demand its transport, got err = %v", err)
	}
}

// dev осознанно терпит невзведённый транспорт — только для in-process фикстур.
// На РАЗВЁРНУТОМ стенде dev-посадка запрещена отдельным правилом
// (security.md §«Production-mode обязателен ВЕЗДЕ»), поэтому послабление здесь
// не расширяет поверхность стенда.
func TestValidate_DevToleratesUnverifiedPeerEdges(t *testing.T) {
	c := armedProd()
	c.AuthMode = "dev"
	c.DBSSLMode = "disable"
	c.IAMClientMTLS.Enable = false
	c.GeoClientMTLS.Enable = false
	if err := c.Validate(); err != nil {
		t.Fatalf("dev mode must tolerate unverified peer edges (in-process fixtures), got err = %v", err)
	}
}
