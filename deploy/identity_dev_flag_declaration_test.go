// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_dev_flag_declaration_test.go — стенд, объявивший боевую посадку, не
// вправе держать провайдера личности в режиме разработки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У обоих провайдеров личности есть ОДНА булева ручка, снимающая транспортные
// требования к сессии: `kratos.kratos.development` и `hydra.hydra.dev`. Со
// включённой ручкой cookie сессии и CSRF перестают нести атрибут `Secure`, то
// есть уезжают по открытому HTTP (CWE-614), а провайдер перестаёт требовать
// https у собственного issuer.
//
// Ручка НЕ видна ни в одном исходе, который мы проверяем сегодня: под
// поднимается Ready, отвечает на пробы, выдаёт рабочие сессии. Разница
// наблюдается только в атрибутах cookie у браузера — то есть нигде в наших
// гейтах.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ОТДЕЛЬНАЯ ПРОВЕРКА, А НЕ СТРОКА В dbtls_declaration_test.go
//
// Тот файл отвечает на вопрос «шифруется ли канал К БАЗЕ» и намеренно
// ограничен подчартами нашего дерева. Здесь предмет другой — транспорт СЕССИИ
// пользователя у стороннего чарта, — и граница у него своя: проверяются
// профили, а не подчарты.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ «НЕ ОБЪЯВЛЕНО» ЗДЕСЬ НЕ НАХОДКА (в отличие от sslmode)
//
// У обеих ручек умолчание чарта — `false`, то есть безопасное. Требовать от
// каждого профиля выписывать `false` значило бы требовать шума, который никто
// не читает. Вместо этого умолчание проверяется ОТДЕЛЬНО
// (TestIdentityDevFlags_ChartDefaultsAreStillSecure): если обновление чарта
// Ory перевернёт умолчание, красной станет предпосылка, а не молчание.
// Именно поэтому нельзя просто «проверить значение»: без проверки предпосылки
// эта проверка тихо превратится в «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Проверяются ТОЛЬКО стеки, ОБЪЯВИВШИЕ боевую посадку (`mode`/`authMode`
//     со значением production*). Стенд разработки ручку держать вправе —
//     правило самонастраивается: профиль, объявивший боевую посадку, приходит
//     под проверку без правки этого файла.
//   - Проверяется ТОЛЬКО провайдер, ВКЛЮЧЁННЫЙ в этом стеке: у выключенного
//     подчарта ручка ничего не значит.
//   - Проверяется ОБЪЯВЛЕНИЕ профиля, а не рендер: значение, приехавшее из
//     умолчания чарта, в манифесте неотличимо от объявленного, а зависимости
//     чартов проверке не нужны — поэтому она не умеет пропуститься.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// identityDevKnob — одна ручка режима разработки у стороннего чарта личности.
//
// Список выписан, а не выведен: чарты Ory лежат в дереве архивами, и вывести
// «какая из булевых ручек снимает Secure у cookie» из их значений нельзя — это
// знание об их семантике, а не о форме. Цена выписанного списка закрыта
// проверкой предпосылки ниже: обе координаты обязаны существовать в архиве
// своего чарта и обязаны иметь там безопасное умолчание.
type identityDevKnob struct {
	subchart string   // ключ значений умбреллы
	path     []string // путь ВНУТРИ поддерева значений подчарта
	archive  string   // архив чарта в charts/
	what     string   // что снимает ручка — для текста находки
}

func (k identityDevKnob) coord() string { return k.subchart + "." + strings.Join(k.path, ".") }

