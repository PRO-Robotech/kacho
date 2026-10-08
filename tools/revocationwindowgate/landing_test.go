// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// landing_test.go — величина обязательной ручки окна берётся из посадки.
//
// # Предмет
//
// Ручка окна без умолчания в исходнике (окно звена прав notify-api, приёмка
// NTF-4 Р16) получает величину от установки: её пишет профиль зонтика
// `deploy/helm/umbrella/values.prod.yaml` по пути values, который называет тег
// `knob` объявления ручки. Гейт прежде читал величину только из исходника и на
// такой площадке краснел по построению — «окно без величины в исходнике», —
// сколько бы установка ни объявляла. Теперь он читает её там, где её пишет
// посадка, и сверяет так же, как величину из исходника: равенство записи
// политики и не выше потолка. Прочие профили зонтика, где ключ задан, обязаны
// задавать то же значение.
//
// # Что доказывается
//
// Инъекции приёмки Р16 (1)–(4), каждая красная со своим текстом, и законный
// близнец — 5 с в профиле и 5 с в записи — молчит и называет координату
// профиля. Близнец отличается от (2) и (3) ровно одним числом. Площадка с
// умолчанием в исходнике в ветку посадки не входит.
package revocationwindowgate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/tools/revocationwindowgate"
)

const (
	declFile     = "services/notify/cmd/notify-api/internal/config/config.go"
	prodPath     = "deploy/helm/umbrella/values.prod.yaml"
	windowKey    = "notify KACHO_NOTIFY_AUTHZ_CACHE_TTL"
	windowValues = "notify.authz.cacheTTL"
)

// scanSites — площадки одного файла объявления, как их видит гейт дерева.
func scanSites(t *testing.T, src string) []revocationwindowgate.Site {
	t.Helper()
	rep := &revocationwindowgate.Report{}
	if err := revocationwindowgate.ScanFile(rep, "notify", declFile, src); err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("предпосылка пробы: разбор синтетики дал находки %v", rep.Findings)
	}
	return rep.Sites
}

// policyOf — политика с одной записью окна и потолком 10 с.
func policyOf(window time.Duration) revocationwindowgate.Policy {
	return revocationwindowgate.Policy{
		Windows: map[string]time.Duration{windowKey: window},
		Ceiling: 10 * time.Second,
	}
}

// prodWith — профиль values.prod с окном звена прав notify = v; v == "" —
// ключа в профиле нет вовсе (соседний ключ того же узла есть).
func prodWith(v string) revocationwindowgate.Profile {
	src := "notify:\n  authMode: production\n"
	if v != "" {
		src += "  authz:\n    cacheTTL: " + v + "\n"
	}
	return revocationwindowgate.Profile{Path: prodPath, Src: []byte(src)}
}

func judge(t *testing.T, src string, pol revocationwindowgate.Policy,
	prod revocationwindowgate.Profile, others ...revocationwindowgate.Profile,
) revocationwindowgate.Verdict {
	t.Helper()
	v := revocationwindowgate.Judge(scanSites(t, src), pol, prod, others)
	t.Logf("профилей прочитано=%d, величин из посадки=%d, находок=%d", v.ProfilesRead, len(v.Landed), len(v.Findings))
	return v
}

// oneFinding — ровно одна находка, и в ней все названные части.
func oneFinding(t *testing.T, v revocationwindowgate.Verdict, parts ...string) {
	t.Helper()
	if len(v.Findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %q", len(v.Findings), v.Findings)
	}
	for _, p := range parts {
		if !strings.Contains(v.Findings[0], p) {
			t.Errorf("находка не называет %q:\n%s", p, v.Findings[0])
		}
	}
}

