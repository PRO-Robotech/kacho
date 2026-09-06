// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

// secureProd — базовый production-Config, проходящий Validate(): используется как
// отправная точка, из которой каждый negative-кейс ослабляет ровно одно измерение.
func secureProd() config.Config {
	return config.Config{
		AuthMode:          "production",
		DBSSLMode:         "require",
		AuthZIAMGRPCAddr:  "kaname-internal:9091",
		ListFilterEnabled: true,
		// Объявление домена величин — часть законной посадки: у ручки ровно два
		// законных значения, и незаданное среди них не значится. Здесь стоит
		// «не развёрнут», потому что эта отправная точка удостоверений не
		// взводит вовсе: адрес без удостоверения был бы ВТОРЫМ ослаблением, и
		// красное ниже перестало бы означать то, что объявлено. Посадку с
		// поднятым ребром величин проверяет armedProd() соседнего файла.
		QuotaAuthority: corequota.NotDeployed,

		// Круг отправителей, которым разрешено передавать личность конечного
		// пользователя, обязан быть сужен — пустой список для corelib означает
		// «принимаем от любого пира с сертификатом», поэтому конфиг с пустым
		// списком БОЛЬШЕ НЕ является безопасной отправной точкой. Оба законных
		// отправителя: api-gateway (публичный :9090) и compute (внутренний :9091).
		AuthZTrustedForwarderSANs: []string{
			"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway",
			"spiffe://kacho.cloud/ns/kacho/sa/kacho-compute",
		},
		// Плоскость данных — часть БОЕВОЙ посадки, а не украшение: без неё
		// сверщик не запускается, ничто не переводит ресурс из намерения в
		// пригодность, и каждый том остаётся создаваемым навсегда при здоровом
		// рапорте сервиса. Поэтому безопасная отправная точка её несёт.
		BlockBackendKind:              "CEPH_RBD",
		BlockBackendInstallPrefix:     "kc7f",
		BlockBackendCredentialsDir:    "/etc/kacho/storage/credentials",
		BlockBackendCallTimeout:       30 * time.Second,
		BlockBackendReconcileInterval: 15 * time.Second,
		BlockBackendReconcileBatch:    100,

		GeoClientMTLS:      grpcclient.TLSClient{Enable: true},
		IAMClientMTLS:      grpcclient.TLSClient{Enable: true},
		PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
	}
}

// TestLoad_defaultAuthModeProduction — secure-by-default: без явного
// KACHO_STORAGE_AUTH_MODE бинарь резолвится в production (fail-closed), не dev.
// dev — явный opt-in (dev-профиль deploy-стенда выставляет его через env). Зеркалит
// iam/vpc/geo/nlb posture (security.md «любой деплой — production-mode»).
func TestLoad_defaultAuthModeProduction(t *testing.T) {
	var c config.Config
	if err := config.LoadInto(&c, map[string]string{
		"KACHO_STORAGE_DB_PASSWORD": "secret",
	}); err != nil {
		t.Fatalf("LoadInto err = %v", err)
	}
	if c.AuthMode != "production" {
		t.Fatalf("default auth mode = %q, want production (fail-closed by default)", c.AuthMode)
	}
}

// TestValidate_devTolerant — dev-режим осознанно терпит insecure-дефолты (plaintext
// DB, mTLS off, authz off): локальные фикстуры стартуют без kaname. Остаток
// стражи НЕ отказывает старту в dev (WARN эмитит serve.go, не fatal).
//
// Круг отправителей здесь БОЛЬШЕ НЕ считается: его стража переехала в конструктор
// дескриптора (`pkg/servicecontract`) и там срабатывает на ЛЮБОМ старте, а не
// только в боевом. Ослаблением это не является — наоборот: прежде круг судили два
// места, теперь одно, и оно общее на все сервисы. Проба на сам круг живёт рядом с
// дескриптором (cmd/storage/describe_test.go).
func TestValidate_devTolerant(t *testing.T) {
	c := config.Config{
		AuthMode:  "dev",
		DBSSLMode: "disable",
		// mTLS off, authz addr empty — всё insecure, но dev это допускает.
		//
		// Объявление домена величин при этом обязано быть: режимом оно не
		// смягчается — это не свойство посадки, а отсутствие выбора оператора.
		QuotaAuthority: corequota.NotDeployed,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("dev mode must tolerate insecure config, got err = %v", err)
	}
}