func identityDevKnobs() []identityDevKnob {
	return []identityDevKnob{
		{
			subchart: "kratos",
			path:     []string{"kratos", "development"},
			archive:  "kratos-0.62.1.tgz",
			what:     "cookie сессии и CSRF перестают нести атрибут Secure и уезжают по открытому HTTP",
		},
		{
			subchart: "hydra",
			path:     []string{"hydra", "dev"},
			archive:  "hydra-0.62.1.tgz",
			what:     "провайдер перестаёт требовать https у собственного issuer",
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Чтение дерева.

// identityStackFacts — то, что проверке нужно знать про один стек.
type identityStackFacts struct {
	effective  map[string]any // значения умбреллы + профили стека
	production bool           // стек ОБЪЯВИЛ боевую посадку
	source     string         // где объявлено — для текста находки
}

// declaresProduction — стек объявил боевую посадку, если среди его
// `mode`/`authMode` есть хотя бы одно production* и НЕ ОСТАЛОСЬ ни одного
// `dev`.
//
// Первая редакция предиката спрашивала только «есть ли хоть одно production*»
// — и подвела под правило стенд разработки: values.dev.yaml объявляет
// `api-gateway.authn.mode=production-strict` (край обязан требовать Bearer
// даже на ноутбуке), оставляя семь других координат в `dev`. Ошибку поймал
// контроль TestDeclaresProduction_RecognisesTheRealTree, а не чтение — потому
// он и написан парой к находке.
//
// Предикат выведен из того, как посадку объявляют сами профили: стенд, у
// которого хоть один компонент остался в режиме разработки, боевым не
// считается. Новый боевой профиль приходит под проверку без правки этого
// файла.
func declaresProduction(declared map[string]any) (bool, string) {
	var found []struct {
		Path  []string
		Value any
	}
	walkStrings(declared, nil, func(k string) bool {
		return strings.EqualFold(k, "mode") || strings.EqualFold(k, "authMode")
	}, &found)

	var prod, dev []string
	for _, f := range found {
		s, ok := f.Value.(string)
		if !ok {
			continue
		}
		coord := strings.Join(f.Path, ".") + "=" + s
		switch v := strings.ToLower(strings.TrimSpace(s)); {
		case strings.HasPrefix(v, "production"):
			prod = append(prod, coord)
		case v == "dev":
			dev = append(dev, coord)
		}
	}
	if len(prod) == 0 {
		return false, "боевых объявлений нет"
	}
	if len(dev) > 0 {
		return false, fmt.Sprintf("боевых объявлений %d, но %d осталось в dev: %s",
			len(prod), len(dev), strings.Join(dev, ", "))
	}
	return true, fmt.Sprintf("боевых объявлений %d, ни одного dev", len(prod))
}

func allIdentityStackFacts(t *testing.T) map[string]identityStackFacts {
	t.Helper()
	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))

	out := map[string]identityStackFacts{}
	for name, chain := range chains {
		declared := map[string]any{}
		for _, p := range chain {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		prod, src := declaresProduction(declared)
		out[name] = identityStackFacts{
			effective:  mergeValues(mergeValues(map[string]any{}, base), declared),
			production: prod,
			source:     src,
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка ниже подавала ей
// синтетический вход, а не подделывала дерево.

type identityFinding struct {
	stack string
	coord string
	value string
}

func scanIdentityDevFlags(stacks map[string]identityStackFacts, knobs []identityDevKnob) []identityFinding {
	var out []identityFinding
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		f := stacks[name]
		if !f.production {
			continue // стенд разработки ручку держать вправе
		}
		for _, k := range knobs {
			if on, ok := lookup(f.effective, k.subchart, "enabled"); !ok || on != true {
				continue // подчарт выключен — ручка ничего не значит
			}
			v, ok := lookup(f.effective, append([]string{k.subchart}, k.path...)...)
			if !ok {
				continue // умолчание чарта; его безопасность держит проверка предпосылки
			}
			if v == true {
				out = append(out, identityFinding{name, k.coord(), fmt.Sprint(v)})
			}
		}
	}
	return out
}

// recordedIdentityDevConcessions — послабления, принятые ЯВНО и записанные с
// координатой, причиной и условием снятия. Ключ — «<стек>/<координата>».
//
// Запись живёт, пока у неё есть предмет: снятое послабление роняет
// TestRecordedIdentityDevConcessions_StillHaveASubject и требует удалить
// запись. Иначе исключение переживёт свой предмет и начнёт разрешать то,
// чего уже нет.
var recordedIdentityDevConcessions = map[string]string{
	"fe3455/kratos.kratos.development": "площадка fe3455 обслуживается по HTTP (перед кластером " +
		"стоит прокси, TLS на нём не терминируется). Ory Kratos привязывает атрибут Secure у " +
		"cookie сессии и CSRF ИСКЛЮЧИТЕЛЬНО к этой ручке — со снятым послаблением браузер по HTTP " +
		"cookie не отдаёт, и вход зацикливается. Послабление касается ТОЛЬКО транспорта " +
		"интерфейса личности; посадка control-plane (authn.mode=production-strict, mTLS, " +
		"sslmode=require на пяти базах сервисов) им не затронута. Условие снятия: появление " +
		"HTTPS у площадки — тогда ручка снимается, и эта запись обязана уйти вместе с ней",

	"dev-prod/kratos.kratos.development": "стенд kind, обслуживаемый по HTTP (hostPort, без " +
		"терминации TLS), — та же привязка Secure к ручке, что у fe3455. Профиль наследует " +
		"значение из values.dev.yaml и НЕ переопределяет его, при том что hydra.hydra.dev тот же " +
		"профиль переворачивает в false явно. Условие снятия: появление HTTPS у стенда dev-prod " +
		"ЛИБО перевод входа на путь, не зависящий от cookie браузера",
}

// ─────────────────────────────────────────────────────────────────────────────
// САМА ПРОВЕРКА.

func TestProductionStacks_DoNotEnableIdentityDevMode(t *testing.T) {
	stacks := allIdentityStackFacts(t)
	knobs := identityDevKnobs()

	prodStacks := 0
	enabled := 0
	for _, f := range stacks {
		if !f.production {
			continue
		}
		prodStacks++
		for _, k := range knobs {
			if on, ok := lookup(f.effective, k.subchart, "enabled"); ok && on == true {
				enabled++
			}
		}
	}

	// Проверка СВОЕЙ предпосылки. «Ноль находок» обязано быть отличимо от
	// «ноль прочитанного»: обход, переставший узнавать стеки или боевую
	// посадку, объявит дерево чистым, ничего не осмотрев.
	if len(stacks) == 0 || len(knobs) == 0 || prodStacks == 0 || enabled == 0 {
		t.Fatalf("обход ничего не прочитал: стеков=%d, ручек=%d, боевых стеков=%d, "+
			"включённых провайдеров личности в них=%d — предикат перестал узнавать дерево, "+
			"а не дерево стало чистым", len(stacks), len(knobs), prodStacks, enabled)
	}

	names := make([]string, 0, len(stacks))
	for n, f := range stacks {
		if f.production {
			names = append(names, n+" ("+f.source+")")
		}
	}
	sort.Strings(names)
	t.Logf("осмотрено: стеков=%d, из них объявивших боевую посадку=%d [%s], "+
		"включённых провайдеров личности в них=%d, ручек=%d",
		len(stacks), prodStacks, strings.Join(names, ", "), enabled, len(knobs))

	byCoord := map[string]identityDevKnob{}
	for _, k := range knobs {
		byCoord[k.coord()] = k
	}

	for _, f := range scanIdentityDevFlags(stacks, knobs) {
		if why, recorded := recordedIdentityDevConcessions[f.stack+"/"+f.coord]; recorded {
			t.Logf("записанное послабление %s/%s = %s: %s", f.stack, f.coord, f.value, why)
			continue
		}
		t.Errorf("%s: %s = %s — стек ОБЪЯВИЛ боевую посадку, а провайдер личности работает "+
			"в режиме разработки: %s. Допустимо только false; осознанное послабление вносится "+
			"в recordedIdentityDevConcessions с причиной и условием снятия, а не молчанием",
			f.stack, f.coord, f.value, byCoord[f.coord].what)
	}
}

// Проверка предпосылки — «умолчание чарта всё ещё безопасно» — живёт в
// identity_chart_default_premise_test.go под тегом сборки `helmcharts`: она
// читает АРХИВ чарта, а архивы подкачиваются `helm dependency build` и в git
// не отслеживаются. Здесь её нет не потому, что она необязательна, а потому
// что задание unit-прогона её условия не создаёт. Что она всё-таки зовётся —
// утверждает TestChartPremiseIsActuallyInvoked ниже.

// Записанное послабление обязано иметь предмет.
func TestRecordedIdentityDevConcessions_StillHaveASubject(t *testing.T) {
	stacks := allIdentityStackFacts(t)
	live := map[string]bool{}
	for _, f := range scanIdentityDevFlags(stacks, identityDevKnobs()) {
		live[f.stack+"/"+f.coord] = true
	}
	for key := range recordedIdentityDevConcessions {
		if !live[key] {
			t.Errorf("записанное послабление %q больше нечего разрешать — ручка снята "+
				"или стек перестал объявлять боевую посадку. Удали запись из "+
				"recordedIdentityDevConcessions: исключение, пережившее свой предмет, "+
				"разрешает то, чего нет", key)
		}
	}
	t.Logf("записанных послаблений=%d, действующих режимов разработки=%d",
		len(recordedIdentityDevConcessions), len(live))
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе той же формы.

func TestScanIdentityDevFlags_SelfTest(t *testing.T) {
	knobs := []identityDevKnob{
		{subchart: "kratos", path: []string{"kratos", "development"}, archive: "x", what: "x"},
	}
	tree := func(enabled, dev any) map[string]any {
		k := map[string]any{"enabled": enabled}
		if dev != nil {
			k["kratos"] = map[string]any{"development": dev}
		}
		return map[string]any{"kratos": k}
	}

	// (а) внесённый дефект — боевой стек, провайдер включён, ручка поднята.
	got := scanIdentityDevFlags(map[string]identityStackFacts{
		"injected": {effective: tree(true, true), production: true},
	}, knobs)
	if len(got) != 1 || got[0].coord != "kratos.kratos.development" || got[0].stack != "injected" {
		t.Fatalf("режим разработки на боевом стеке не пойман или пойман без координаты: %+v", got)
	}

	// (б) законный близнец ТОЙ ЖЕ формы — ручка объявлена и опущена. Молчит.
	if got := scanIdentityDevFlags(map[string]identityStackFacts{
		"injected": {effective: tree(true, false), production: true},
	}, knobs); len(got) != 0 {
		t.Fatalf("объявленный false покрашен: %+v", got)
	}

	// (в) второй законный близнец — стенд разработки посадки не объявлял.
	if got := scanIdentityDevFlags(map[string]identityStackFacts{
		"injected": {effective: tree(true, true), production: false},
	}, knobs); len(got) != 0 {
		t.Fatalf("стенд без объявленной боевой посадки покрашен: %+v", got)
	}

	// (г) третий законный близнец — провайдер выключен, ручка ничего не значит.
	if got := scanIdentityDevFlags(map[string]identityStackFacts{
		"injected": {effective: tree(false, true), production: true},
	}, knobs); len(got) != 0 {
		t.Fatalf("выключенный провайдер покрашен: %+v", got)
	}

	// (д) умолчание чарта — ручка не объявлена вовсе. Молчит здесь, её
	//     безопасность держит TestIdentityDevFlags_ChartDefaultsAreStillSecure.
	if got := scanIdentityDevFlags(map[string]identityStackFacts{
		"injected": {effective: tree(true, nil), production: true},
	}, knobs); len(got) != 0 {
		t.Fatalf("необъявленная ручка покрашена: %+v", got)
	}
}

// Предикат боевой посадки обязан узнавать НАСТОЯЩЕЕ дерево, а не только
// синтетику: иначе самопроверка выше зелёная, а обход читает ноль.
func TestDeclaresProduction_RecognisesTheRealTree(t *testing.T) {
	stacks := allIdentityStackFacts(t)
	for _, want := range []string{"prod", "fe3455", "dev-prod"} {
		f, ok := stacks[want]
		if !ok {
			t.Errorf("стек %q не выведен из таблицы стеков", want)
			continue
		}
		if !f.production {
			t.Errorf("стек %q не опознан как объявивший боевую посадку — предикат перестал "+
				"узнавать `mode`/`authMode`", want)
		}
	}
	// Отрицание — только в паре с положительным выше: предикат, признающий
	// боевым всё подряд, зеленит первую половину этой проверки.
	//
	// `prorobotech` разобран как НЕ боевой намеренно, и это утверждение про
	// ЦЕПОЧКУ ИЗ ТАБЛИЦЫ, а не про профиль: таблица перечисляет его поверх
	// values.dev.yaml, тогда как шапка самого профиля объявляет средний слой
	// (values.dev-prod.yaml) обязательным. Пока таблица описывает двухслойную
	// цепочку, посадка у неё — стенд разработки, и правило к ней не относится.
	// Приведут цепочку к документированной — предикат переклассифицирует её
	// сам, без правки этого файла.
	for _, want := range []string{"dev", "prorobotech"} {
		if f, ok := stacks[want]; ok && f.production {
			t.Errorf("стек %q опознан боевым (%s) — предикат стал слишком широким и "+
				"подведёт под правило стенд разработки", want, f.source)
		}
	}
}