// Близнец: 5 с в профиле и 5 с в записи — тишина, и величина сопоставлена с
// координатой профиля.
func TestLanding_TwinFiveSecondsIsSilentAndNamesTheProfile(t *testing.T) {
	v := judge(t, srcRequiredWindowKnob, policyOf(5*time.Second), prodWith("5s"))
	if len(v.Findings) != 0 {
		t.Fatalf("близнец красный: %q", v.Findings)
	}
	if len(v.Landed) != 1 {
		t.Fatalf("величин из посадки %d, ожидалась одна: %+v", len(v.Landed), v.Landed)
	}
	l := v.Landed[0]
	if l.Key != windowKey || l.Window != 5*time.Second || l.Profile != prodPath || l.Line != 4 {
		t.Fatalf("сопоставлено %+v, ожидалось %s = 5s в %s:4", l, windowKey, prodPath)
	}
	if v.ProfilesRead != 1 {
		t.Fatalf("профилей прочитано %d, ожидался 1", v.ProfilesRead)
	}
}

// (1) ключ снят из values.prod — «окно без величины в посадке» с путём values и
// координатой объявления.
func TestLanding_KeyAbsentFromProdIsAFinding(t *testing.T) {
	v := judge(t, srcRequiredWindowKnob, policyOf(5*time.Second), prodWith(""))
	oneFinding(t, v, "окно без величины в посадке", windowKey, windowValues, prodPath, declFile+":6")
}

// (2) 7 с в профиле при записи 5 с — «окно разошлось с политикой», оба числа.
func TestLanding_ProdDiffersFromPolicyIsAFinding(t *testing.T) {
	v := judge(t, srcRequiredWindowKnob, policyOf(5*time.Second), prodWith("7s"))
	oneFinding(t, v, "окно разошлось с политикой", windowKey, "7s", "5s", prodPath+":4")
}

// (3) 11 с в профиле и в записи — «окно превышает потолок политики», граница 10s.
func TestLanding_ProdAboveCeilingIsAFinding(t *testing.T) {
	v := judge(t, srcRequiredWindowKnob, policyOf(11*time.Second), prodWith("11s"))
	oneFinding(t, v, "окно превышает потолок политики", windowKey, "11s", "10s", prodPath+":4")
}

// (4) путь в теге knob изменён, values не тронуты — та же находка, что (1):
// путь берётся из объявления, своего перечня путей у гейта нет.
func TestLanding_KnobTagPathChangedIsTheSameFindingAsAbsent(t *testing.T) {
	src := strings.Replace(srcRequiredWindowKnob, `knob:"notify.authz.cacheTTL"`, `knob:"notify.authz.cacheTtl"`, 1)
	if src == srcRequiredWindowKnob {
		t.Fatal("предпосылка пробы: инъекция не изменила тег")
	}
	v := judge(t, src, policyOf(5*time.Second), prodWith("5s"))
	oneFinding(t, v, "окно без величины в посадке", windowKey, "notify.authz.cacheTtl", prodPath)
}

// Обязательная ручка без тега knob — пути values назвать нечем: находка, а не
// тишина.
func TestLanding_RequiredKnobWithoutValuesPathIsAFinding(t *testing.T) {
	src := strings.Replace(srcRequiredWindowKnob, ` knob:"notify.authz.cacheTTL"`, "", 1)
	if src == srcRequiredWindowKnob {
		t.Fatal("предпосылка пробы: инъекция не сняла тег")
	}
	v := judge(t, src, policyOf(5*time.Second), prodWith("5s"))
	oneFinding(t, v, "тега knob", windowKey, declFile+":6")
}

// Значение в профиле не читается длительностью — находка с координатой.
func TestLanding_UnreadableProdValueIsAFinding(t *testing.T) {
	v := judge(t, srcRequiredWindowKnob, policyOf(5*time.Second), prodWith("five"))
	oneFinding(t, v, "не читается", windowKey, `"five"`, prodPath+":4")
}

// Законные формы записи величины в YAML: кавычки и ссылка на якорь — тишина.
func TestLanding_QuotedAndAliasedValuesAreRead(t *testing.T) {
	for name, src := range map[string]string{
		"кавычки": "notify:\n  authz:\n    cacheTTL: \"5s\"\n",
		"якорь":   "common: &w 5s\nnotify:\n  authz:\n    cacheTTL: *w\n",
		"слияние": "base: &b\n  cacheTTL: 5s\nnotify:\n  authz:\n    <<: *b\n",
	} {
		t.Run(name, func(t *testing.T) {
			prod := revocationwindowgate.Profile{Path: prodPath, Src: []byte(src)}
			v := judge(t, srcRequiredWindowKnob, policyOf(5*time.Second), prod)
			if len(v.Findings) != 0 || len(v.Landed) != 1 || v.Landed[0].Window != 5*time.Second {
				t.Fatalf("форма %s: находки %q, сопоставлено %+v", name, v.Findings, v.Landed)
			}
		})
	}
}

