// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"strings"
	"testing"
	"time"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

// Страж старта обязан ОЦЕНИВАТЬ измерения плоскости данных, а не просто печатать
// их. Приёмка STOR-P-22, STOR-P-71, STOR-P-72.
//
// Форма проб парная везде: у каждого отрицания стоит положительный контроль. Без
// него проба зеленела бы на страже, отвергающем любую конфигурацию, — то есть на
// стороже, который так же бесполезен, как отсутствующий.

// baseProduction — боевая конфигурация БЕЗ плоскости данных: всё, что страж
// требует помимо предмета этих проб, здесь уже взведено, поэтому падение указывает
// ровно на предмет.
func baseProduction() Config {
	// Объявление домена величин — часть законной посадки: у ручки ровно два
	// законных значения, и незаданное среди них не значится. Отправная точка,
	// его не несущая, отличалась бы от законной ДВУМЯ фактами сразу.
	return Config{
		AuthMode:          "production",
		ListFilterEnabled: true,
		QuotaAuthority:    corequota.NotDeployed,
	}
}

// withBackend взводит ПОЛНЫЙ набор ручек плоскости данных. Состав назван в ОДНОМ
// месте намеренно: проба, перечисляющая обязательные ручки по частям, разошлась бы
// со стражем молча — а перечень стража обязан оставаться полным.
func withBackend(c Config) Config {
	c.BlockBackendKind = "CEPH_RBD"
	c.BlockBackendInstallPrefix = "kc7f"
	c.BlockBackendCredentialsDir = "/etc/kacho/storage/backend"
	c.BlockBackendCallTimeout = 30 * time.Second
	c.BlockBackendReconcileInterval = 15 * time.Second
	c.BlockBackendReconcileBatch = 100
	return c
}

func TestBootGuard_BackendConfigured_FullConfigStarts(t *testing.T) {
	t.Parallel()
	// Положительный контроль всей группы: полная конфигурация с бэкендом
	// поднимается. Без него любое отрицание ниже было бы неотличимо от стража,
	// который не пропускает ничего.
	if err := withBackend(baseProduction()).Validate(); err != nil {
		t.Fatalf("полная боевая конфигурация с плоскостью данных обязана подниматься: %v", err)
	}
}

func TestBootGuard_WithoutBackend_StartsInAnyMode(t *testing.T) {
	t.Parallel()
	// Здесь стояло ОБРАТНОЕ утверждение: боевая посадка без плоскости данных
	// обязана отказывать в старте. Оно продержалось ровно до сквозного прогона и
	// было неверно по определению продукта: Kachō — платформа ТОЛЬКО управляющей
	// плоскости, плоскости данных у неё нет by construction. Требование её
	// объявления означало, что ни один стенд без кластера хранения не делает том
	// пригодным, — и роняло чужие наборы, чья фикстура ждёт готового источника.
	//
	// Действующее правило проверяется здесь обеими посадками: вид не объявлен —
	// старт проходит; полнота ручек требуется ниже и только когда вид ОБЪЯВЛЕН.
	for _, mode := range []string{"production", "dev"} {
		c := baseProduction()
		c.AuthMode = mode
		if err := c.Validate(); err != nil {
			t.Fatalf("посадка %q без плоскости данных обязана подниматься: %v", mode, err)
		}
	}
}

func TestBootGuard_BackendWithoutInstallPrefix_RefusesToStart(t *testing.T) {
	t.Parallel()
	c := withBackend(baseProduction())
	c.BlockBackendInstallPrefix = ""

	err := c.Validate()
	if err == nil {
		t.Fatal("бэкенд без префикса установки обязан ронять старт")
	}
	// Сообщение адресовано ОПЕРАТОРУ: оно обязано назвать ручку, иначе стенд не
	// поднять. Это одно из трёх мест, выведенных из-под запрета на раскрытие.
	if !strings.Contains(err.Error(), "KACHO_STORAGE_BLOCK_BACKEND_INSTALL_PREFIX") {
		t.Errorf("отказ обязан называть ручку, а не только факт: %v", err)
	}
	if !strings.Contains(err.Error(), "adopt each other") {
		t.Errorf("отказ обязан называть последствие, иначе его снимут как непонятное: %v", err)
	}
}

