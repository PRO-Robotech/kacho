// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package quotaceiling_test

// catalog_test.go — СТРАЖ МОЩНЕЙ: множество объявленных величин либо пустое,
// либо равное каталогу домена (приёмка `QUOTA-FATE-1`, §2.2а, стадия `S1`).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ БЛИЗНЕЦОВ ДВА, А НЕ ОДИН
//
// Страж судит МОЩНОСТЬ множества, и у такого стража отрицание не закрепляется
// одним положительным контролем: страж, падающий на «не полное», сломал бы
// `Q-07` (пустое законно); страж, падающий на «не пустое», сломал бы `Q-08`
// (полный каталог законен). Ни один близнец в одиночку предиката не закрепляет —
// поэтому их два, и оба стоят здесь.
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРИ ИСХОДА ОДНОЙ ВЕЛИЧИНЫ РАЗЛИЧИМЫ, И ЭТО НЕСУЩЕЕ
//
//	величина отсутствует у ЧАСТИ видов → отказ старта, названы ВСЕ необъявленные
//	величина отрицательна              → отказ старта, названы вид И значение
//	величина равна нулю                → СТАРТ ПРОХОДИТ: «этого вида не заводить»
//
// Без третьей строки ноль был бы неотличим от отсутствия, и оператор, желающий
// запретить вид, не смог бы этого выразить ни при каком вводе. Поэтому поле —
// указатель, а не число.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaceiling"
)

// probeConfig — посадка вымышленного домена о трёх видах.
type probeConfig struct {
	Alpha *int64
	Beta  *int64
	Gamma *int64
}

func probeCatalog() quotaceiling.Catalog[probeConfig] {
	return quotaceiling.Catalog[probeConfig]{
		{
			Key:   "quota.ceilings.alpha",
			Env:   "KACHO_PROBE_QUOTA__CEILINGS__ALPHA",
			Kind:  "probe.alpha",
			Why:   "сколько альф в проекте",
			Value: func(c probeConfig) *int64 { return c.Alpha },
		},
		{
			Key:   "quota.ceilings.beta",
			Env:   "KACHO_PROBE_QUOTA__CEILINGS__BETA",
			Kind:  "probe.beta",
			Why:   "сколько бет в проекте",
			Value: func(c probeConfig) *int64 { return c.Beta },
		},
		{
			Key:   "quota.ceilings.gamma",
			Env:   "KACHO_PROBE_QUOTA__CEILINGS__GAMMA",
			Kind:  "probe.gamma",
			Why:   "сколько гамм в проекте",
			Value: func(c probeConfig) *int64 { return c.Gamma },
		},
	}
}

func i64(v int64) *int64 { return &v }

// TestEmptySetIsLawful — ПЕРВЫЙ близнец (`Q-07`): посадка не объявила ни одной
// величины, и это законно.
//
// Пустое множество означает «потолков в этой установке нет» — сегодняшнее
// состояние всех пяти установок. Оно не снимает ни одного действующего потолка,
// потому что действующих нет ни одного (§2.2б приёмки).
func TestEmptySetIsLawful(t *testing.T) {
	t.Parallel()
	if err := probeCatalog().Validate(probeConfig{}); err != nil {
		t.Fatalf("пустое множество обязано быть законным (Q-07), получен отказ: %v", err)
	}
}

// TestFullCatalogIsLawfulIncludingZero — ВТОРОЙ близнец (`Q-08`): каталог
// объявлен целиком, и ноль среди величин законен.
//
// Ноль стоит намеренно: он пинит, что страж считает ОБЪЯВЛЕННОСТЬ, а не
// истинность величины. Страж, считающий ноль незаявленным, прошёл бы оба
// предыдущих исхода и упал бы здесь.
func TestFullCatalogIsLawfulIncludingZero(t *testing.T) {
	t.Parallel()
	cfg := probeConfig{Alpha: i64(3), Beta: i64(0), Gamma: i64(7)}
	if err := probeCatalog().Validate(cfg); err != nil {
		t.Fatalf("полный каталог обязан быть законным (Q-08), получен отказ: %v", err)
	}
}

// TestPartialSetIsRefusedNamingEveryUnstatedKind — `Q-02`: частично объявленный
// набор есть ОТКАЗ СТАРТА, и отказ называет КАЖДЫЙ необъявленный вид с его
// ручкой.
//
// Не первый из них: оператор, узнающий перечень по одному имени за перезапуск,
// платит перекатом за строку.
func TestPartialSetIsRefusedNamingEveryUnstatedKind(t *testing.T) {
	t.Parallel()
	cfg := probeConfig{Alpha: i64(3)} // объявлена одна из трёх
	err := probeCatalog().Validate(cfg)
	if err == nil {
		t.Fatalf("частичный набор обязан ронять старт (Q-02), отказа нет")
	}
	msg := err.Error()
	for _, want := range []string{
		"probe.beta", "quota.ceilings.beta", "KACHO_PROBE_QUOTA__CEILINGS__BETA",
		"probe.gamma", "quota.ceilings.gamma", "KACHO_PROBE_QUOTA__CEILINGS__GAMMA",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("отказ не называет %q; получено: %s", want, msg)
		}
	}
	// Объявленная величина в перечне необъявленных стоять не может.
	if strings.Contains(msg, "probe.alpha") {
		t.Errorf("отказ называет ОБЪЯВЛЕННЫЙ вид probe.alpha: %s", msg)
	}
}