// TestValidate_productionSecureOK — полностью secure production-Config проходит.
func TestValidate_productionSecureOK(t *testing.T) {
	if err := secureProd().Validate(); err != nil {
		t.Fatalf("secure production config must validate, got err = %v", err)
	}
}

// Замки на sslmode до своей БД, транспорт ОБОИХ слушателей и ребро решения о
// доступе стояли ЗДЕСЬ и переехали в cmd/storage/describe_test.go вместе со своим
// предметом: эти измерения судит теперь конструктор дескриптора — один отказ на
// все сервисы вместо семи собственных.
//
// Переезд не ослабил ни одного из них, а два усилил, и это проверяется там же:
// транспорт спрашивается у САМОГО ТРАНСПОРТА (`Info().SecurityProtocol`), а не у
// ручки `Enable`, поэтому взведённая ручка с выродившимися креденшелами больше не
// проходит; а ребро решения о доступе обязано быть объявлено на ЛЮБОЙ посадке, а
// не только в боевой.

// TestValidate_unknownMode — незнакомый AuthMode → refuse (whitelist).
func TestValidate_unknownMode(t *testing.T) {
	c := secureProd()
	c.AuthMode = "prod" // typo
	if err := c.Validate(); err == nil {
		t.Fatal("unknown auth mode must refuse to start")
	}
}

// TestValidate_productionRequiresListFilter — production ОБЯЗАН нести включённый
// per-object list-filter публичного List. Per-RPC Check гейтит List лишь на
// project-tier `viewer`; сужение страницы до per-object `viewer` (то же отношение,
// что энфорсит Get) делает
// ТОЛЬКО фильтр. С выключенным фильтром любой член проекта видит КАЖДЫЙ том/снимок/
// образ проекта, включая объекты без per-object гранта (over-show / BOLA-lite,
// CWE-862). Fail-closed зеркалит требование mTLS и authz-адреса.
func TestValidate_productionRequiresListFilter(t *testing.T) {
	c := secureProd()
	c.ListFilterEnabled = false
	err := c.Validate()
	if err == nil {
		t.Fatal("production mode with LIST_FILTER_ENABLED=false must refuse to start " +
			"(public List would bypass the per-object filter)")
	}
	if !strings.Contains(err.Error(), "LIST_FILTER_ENABLED") {
		t.Fatalf("refusal must name the knob, got %v", err)
	}

	// production-strict — то же требование (не слабее).
	c = secureProd()
	c.AuthMode = "production-strict"
	c.ListFilterEnabled = false
	if err := c.Validate(); err == nil {
		t.Fatal("production-strict with LIST_FILTER_ENABLED=false must refuse to start")
	}
}

// TestLoad_listFilterDefaults — knob'ы фильтра дефолтятся в secure/production-форму:
// включён, fail-closed, реалистичный per-call дедлайн (см. authzfilter.DefaultConfig).
func TestLoad_listFilterDefaults(t *testing.T) {
	var c config.Config
	if err := config.LoadInto(&c, map[string]string{
		"KACHO_STORAGE_DB_PASSWORD": "secret",
	}); err != nil {
		t.Fatalf("LoadInto err = %v", err)
	}
	if !c.ListFilterEnabled {
		t.Fatal("list filter must default to enabled (secure-by-default)")
	}
	if c.ListFilterFailOpen {
		t.Fatal("list filter must default to fail-closed")
	}
	if c.ListFilterTimeoutMs != 1000 || c.ListFilterCacheTTLMs != 5000 || c.ListFilterCacheMaxEntries != 10000 {
		t.Fatalf("unexpected defaults: timeout=%d ttl=%d maxEntries=%d",
			c.ListFilterTimeoutMs, c.ListFilterCacheTTLMs, c.ListFilterCacheMaxEntries)
	}
}