func TestBootGuard_MalformedInstallPrefix_RefusesToStart(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"K", "kacho_storage", "7prefix", "оченьдлинныйпрефиксустановки"} {
		c := withBackend(baseProduction())
		c.BlockBackendInstallPrefix = bad
		if err := c.Validate(); err == nil {
			t.Errorf("префикс %q не соответствует форме и обязан ронять старт", bad)
		}
	}
	// Положительный контроль формы.
	for _, good := range []string{"kc", "kc7f", "prod01"} {
		c := withBackend(baseProduction())
		c.BlockBackendInstallPrefix = good
		if err := c.Validate(); err != nil {
			t.Errorf("префикс %q законен и обязан приниматься: %v", good, err)
		}
	}
}

func TestBootGuard_BackendWithoutCredentialsDir_RefusesToStart(t *testing.T) {
	t.Parallel()
	c := withBackend(baseProduction())
	c.BlockBackendCredentialsDir = ""

	err := c.Validate()
	if err == nil {
		t.Fatal("бэкенд без каталога учётного материала обязан ронять старт")
	}
	if !strings.Contains(err.Error(), "KACHO_STORAGE_BLOCK_BACKEND_CREDENTIALS_DIR") {
		t.Errorf("отказ обязан называть ручку: %v", err)
	}
}

func TestBootGuard_BackendWithoutCallTimeout_RefusesToStart(t *testing.T) {
	t.Parallel()
	c := withBackend(baseProduction())
	c.BlockBackendCallTimeout = 0

	if err := c.Validate(); err == nil {
		t.Fatal("бэкенд без срока обращения обязан ронять старт: неотвечающий пир иначе паркует слот сверщика навсегда")
	}
}

func TestBootGuard_BackendWithoutReconcilerCadence_RefusesToStart(t *testing.T) {
	t.Parallel()
	for name, mut := range map[string]func(*Config){
		"нет периода": func(c *Config) { c.BlockBackendReconcileInterval = 0 },
		"нет партии":  func(c *Config) { c.BlockBackendReconcileBatch = 0 },
	} {
		c := withBackend(baseProduction())
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: без сверщика ресурс НИКОГДА не доходит до готовности, а сервис отчитывается здоровым", name)
		}
	}
}

func TestBootGuard_AllProblemsReportedInOneRun(t *testing.T) {
	t.Parallel()
	// Оператор обязан увидеть ВЕСЬ перечень за один прогон, а не чинить по одной:
	// иначе поднятие стенда превращается в серию перезапусков.
	c := baseProduction()
	c.BlockBackendKind = "CEPH_RBD"
	// префикс, каталог, срок и каденция — все пусты одновременно

	err := c.Validate()
	if err == nil {
		t.Fatal("конфигурация обязана быть отвергнута")
	}
	for _, knob := range []string{
		"KACHO_STORAGE_BLOCK_BACKEND_INSTALL_PREFIX",
		"KACHO_STORAGE_BLOCK_BACKEND_CREDENTIALS_DIR",
		"KACHO_STORAGE_BLOCK_BACKEND_CALL_TIMEOUT",
		"KACHO_STORAGE_BLOCK_BACKEND_RECONCILE_INTERVAL",
	} {
		if !strings.Contains(err.Error(), knob) {
			t.Errorf("перечень обязан быть полным за один прогон, не хватает %s: %v", knob, err)
		}
	}
}

func TestBootGuard_DevModeDoesNotRequireBackendKnobs(t *testing.T) {
	t.Parallel()
	// dev-посадка — только локальные фикстуры. Требования боевого режима на неё
	// не распространяются, и это записанное решение, а не послабление на ходу.
	c := Config{AuthMode: "dev", BlockBackendKind: "CEPH_RBD",
		QuotaAuthority: corequota.NotDeployed}
	if err := c.Validate(); err != nil {
		t.Fatalf("dev-режим не обязан нести боевые требования: %v", err)
	}
}
