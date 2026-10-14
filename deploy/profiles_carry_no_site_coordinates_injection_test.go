// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Способность гейта координат площадки упасть и смолчать (kacho#3040).
//
// Каждая инъекция вносит ОДНУ координату в настоящий профиль дерева и ждёт
// находку ровно её класса; законный близнец меняет ровно один факт — ту же
// координату заглушкой либо ссылкой — и ждёт тишины. Адреса в пробах —
// диапазоны документации и зарезервированные домены, кроме одного
// маршрутизируемого адреса общедоступного резолвера: координатой площадки он
// не является ни у кого.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func coordInjectionBase(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(umbrellaDir, "values.prod.yaml"))
	if err != nil {
		t.Fatalf("основа инъекции не читается: %v", err)
	}
	return raw
}

func judgeInjected(t *testing.T, extra string) []coordFinding {
	t.Helper()
	raw := append(coordInjectionBase(t), []byte("\n"+extra+"\n")...)
	got, err := judgeProfile("инъекция", raw, &coordCensus{usedAddresses: map[string]bool{}})
	if err != nil {
		t.Fatalf("инъекция не разбирается: %v", err)
	}
	return got
}

func TestSiteCoordinateGate_InjectionsAreFoundAndTwinsAreSilent(t *testing.T) {
	if base := judgeInjected(t, ""); len(base) != 0 {
		t.Fatalf("основа инъекции уже несёт находки (%v) — инъекция ничего бы не доказала", base)
	}
	cases := []struct {
		name, extra, class string // class пуст — близнец, ждём тишины
	}{
		{"адрес балансировщика в комментарии", "# балансировщик стенда — 1.1.1.1", "маршрутизируемый адрес IPv4"},
		{"близнец: адрес документации в комментарии", "# балансировщик стенда — 203.0.113.7", ""},
		{"близнец: частная сеть значением", "probe:\n  host: 10.20.30.40", ""},
		{"адрес origin консоли значением", "probe:\n  appBaseURL: \"http://1.1.1.1\"", "маршрутизируемый адрес IPv4"},
		{"адрес почты на незарезервированном домене", "probe:\n  fromAddress: \"sender@mail.kacho.cloud\"", "адрес почты на незарезервированном домене"},
		{"близнец: тот же адрес на зарезервированном домене", "probe:\n  fromAddress: \"sender@mail.kacho.test\"", ""},
		{"учётка-адрес внутри строки подключения", "probe:\n  connectionURI: \"smtps://sender%40mail.kacho.cloud@relay.kacho.invalid:465/\"", "адрес почты на незарезервированном домене"},
		{"почтовый узел вне кластера", "probe:\n  connectionURI: \"smtps://relay.kacho.cloud:465/\"", "почтовый узел вне кластера и не заглушка"},
		{"учётка без домена на внешнем узле", "probe:\n  connectionURI: \"smtps://sender@relay.kacho.cloud:465/\"", "учётка в адресе почтового узла"},
		{"близнец: узел-заглушка с учёткой-заглушкой", "probe:\n  connectionURI: \"smtps://sender%40mail.kacho.invalid@relay.kacho.invalid:465/\"", ""},
		{"близнец: узел внутри кластера выражением шаблона", "probe:\n  connectionURI: 'smtp://{{ .Release.Name }}-mailpit:1025/'", ""},
		{"близнец: узел внутри кластера по имени службы", "# прежде: smtp://mailhog.kacho.svc:1025/", ""},
		{"почтовый узел вне кластера в комментарии", "# прежде: smtps://relay.kacho.cloud:465/", "почтовый узел вне кластера и не заглушка"},
	}
	for _, c := range cases {
		got := judgeInjected(t, c.extra)
		if c.class == "" {
			if len(got) != 0 {
				t.Errorf("%s: законный близнец дал находки: %v", c.name, got)
			}
			continue
		}
		found := false
		for _, f := range got {
			if f.Class == c.class {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: находки класса %q нет (получено %v) — гейт смолчал на координате", c.name, c.class, got)
		} else {
			t.Logf("%s: найдено, как ждали", c.name)
		}
	}
}

func TestSiteCoordinateGate_PublishedAddressIsSilentOnlyByExactMatch(t *testing.T) {
	used := map[string]bool{}
	if got := judgeCoordText("инъекция", 1, "contactEmail: security@kacho.cloud", used); len(got) != 0 {
		t.Errorf("адрес из перечня публикуемых дал находку: %v", got)
	}
	if !used["security@kacho.cloud"] {
		t.Errorf("адрес из перечня не отмечен использованным — проверка неиспользованных записей слепа")
	}
	if got := judgeCoordText("инъекция", 1, "contactEmail: root@kacho.cloud", nil); len(got) == 0 {
		t.Errorf("соседний адрес того же домена прошёл — перечень сужает по домену, а не по адресу")
	}
}

func TestSiteCoordinateGate_PasswordLiteralIsFoundOnlyOutsideTheDeveloperMachine(t *testing.T) {
	stacks := deployStacks(t)
	fold := func(name string, top map[string]any) map[string]any {
		var merged map[string]any
		for _, p := range stacks[name] {
			merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		return mergeValues(merged, top)
	}
	inject := map[string]any{"pg-vpc": map[string]any{"auth": map[string]any{"password": "probe-literal"}}}
	ref := map[string]any{"pg-vpc": map[string]any{"auth": map[string]any{"password": "", "existingSecret": "kacho-umbrella-pg-vpc"}}}

	base := judgeStackPasswords(map[string]map[string]any{"prod": fold("prod", nil)})
	if len(base) != 0 {
		t.Fatalf("основа инъекции уже несёт находки (%v)", base)
	}
	red := judgeStackPasswords(map[string]map[string]any{"prod": fold("prod", inject)})
	if len(red) != 1 || !strings.Contains(red[0], "pg-vpc.auth.password") {
		t.Errorf("пароль литералом в боевом стеке не найден ровно одной находкой: %v", red)
	}
	if twin := judgeStackPasswords(map[string]map[string]any{"prod": fold("prod", ref)}); len(twin) != 0 {
		t.Errorf("близнец (ссылка на секрет) дал находки: %v", twin)
	}
	if local := judgeStackPasswords(map[string]map[string]any{"dev": fold("dev", inject)}); len(local) != 0 {
		t.Errorf("близнец (тот же литерал на стенде машины разработчика) дал находки: %v", local)
	}
	expr := map[string]any{"pg-vpc": map[string]any{"auth": map[string]any{"password": "{{ .Values.x }}"}}}
	if e := judgeStackPasswords(map[string]map[string]any{"prod": fold("prod", expr)}); len(e) != 0 {
		t.Errorf("близнец (выражение шаблона) дал находки: %v", e)
	}
}

func TestSiteCoordinateGate_EmptyWalkIsARefusal(t *testing.T) {
	c := &coordCensus{usedAddresses: map[string]bool{}}
	got, err := judgeProfile("пусто", []byte(""), c)
	if err != nil {
		t.Fatalf("пустой вход не разобран: %v", err)
	}
	if len(got) != 0 || c.Scalars != 0 {
		t.Fatalf("пустой вход дал скаляры или находки: %d, %v", c.Scalars, got)
	}
	// Перепись по дереву обязана отказать на нуле скаляров — предпосылка, а не
	// чистота: тот же счётчик она и печатает.
}