// Прочие профили: где ключ задан, значение равно записи. 7 с в профиле стенда —
// находка с координатой профиля; 5 с и отсутствие ключа — тишина.
func TestLanding_OtherProfilesMustAgreeWhereTheyDeclare(t *testing.T) {
	stand := func(path, v string) revocationwindowgate.Profile {
		p := prodWith(v)
		p.Path = path
		return p
	}
	v := judge(t, srcRequiredWindowKnob, policyOf(5*time.Second), prodWith("5s"),
		stand("deploy/helm/umbrella/values.dev.yaml", "7s"),
		stand("deploy/helm/umbrella/values.yaml", "5s"),
		stand("deploy/helm/umbrella/values.own.yaml", ""),
	)
	oneFinding(t, v, "deploy/helm/umbrella/values.dev.yaml:4", windowKey, "7s", "5s")
	if v.ProfilesRead != 4 {
		t.Fatalf("профилей прочитано %d, ожидалось 4", v.ProfilesRead)
	}
}

// Профиль, который не разбирается как YAML, — находка, а не пропуск.
func TestLanding_UnparsableProfileIsAFinding(t *testing.T) {
	bad := revocationwindowgate.Profile{Path: "deploy/helm/umbrella/values.dev.yaml", Src: []byte("notify: [\n")}
	v := judge(t, srcRequiredWindowKnob, policyOf(5*time.Second), prodWith("5s"), bad)
	oneFinding(t, v, "deploy/helm/umbrella/values.dev.yaml", "не разбирается")
}

// Площадка с умолчанием в исходнике в ветку посадки не входит: ключа в профиле
// нет — тишина, величина сверена из исходника.
func TestLanding_DefaultedSiteIsJudgedBySourceNotByLanding(t *testing.T) {
	v := judge(t, srcDefaultedWindowKnob, policyOf(5*time.Second), prodWith(""))
	if len(v.Findings) != 0 || len(v.Landed) != 0 {
		t.Fatalf("площадка с умолчанием: находки %q, из посадки %+v", v.Findings, v.Landed)
	}
	// Близнец: умолчание 7s при записи 5s — прежняя находка из исходника.
	src := strings.Replace(srcDefaultedWindowKnob, `default:"5s"`, `default:"7s"`, 1)
	oneFinding(t, judge(t, src, policyOf(5*time.Second), prodWith("")), "окно разошлось с политикой", "7s", "5s")
}

// Перепись политики в обе стороны: площадка без записи и запись без площадки.
func TestLanding_CensusBothDirections(t *testing.T) {
	pol := revocationwindowgate.Policy{Windows: map[string]time.Duration{}, Ceiling: 10 * time.Second}
	oneFinding(t, judge(t, srcRequiredWindowKnob, pol, prodWith("5s")), "окно не объявлено политикой", windowKey)

	pol.Windows = map[string]time.Duration{windowKey: 5 * time.Second, "notify KACHO_GONE_TTL": time.Second}
	oneFinding(t, judge(t, srcRequiredWindowKnob, pol, prodWith("5s")), "запись политики без предмета", "notify KACHO_GONE_TTL")
}

// ScanFile несёт путь values из тега knob — у обязательной и у умолчанной
// площадки одинаково.
func TestScanFile_SiteCarriesTheKnobTagAsValuesPath(t *testing.T) {
	for name, src := range map[string]string{"обязательная": srcRequiredWindowKnob, "с умолчанием": srcDefaultedWindowKnob} {
		sites := scanSites(t, src)
		if len(sites) != 1 || sites[0].ValuesPath != windowValues {
			t.Fatalf("%s: площадки %+v, ожидался путь values %q", name, sites, windowValues)
		}
	}
}
