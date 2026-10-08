// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// Пробы стража ручек края (приёмка NTF-2, Р8 и сценарий 71; замысел issue-2917,
// З8, З9, З23). Ключи края — 19; таблица границ одна (`anon_mail_bounds.go`), и
// её читают и страж, и перепись варианта (т).

// r8EdgeKeyPatterns — ключи края ДОСЛОВНО по таблице Р8 приёмки, с фигурными
// скобками. Раскрытие — здесь, в пробе, а не в коде таблицы: таблица обязана
// совпасть с приёмкой, а не с самой собой.
var r8EdgeKeyPatterns = []string{
	"KACHO_API_GATEWAY_ANON_MAIL_IP_{FREE,POW,HARD}_LIMIT",
	"KACHO_API_GATEWAY_ANON_MAIL_IP_{FREE,POW,HARD}_WINDOW",
	"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_{BASE,HIGH}",
	"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_{V4_24,V6_56,V6_48}_{POW,HARD}_LIMIT",
	"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_{POW,HARD}_WINDOW",
	"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_{RATE_PER_SECOND,BURST}",
	"KACHO_API_GATEWAY_TRUSTED_HOPS",
}

// expandBraces раскрывает каждую группу `{a,b}` шаблона в отдельные ключи.
func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}
	}
	end := open + strings.IndexByte(pattern[open:], '}')
	var out []string
	for _, alt := range strings.Split(pattern[open+1:end], ",") {
		out = append(out, expandBraces(pattern[:open]+alt+pattern[end+1:])...)
	}
	return out
}

// baseProfile — ориентиры базового профиля (Р8): близнец всех отрицательных
// вариантов, каждый из которых меняет ровно один факт.
func baseProfile() map[string]string {
	return map[string]string{
		"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_LIMIT":           "3",
		"KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT":            "10",
		"KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_LIMIT":           "100",
		"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_WINDOW":          "15m",
		"KACHO_API_GATEWAY_ANON_MAIL_IP_POW_WINDOW":           "1h",
		"KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_WINDOW":          "1h",
		"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_BASE":           "16",
		"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH":           "20",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V4_24_POW_LIMIT":  "50",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V4_24_HARD_LIMIT": "500",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_56_POW_LIMIT":  "50",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_56_HARD_LIMIT": "500",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_POW_LIMIT":  "50",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_HARD_LIMIT": "1000",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_POW_WINDOW":       "1h",
		"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_HARD_WINDOW":      "1h",
		"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND":  "20",
		"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_BURST":            "100",
		"KACHO_API_GATEWAY_TRUSTED_HOPS":                      "1",
	}
}