// TestNegativeValueIsRefusedNamingKindAndValue — отрицательная величина роняет
// старт и называет вид И значение.
//
// Отрицательное НЕ означает «без ограничения»: такое прочтение сняло бы потолок
// величиной, похожей на опечатку. «Не заводить вовсе» выражается нулём.
func TestNegativeValueIsRefusedNamingKindAndValue(t *testing.T) {
	t.Parallel()
	cfg := probeConfig{Alpha: i64(3), Beta: i64(-1), Gamma: i64(7)}
	err := probeCatalog().Validate(cfg)
	if err == nil {
		t.Fatalf("отрицательная величина обязана ронять старт, отказа нет")
	}
	msg := err.Error()
	if !strings.Contains(msg, "probe.beta") || !strings.Contains(msg, "-1") {
		t.Errorf("отказ обязан назвать вид И значение; получено: %s", msg)
	}
}

// TestStatedCarriesDeclaredValuesIncludingZero — объявленные величины уезжают из
// настройки под своими видами, и ноль среди них сохраняется.
func TestStatedCarriesDeclaredValuesIncludingZero(t *testing.T) {
	t.Parallel()
	cfg := probeConfig{Alpha: i64(3), Beta: i64(0), Gamma: i64(7)}
	got := probeCatalog().Stated(cfg)
	want := map[string]int64{"probe.alpha": 3, "probe.beta": 0, "probe.gamma": 7}
	if len(got) != len(want) {
		t.Fatalf("объявленных величин %d, ожидалось %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("величина %s = %d, ожидалось %d", k, got[k], v)
		}
	}
}

// TestStatedIsEmptyWhenNothingDeclared — пустая посадка даёт пустое множество, а
// не карту нулей.
//
// Нуль-величина и необъявленная величина обязаны остаться различимыми и здесь:
// карта нулей объявила бы «ни одного ресурса ни одного вида», то есть ровно
// обратное сегодняшнему поведению.
func TestStatedIsEmptyWhenNothingDeclared(t *testing.T) {
	t.Parallel()
	if got := probeCatalog().Stated(probeConfig{}); len(got) != 0 {
		t.Fatalf("пустая посадка обязана давать пустое множество, получено: %v", got)
	}
}

// TestMalformedCatalogIsRefused — страж судит и СВОЮ таблицу: пустой каталог,
// повторяющийся вид и пропущенный доступ к полю — отказ.
//
// Без этого страж, чью таблицу испортили, зелен при любом входе: пустой каталог
// даёт «объявлено 0 из 0», то есть законное пустое множество на ЛЮБОЙ посадке.
func TestMalformedCatalogIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]quotaceiling.Catalog[probeConfig]{
		"пустой каталог": {},
		"повтор вида": {
			{Key: "a", Env: "A", Kind: "probe.alpha", Why: "x",
				Value: func(c probeConfig) *int64 { return c.Alpha }},
			{Key: "b", Env: "B", Kind: "probe.alpha", Why: "y",
				Value: func(c probeConfig) *int64 { return c.Beta }},
		},
		"нет доступа к полю": {
			{Key: "a", Env: "A", Kind: "probe.alpha", Why: "x"},
		},
	}
	for name, cat := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cat.Validate(probeConfig{}); err == nil {
				t.Fatalf("негодная таблица обязана ронять старт, отказа нет")
			}
		})
	}
}

// TestEnvNameIsDerivedFromKey — имя переменной ВЫВОДИТСЯ из ключа тем же
// правилом, каким его выводит viper, а не пишется вторым литералом.
//
// Выписанное второе имя расходится с первым молча, и переменная, названная
// текстом отказа, перестаёт доезжать до поля.
func TestEnvNameIsDerivedFromKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ prefix, key, want string }{
		{"KACHO_VPC", "quota.ceilings.network", "KACHO_VPC_QUOTA__CEILINGS__NETWORK"},
		{"KACHO_NLB", "quota.ceilings.network-load-balancers",
			"KACHO_NLB_QUOTA__CEILINGS__NETWORK_LOAD_BALANCERS"},
	} {
		if got := quotaceiling.EnvNameOf(tc.prefix, tc.key); got != tc.want {
			t.Errorf("EnvNameOf(%q, %q) = %q, ожидалось %q", tc.prefix, tc.key, got, tc.want)
		}
	}
}
