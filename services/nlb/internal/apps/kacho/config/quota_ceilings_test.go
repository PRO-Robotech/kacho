// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_ceilings_test.go — ВЕЛИЧИНЫ ПОТОЛКОВ ОБЪЯВЛЯЕТ ПОСАДКА ДОМЕНА (приёмка
// `QUOTA-FATE-1`, стадия `S1`; сценарии `Q-02`, `Q-03`, `Q-07`, `Q-08`).

import (
	"strings"
	"testing"
)

// nlbCeilingsYAML — форма файла, которой посадка объявляет ТРИ величины каталога.
// Ноль стоит намеренно: он законная величина, и проба обязана его пронести.
const nlbCeilingsYAML = minimalValidYAML + `
quota:
  authority: not-deployed
  ceilings:
    network-load-balancers: 4
    listeners: 0
    target-groups: 6
`

// TestCeilingFileKeysArmTheFields — ключи файла ДОЕЗЖАЮТ до полей, включая явный
// ноль.
//
// Опечатка в ключе выглядит В ТОЧНОСТИ как «посадка величину не объявила», и без
// этой пробы оператор не отличил бы свою ошибку от нашей.
func TestCeilingFileKeysArmTheFields(t *testing.T) {
	cfg, err := Load(writeYAML(t, nlbCeilingsYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	stated := QuotaCeilingCatalog.Stated(cfg.Quota.Ceilings)
	if len(stated) != len(QuotaCeilingCatalog) {
		t.Fatalf("объявлено величин %d, ожидалось %d: %v",
			len(stated), len(QuotaCeilingCatalog), stated)
	}
	if got := stated["loadbalancer.networkLoadBalancers"]; got != 4 {
		t.Errorf("loadbalancer.networkLoadBalancers = %d, ожидалось 4", got)
	}
	if got, ok := stated["loadbalancer.listeners"]; !ok || got != 0 {
		t.Errorf("ноль обязан доехать как ноль: got=%d ok=%v", got, ok)
	}
	if err := cfg.ValidateQuotaCeilings(); err != nil {
		t.Errorf("полный каталог законен (Q-08), получен отказ: %v", err)
	}
}

// TestCeilingEnvVarsArmTheFields — переменная, названная ТЕКСТОМ ОТКАЗА,
// доезжает до поля.
//
// `AutomaticEnv` разрешает переменную только для ключа, который viper УЖЕ знает;
// умолчания у этих ключей нет намеренно, поэтому привязка ЯВНАЯ. Без неё отказ
// выглядит исчерпывающим и не восстанавливает следующий шаг.
func TestCeilingEnvVarsArmTheFields(t *testing.T) {
	for _, k := range QuotaCeilingCatalog {
		t.Setenv(k.Env, "5")
	}
	cfg, err := Load(writeYAML(t, minimalValidYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	stated := QuotaCeilingCatalog.Stated(cfg.Quota.Ceilings)
	if len(stated) != len(QuotaCeilingCatalog) {
		t.Fatalf("каждая переменная каталога обязана доехать до своего поля: %v", stated)
	}
	for _, kind := range QuotaCeilingCatalog.Kinds() {
		if stated[kind] != 5 {
			t.Errorf("вид %s = %d, ожидалось 5", kind, stated[kind])
		}
	}
}

// TestEmptyCeilingSetIsLawful — ПЕРВЫЙ близнец (`Q-07`): пустое множество
// законно, процесс поднимается, страж молчит.
func TestEmptyCeilingSetIsLawful(t *testing.T) {
	cfg, err := Load(writeYAML(t, minimalValidYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := QuotaCeilingCatalog.Stated(cfg.Quota.Ceilings); len(got) != 0 {
		t.Fatalf("пустая посадка обязана давать пустое множество: %v", got)
	}
	if err := cfg.ValidateQuotaCeilings(); err != nil {
		t.Errorf("пустое множество законно (Q-07), получен отказ: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("пустое множество не роняет общий страж: %v", err)
	}
}

// TestPartialCeilingSetRefusesStartNamingEveryUnstatedKind — `Q-02`: частичный
// набор роняет старт и называет КАЖДЫЙ необъявленный вид с его ручкой.
func TestPartialCeilingSetRefusesStartNamingEveryUnstatedKind(t *testing.T) {
	partial := strings.Replace(nlbCeilingsYAML, "    listeners: 0\n", "", 1)
	partial = strings.Replace(partial, "    target-groups: 6\n", "", 1)
	// Разбор этой службы зовёт стража сам, поэтому отказ приходит ИЗ `Load` —
	// то есть до того, как процесс поднимется, и это ровно исход `Q-02`.
	cfg, err := Load(writeYAML(t, partial))
	if err == nil {
		t.Fatalf("частичный набор обязан ронять старт (Q-02); Load вернул %+v", cfg.Quota)
	}
	msg := err.Error()
	for _, want := range []string{
		"loadbalancer.listeners", "quota.ceilings.listeners",
		"KACHO_NLB_QUOTA__CEILINGS__LISTENERS",
		"loadbalancer.targetGroups", "quota.ceilings.target-groups",
		"KACHO_NLB_QUOTA__CEILINGS__TARGET_GROUPS",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("отказ не называет %q; получено: %s", want, msg)
		}
	}
	// Объявленный вид в перечне НЕобъявленных стоять не может.
	if strings.Contains(msg, "loadbalancer.networkLoadBalancers") {
		t.Errorf("отказ называет ОБЪЯВЛЕННЫЙ вид: %s", msg)
	}
}

// TestQuotaCeilingCatalogShapeIsSound — таблица величин судится сама.
func TestQuotaCeilingCatalogShapeIsSound(t *testing.T) {
	if err := QuotaCeilingCatalog.Shape(); err != nil {
		t.Fatalf("таблица величин негодна: %v", err)
	}
	for _, k := range QuotaCeilingCatalog {
		if !strings.HasPrefix(k.Kind, "loadbalancer.") {
			t.Errorf("вид %s не принадлежит каталогу домена", k.Kind)
		}
	}
}