// loadProfile кладёт профиль в окружение (снимая перечисленные ключи) и грузит
// конфигурацию ТЕМ ЖЕ загрузчиком, что у процесса.
func loadProfile(t *testing.T, env map[string]string, drop ...string) Config {
	t.Helper()
	for _, row := range edgeKnobTable {
		t.Setenv(row.name, "")
		if err := os.Unsetenv(row.name); err != nil {
			t.Fatalf("unset %s: %v", row.name, err)
		}
	}
	dropped := map[string]bool{}
	for _, k := range drop {
		dropped[k] = true
	}
	for k, v := range env {
		if !dropped[k] {
			t.Setenv(k, v)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("конфигурация не загрузилась: %v", err)
	}
	return cfg
}

func TestEdgeKnobTableIsTheNineteenEdgeKeysOfR8(t *testing.T) {
	var want []string
	for _, p := range r8EdgeKeyPatterns {
		want = append(want, expandBraces(p)...)
	}
	sort.Strings(want)
	got := make([]string, 0, len(edgeKnobTable))
	for _, row := range edgeKnobTable {
		got = append(got, row.name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("таблица границ края расходится с Р8:\n таблица: %v\n Р8:      %v", got, want)
	}
	if len(want) != 19 {
		t.Fatalf("Р8 объявляет у края 19 ключей, раскрытие дало %d", len(want))
	}
	t.Logf("ключей края в таблице границ: %d (Р8: 19)", len(got))
}

// TestEdgeKnobRowReadsItsOwnField — имя строки таблицы и поле настроек — одна
// ручка: метка, заданная окружением под ИМЕНЕМ строки, доезжает до геттера
// строки. У поля нет тега умолчания (Р8: «умолчаний в бинаре нет»).
func TestEdgeKnobRowReadsItsOwnField(t *testing.T) {
	for _, row := range edgeKnobTable {
		t.Setenv(row.name, "mark:"+row.name)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	typ := reflect.TypeOf(Config{})
	tagged := map[string]reflect.StructField{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if env := f.Tag.Get("envconfig"); env != "" {
			tagged[env] = f
		}
	}
	for _, row := range edgeKnobTable {
		if got := row.raw(cfg); got != "mark:"+row.name {
			t.Errorf("%s: геттер строки читает не своё поле (получено %q)", row.name, got)
		}
		f, ok := tagged[row.name]
		if !ok {
			t.Errorf("%s: поля настроек с таким именем нет", row.name)
			continue
		}
		if _, has := f.Tag.Lookup("default"); has {
			t.Errorf("%s: у поля %s есть тег умолчания — Р8 умолчаний в бинаре не допускает", row.name, f.Name)
		}
	}
	t.Logf("строк таблицы сверено с полями настроек: %d", len(edgeKnobTable))
}

func TestEdgeGuardAcceptsTheBaseProfile(t *testing.T) {
	cfg := loadProfile(t, baseProfile())
	got, err := ResolveEdgeLimits(cfg)
	if err != nil {
		t.Fatalf("базовый профиль отвергнут: %v", err)
	}
	if !got.TrustedHops.Parsed() || got.TrustedHops.Count() != 1 {
		t.Fatalf("число прыжков: %+v, ждали 1", got.TrustedHops)
	}
	want := AnonMailLimits{
		Source: AnonMailSourceLimits{
			Free: 3, PoW: 10, Hard: 100,
			FreeWindow: 15 * time.Minute, PoWWindow: time.Hour, HardWindow: time.Hour,
		},
		PoWBits:          AnonMailPoWBits{Base: 16, High: 20},
		SubnetV4Len24:    AnonMailSubnetLimits{PoW: 50, Hard: 500},
		SubnetV6Len56:    AnonMailSubnetLimits{PoW: 50, Hard: 500},
		SubnetV6Len48:    AnonMailSubnetLimits{PoW: 50, Hard: 1000},
		SubnetPoWWindow:  time.Hour,
		SubnetHardWindow: time.Hour,
		Global:           AnonMailGlobalFlow{RatePerSecond: 20, Burst: 100},
	}
	if got.AnonMail != want {
		t.Fatalf("разобранные пределы:\n получено %+v\n ждали    %+v", got.AnonMail, want)
	}
}

// TestEdgeGuardRefusesEachAbsentKey — вариант (т) сценария 71: по варианту на
// КАЖДЫЙ ключ края, где снят только он. Перечень — таблица границ.
func TestEdgeGuardRefusesEachAbsentKey(t *testing.T) {
	n := 0
	for _, row := range edgeKnobTable {
		cfg := loadProfile(t, baseProfile(), row.name)
		_, err := ResolveEdgeLimits(cfg)
		if err == nil {
			t.Errorf("снята %s — старт не отвергнут", row.name)
			continue
		}
		if !strings.Contains(err.Error(), row.name+" не задана") {
			t.Errorf("снята %s — сообщение не называет ключ и его отсутствие: %v", row.name, err)
		}
		n++
	}
	t.Logf("вариантов (т) у края: %d из %d ключей", n, len(edgeKnobTable))
	if n != 19 {
		t.Fatalf("вариантов (т) у края должно быть 19, отвергнуто %d", n)
	}
}

// TestEdgeGuardNamesKeyAndBound — варианты края сценария 71 и граница каждой
// строки Р8 выборкой. Каждый вариант меняет ОДИН факт против базового профиля.
func TestEdgeGuardNamesKeyAndBound(t *testing.T) {
	cases := []struct {
		name    string
		set     map[string]string
		drop    string
		mention []string
	}{
		{name: "(г) снята IP_HARD_LIMIT", drop: "KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_LIMIT",
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_LIMIT не задана"}},
		{name: "(д) FREE > POW", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_LIMIT": "11"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_LIMIT = 11", "KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT = 10", "FREE ≤ POW"}},
		{name: "(е) не задано число прыжков", drop: "KACHO_API_GATEWAY_TRUSTED_HOPS",
			mention: []string{"KACHO_API_GATEWAY_TRUSTED_HOPS не задана"}},
		{name: "(ж) снята GLOBAL_RATE_PER_SECOND", drop: "KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND",
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND не задана"}},
		{name: "(з) GLOBAL_RATE_PER_SECOND = 0", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND": "0"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND = 0", "> 0"}},
		{name: "(и) BURST < RATE", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_BURST": "19"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_BURST = 19", "KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND = 20", "BURST ≥ RATE"}},
		{name: "(к) /48 POW > HARD", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_POW_LIMIT": "1001"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_POW_LIMIT = 1001", "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_HARD_LIMIT = 1000", "POW ≤ HARD"}},
		{name: "(р) BITS_BASE = BITS_HIGH", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_BASE": "20"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_BASE = 20", "KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH = 20", "BASE < HIGH"}},
		{name: "(с) снята POW_BITS_HIGH", drop: "KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH",
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH не задана"}},
		{name: "(х) IP_FREE_WINDOW > IP_POW_WINDOW", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_WINDOW": "90m"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_WINDOW = 1h30m0s", "KACHO_API_GATEWAY_ANON_MAIL_IP_POW_WINDOW = 1h0m0s", "W_F ≤ W_P"}},
		{name: "(ц) SUBNET_HARD_WINDOW = 25h", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_HARD_WINDOW": "25h"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_HARD_WINDOW = 25h0m0s", "24h0m0s"}},
		// Выборка по прочим строкам Р8 — не меньше варианта на строку.
		{name: "источник: FREE = 0", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_LIMIT": "0"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_LIMIT = 0", "≥ 1"}},
		{name: "источник: POW > HARD", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT": "101"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT = 101", "POW ≤ HARD"}},
		{name: "источник: W_F < 1 мин", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_WINDOW": "59s"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_WINDOW = 59s", "1m0s"}},
		{name: "источник: HARD / W_H > 1000/ч", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_LIMIT": "1001"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_LIMIT = 1001", "KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_WINDOW = 1h0m0s", "1000/ч"}},
		{name: "сложность: BASE < 8", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_BASE": "7"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_BASE = 7", "8"}},
		{name: "сложность: HIGH > 24", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH": "25"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH = 25", "24"}},
		{name: "подсеть: W_Ps > W_Hs", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_POW_WINDOW": "2h"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_POW_WINDOW = 2h0m0s", "W_Ps ≤ W_Hs"}},
		{name: "подсеть: /24 HARD / W_Hs > 10000/ч", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V4_24_HARD_LIMIT": "10001"},
			mention: []string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V4_24_HARD_LIMIT = 10001", "10000/ч"}},
		{name: "прыжки: отрицательное", set: map[string]string{"KACHO_API_GATEWAY_TRUSTED_HOPS": "-1"},
			mention: []string{"KACHO_API_GATEWAY_TRUSTED_HOPS = -1", "≥ 0"}},
		{name: "счёт: не целое", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT": "ten"},
			mention: []string{`KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT = "ten"`, "целое"}},
		{name: "окно: не длительность", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_POW_WINDOW": "60"},
			mention: []string{`KACHO_API_GATEWAY_ANON_MAIL_SUBNET_POW_WINDOW = "60"`, "длительность"}},
		{name: "темп: не число", set: map[string]string{"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND": "NaN"},
			mention: []string{`KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND = "NaN"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := baseProfile()
			for k, v := range tc.set {
				env[k] = v
			}
			var drop []string
			if tc.drop != "" {
				drop = append(drop, tc.drop)
			}
			_, err := ResolveEdgeLimits(loadProfile(t, env, drop...))
			if err == nil {
				t.Fatalf("старт не отвергнут")
			}
			for _, m := range tc.mention {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("сообщение не называет %q:\n%v", m, err)
				}
			}
		})
	}
	t.Logf("отрицательных вариантов края: %d", len(cases))
}

// TestEdgeGuardBoundsAreInclusiveWhereR8SaysSo — близнецы границ: ровно на
// границе значение принимается (≤, ≥), иначе проба выше краснела бы на чём
// угодно.
func TestEdgeGuardBoundsAreInclusiveWhereR8SaysSo(t *testing.T) {
	cases := map[string]map[string]string{
		"FREE = POW = HARD":          {"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_LIMIT": "100", "KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT": "100"},
		"HARD / W_H = 1000/ч":        {"KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_LIMIT": "1000"},
		"окна 1 мин и 24 ч":          {"KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_WINDOW": "1m", "KACHO_API_GATEWAY_ANON_MAIL_IP_POW_WINDOW": "24h", "KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_WINDOW": "24h"},
		"биты 8 и 24":                {"KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_BASE": "8", "KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH": "24"},
		"/48 HARD / W_Hs = 10000/ч":  {"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_HARD_LIMIT": "10000"},
		"BURST = RATE":               {"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_BURST": "20"},
		"прыжков 0":                  {"KACHO_API_GATEWAY_TRUSTED_HOPS": "0"},
		"дробный темп":               {"KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND": "0.5", "KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_BURST": "1"},
		"POW подсети = HARD подсети": {"KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V4_24_POW_LIMIT": "500"},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			env := baseProfile()
			for k, v := range set {
				env[k] = v
			}
			if _, err := ResolveEdgeLimits(loadProfile(t, env)); err != nil {
				t.Fatalf("значение на границе отвергнуто: %v", err)
			}
		})
	}
}

// TestEdgeLimitsPrintEffectiveValues — близнец сценария 71: в журнале старта
// напечатаны действующие значения всех ручек края.
func TestEdgeLimitsPrintEffectiveValues(t *testing.T) {
	limits, err := ResolveEdgeLimits(loadProfile(t, baseProfile()))
	if err != nil {
		t.Fatalf("базовый профиль отвергнут: %v", err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("edge limits", "limits", limits)
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("запись журнала не разбирается: %v\n%s", err, buf.String())
	}
	group, ok := rec["limits"].(map[string]any)
	if !ok {
		t.Fatalf("значения ручек не напечатаны группой: %s", buf.String())
	}
	for k, v := range baseProfile() {
		if group[k] != v {
			t.Errorf("журнал: %s = %v, ждали %q", k, group[k], v)
		}
	}
	if len(group) != len(edgeKnobTable) {
		t.Errorf("напечатано ручек %d, в таблице %d", len(group), len(edgeKnobTable))
	}
	t.Logf("напечатано ручек края: %d", len(group))
}

func TestAnonMailWindowUpperBoundIsTheTableCeiling(t *testing.T) {
	if got := AnonMailWindowUpperBound(); got != 24*time.Hour {
		t.Fatalf("верхняя граница окон края = %s, Р8 объявляет 24 ч", got)
	}
}

func TestParseTrustedHops(t *testing.T) {
	for _, bad := range []string{"", "  ", "-1", "one", "1.5"} {
		if _, err := ParseTrustedHops(bad); err == nil || !strings.Contains(err.Error(), TrustedHopsKnob) {
			t.Errorf("ParseTrustedHops(%q): ждали отказ с именем ручки, получено %v", bad, err)
		}
	}
	for raw, want := range map[string]int{"0": 0, "1": 1, " 3 ": 3} {
		h, err := ParseTrustedHops(raw)
		if err != nil || !h.Parsed() || h.Count() != want {
			t.Errorf("ParseTrustedHops(%q) = %+v, %v; ждали %d", raw, h, err, want)
		}
	}
	var zero TrustedHops
	if zero.Parsed() {
		t.Fatal("нулевое значение TrustedHops объявляет себя построенным разбором")
	}
}

// TestAnonMailPoWKeyRefusesStartWithoutAKey — ключ PoW (З9, CX2-13): без ручки,
// без файла и с ключом короче 32 байт — отказ старта с именем ручки; 32 байта —
// принято, ключ не печатается.
func TestAnonMailPoWKeyRefusesStartWithoutAKey(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, bytes.Repeat([]byte{'k'}, n), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	short, exact := write("short", 31), write("exact", 32)
	for name, path := range map[string]string{
		"ручка не задана": "",
		"файла нет":       filepath.Join(dir, "absent"),
		"31 байт":         short,
	} {
		_, err := ReadAnonMailPoWKey(Config{AnonMailPoWKeyFile: path})
		if err == nil || !strings.Contains(err.Error(), AnonMailPoWKeyFileKnob) {
			t.Errorf("%s: ждали отказ с именем ручки, получено %v", name, err)
		}
	}
	_, err := ReadAnonMailPoWKey(Config{AnonMailPoWKeyFile: short})
	if err == nil || !strings.Contains(err.Error(), "32") {
		t.Errorf("короткий ключ: сообщение не называет границу 32 байт: %v", err)
	}
	key, err := ReadAnonMailPoWKey(Config{AnonMailPoWKeyFile: exact})
	if err != nil {
		t.Fatalf("ключ в 32 байта отвергнут: %v", err)
	}
	if len(key.Bytes()) != 32 {
		t.Fatalf("ключ прочитан длиной %d", len(key.Bytes()))
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("k", "key", key)
	if strings.Contains(buf.String(), strings.Repeat("k", 32)) {
		t.Fatalf("ключ PoW попал в журнал: %s", buf.String())
	}
}
