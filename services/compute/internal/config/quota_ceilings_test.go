// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// quota_ceilings_test.go — ВЕЛИЧИНЫ ПОТОЛКОВ ОБЪЯВЛЯЕТ ПОСАДКА ДОМЕНА (приёмка
// судьбы авторитета величин, стадия S1; сценарии Q-02, Q-03, Q-07, Q-08).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ БЛИЗНЕЦОВ ДВА, А НЕ ОДИН
//
// Страж судит МОЩНОСТЬ множества: страж, падающий на «не полное», сломал бы
// Q-07 (пустое законно); страж, падающий на «не пустое», сломал бы Q-08 (полный
// каталог законен). Ни один близнец в одиночку предиката не закрепляет.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// baseCeilingEnv — обязательные ручки разбора, к предмету пробы не относящиеся:
// без них разбор отказывает раньше стража величин, и проба судила бы не то.
func baseCeilingEnv() map[string]string {
	return map[string]string{"KACHO_COMPUTE_DB_PASSWORD": "probe"}
}

// fullCeilingEnv — каталог объявлен ЦЕЛИКОМ; ноль стоит намеренно: он законная
// величина, и проба обязана его пронести.
func fullCeilingEnv() map[string]string {
	out := baseCeilingEnv()
	for i, k := range config.QuotaCeilingCatalog {
		if i == 0 {
			out[k.Env] = "0"
			continue
		}
		out[k.Env] = "7"
	}
	return out
}

// TestCeilingEnvVarsArmTheFields — переменная, названная ТЕКСТОМ ОТКАЗА,
// доезжает до своего поля, включая явный ноль.
//
// Имя переменной стоит и в таблице, и в теге разбора; без этой пробы второе
// написание расходится с первым МОЛЧА, и отказ называет переменную, которая до
// поля не доезжает. Перечень выводится ИЗ ТАБЛИЦЫ, а не выписывается.
func TestCeilingEnvVarsArmTheFields(t *testing.T) {
	cfg := loadCfg(t, fullCeilingEnv())
	stated := config.QuotaCeilingCatalog.Stated(cfg.QuotaCeilings)
	if len(stated) != len(config.QuotaCeilingCatalog) {
		t.Fatalf("каждая переменная каталога обязана доехать до своего поля: %v", stated)
	}
	zeroKind := config.QuotaCeilingCatalog[0].Kind
	if got, ok := stated[zeroKind]; !ok || got != 0 {
		t.Errorf("ноль обязан доехать как ноль (вид %s): got=%d ok=%v", zeroKind, got, ok)
	}
	if err := cfg.ValidateQuotaCeilings(); err != nil {
		t.Errorf("полный каталог законен (Q-08), получен отказ: %v", err)
	}
}

// TestEmptyCeilingSetIsLawful — ПЕРВЫЙ близнец (Q-07): посадка не объявляет ни
// одной величины, страж молчит, поведение остаётся сегодняшним.
func TestEmptyCeilingSetIsLawful(t *testing.T) {
	cfg := loadCfg(t, baseCeilingEnv())
	if got := config.QuotaCeilingCatalog.Stated(cfg.QuotaCeilings); len(got) != 0 {
		t.Fatalf("пустая посадка обязана давать пустое множество: %v", got)
	}
	if err := cfg.ValidateQuotaCeilings(); err != nil {
		t.Errorf("пустое множество законно (Q-07), получен отказ: %v", err)
	}
}

// TestPartialCeilingSetIsRefusedNamingEveryUnstatedKind — Q-02: частичный набор
// роняет старт и называет КАЖДЫЙ необъявленный вид с его ручкой, а не первый из
// них: оператор, узнающий перечень по одному имени за перезапуск, платит
// перекатом за строку.
func TestPartialCeilingSetIsRefusedNamingEveryUnstatedKind(t *testing.T) {
	env := fullCeilingEnv()
	// Объявлена ровно одна величина — остальные сняты.
	for i, k := range config.QuotaCeilingCatalog {
		if i > 0 {
			delete(env, k.Env)
		}
	}
	cfg := loadCfg(t, env)
	err := cfg.ValidateQuotaCeilings()
	if err == nil {
		t.Fatalf("частичный набор обязан ронять старт (Q-02)")
	}
	msg := err.Error()
	for i, k := range config.QuotaCeilingCatalog {
		if i == 0 {
			if strings.Contains(msg, k.Kind) {
				t.Errorf("отказ называет ОБЪЯВЛЕННЫЙ вид %s: %s", k.Kind, msg)
			}
			continue
		}
		if !strings.Contains(msg, k.Kind) {
			t.Errorf("отказ не называет вид %s: %s", k.Kind, msg)
		}
		if !strings.Contains(msg, k.Env) {
			t.Errorf("отказ не называет ручку %s: %s", k.Env, msg)
		}
	}
	// Ни одно умолчание не подставлено объявленной величине.
	if got := config.QuotaCeilingCatalog.Stated(cfg.QuotaCeilings); len(got) != 1 {
		t.Errorf("объявленных величин %d, ожидалась одна: %v", len(got), got)
	}
}

// TestNegativeCeilingIsRefused — отрицательная величина роняет старт: она НЕ
// означает «без ограничения», такое прочтение сняло бы потолок значением,
// похожим на опечатку. «Не заводить вовсе» выражается нулём.
func TestNegativeCeilingIsRefused(t *testing.T) {
	env := fullCeilingEnv()
	env[config.QuotaCeilingCatalog[0].Env] = "-1"
	cfg := loadCfg(t, env)
	err := cfg.ValidateQuotaCeilings()
	if err == nil {
		t.Fatalf("отрицательная величина обязана ронять старт")
	}
	if !strings.Contains(err.Error(), "-1") {
		t.Errorf("отказ обязан назвать значение: %s", err.Error())
	}
}

// TestQuotaCeilingCatalogShapeIsSound — таблица величин судится сама: непуста,
// виды и ручки не повторяются, у каждой записи есть доступ к полю и довод.
func TestQuotaCeilingCatalogShapeIsSound(t *testing.T) {
	if err := config.QuotaCeilingCatalog.Shape(); err != nil {
		t.Fatalf("таблица величин негодна: %v", err)
	}
	for _, k := range config.QuotaCeilingCatalog {
		if !strings.HasPrefix(k.Kind, "compute.") {
			t.Errorf("вид %s не принадлежит каталогу домена", k.Kind)
		}
	}
}
