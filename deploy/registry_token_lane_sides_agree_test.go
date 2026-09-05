// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// registry_token_lane_sides_agree_test.go — у докерной полосы выдачи ДВЕ
// стороны, и объявление у них ОДНО (задача #1184).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Реестр называет докер-клиенту имя своей службы в вызове на аутентификацию;
// клиент возвращает это имя в `?service=`; iam чеканит удостоверение адресату,
// которого объявил сам. Пока iam адресата не сверял, расхождение этих двух
// объявлений было НЕВИДИМО: клиент echo-ит услышанное от реестра, реестр это и
// ждёт, подписант чеканил что просят. Сверка введена — и расхождение стало
// отказом во входе докера.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СВЯЗАНЫ, А НЕ РАВНЫ
//
// Свести умолчания нельзя и не нужно: имя реестра — тенант-фейсинг DNS, оно
// законно СВОЁ у каждого кластера. Значит стороны обязаны быть СВЯЗАНЫ.
// Объявление одно — `global.kacho.registry.serviceAud`; читают его ОБА
// подчарта (`global` есть единственное, что видно из обоих контекстов). При
// таком устройстве расходиться нечему by construction, а рендер отказывает,
// если кто-то объявит вторую сторону отдельно и иначе.
//
// ЗДЕСЬ СУДЯТСЯ ВСЕ ПРОФИЛИ, поднимающие полосу. Прежняя редакция судила один
// из шести: она сравнивала два ОБЪЯВЛЕНИЯ и потому молчала там, где не
// объявлено ни одного, — а умолчание стороны личности лежало в дереве, закрытом
// для этого пакета (`services/iam/internal/...`), и прочитать его было нечем.
// С единым источником величина живёт в каталоге развёртывания, и «унаследовано»
// перестало означать «не судимо».
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА
//
//   - профиль, не поднимающий реестр, ничего объявлять не обязан;
//   - СОДЕРЖАНИЕ имени не судится: какое оно у стенда — решение профиля;
//   - перечень профилей берётся КАТАЛОГОМ, а не выписывается.
package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// laneSideFinding — одна находка с координатой, по которой её чинят.
type laneSideFinding struct {
	profile string
	what    string
}

func (f laneSideFinding) String() string { return f.profile + ": " + f.what }

// laneCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного», а «судимо» — от «поднимает полосу»: пропуск, невидимый в
// переписи, неотличим от проверенного.
type laneCensus struct {
	profiles int
	serving  int
	judged   int
}

const laneSourceKeyPath = "global.kacho.registry.serviceAud"

// laneSingleSource — действующее значение единого источника для профиля:
// собственное объявление профиля, иначе объявление БАЗЫ (`values.yaml`, которую
// helm загружает всегда).
func laneSingleSource(profile, base map[string]any) string {
	if s, _ := dig(profile, "global", "kacho", "registry", "serviceAud").(string); strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	s, _ := dig(base, "global", "kacho", "registry", "serviceAud").(string)
	return strings.TrimSpace(s)
}

// laneAddressee — ДЕЙСТВУЮЩИЙ адресат полосы для профиля: собственное
// объявление подчарта, если оно есть, иначе единый источник. Тот же порядок,
// что в шаблонах обеих сторон; вычисляется ЗДЕСЬ один раз, чтобы соседние
// проверки не заводили каждая свою копию — разойдясь, копии молча судили бы о
// разных величинах.
func laneAddressee(profile, base map[string]any) string {
	if s, _ := dig(profile, "kaname", "config", "apiServer", "registryToken", "service").(string); strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return laneSingleSource(profile, base)
}

// scanRegistryLaneSides — ядро проверки.
func scanRegistryLaneSides(profiles map[string]map[string]any, base map[string]any) ([]laneSideFinding, laneCensus) {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []laneSideFinding
	census := laneCensus{profiles: len(profiles)}

	for _, name := range names {
		reg, _ := dig(profiles[name], "registry").(map[string]any)
		if reg == nil {
			continue
		}
		if on, present := reg["enabled"].(bool); present && !on {
			continue
		}
		if iam, _ := dig(profiles[name], "kaname").(map[string]any); iam == nil {
			continue
		}
		census.serving++
		census.judged++

		source := laneSingleSource(profiles[name], base)
		if source == "" {
			findings = append(findings, laneSideFinding{name,
				"полоса поднята, а " + laneSourceKeyPath + " не объявлен ни профилем, ни базой — " +
					"сторона реестра и сторона личности выводились бы каждая из своего умолчания"})
			continue
		}

		regSide, _ := reg["serviceAud"].(string)
		if regSide = strings.TrimSpace(regSide); regSide != "" && regSide != source {
			findings = append(findings, laneSideFinding{name,
				"registry.serviceAud объявлен отдельно (" + regSide + ") и расходится с " +
					laneSourceKeyPath + " (" + source + ") — рендер откажет, стенд не поднимется"})
		}
		iamSide, _ := dig(profiles[name], "kaname", "config", "apiServer", "registryToken", "service").(string)
		if iamSide = strings.TrimSpace(iamSide); iamSide != "" && iamSide != source {
			findings = append(findings, laneSideFinding{name,
				"kaname.config.apiServer.registryToken.service объявлен отдельно (" + iamSide +
					") и расходится с " + laneSourceKeyPath + " (" + source + ") — рендер откажет, стенд не поднимется"})
		}
	}
	return findings, census
}

// TestRegistryTokenLaneSidesAgree — сама проверка.
func TestRegistryTokenLaneSidesAgree(t *testing.T) {
	files := profileFiles(t)
	profiles := make(map[string]map[string]any, len(files))
	var base map[string]any
	for _, f := range files {
		v := readYAML(t, f)
		profiles[f] = v
		if filepath.Base(f) == "values.yaml" {
			base = v
		}
	}
	if base == nil {
		t.Fatal("базовых значений умбреллы (values.yaml) нет — предпосылка проверки не выполняется")
	}

	findings, c := scanRegistryLaneSides(profiles, base)
	t.Logf("перепись: профилей осмотрено %d · поднимают полосу %d · судимо %d · находок %d",
		c.profiles, c.serving, c.judged, len(findings))

	if c.serving == 0 {
		t.Fatal("ни один профиль не поднимает докерную полосу — проверке нечего судить")
	}
	if c.judged != c.serving {
		t.Errorf("судимо %d из %d поднимающих полосу — пропуск, невидимый в переписи, неотличим от проверенного",
			c.judged, c.serving)
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// TestRegistryLaneSidesScannerSeesEachDivergenceAndIsSilentOnTheLink — инъекция
// по КАЖДОЙ оси плюс законный близнец. Оси разведены намеренно: одна проба,
// снимающая всё разом, зеленела бы на починке любой одной стороны.
func TestRegistryLaneSidesScannerSeesEachDivergenceAndIsSilentOnTheLink(t *testing.T) {
	base := map[string]any{"global": map[string]any{"kacho": map[string]any{
		"registry": map[string]any{"serviceAud": "base.example"},
	}}}
	profile := func(source, regSide, iamSide string) map[string]any {
		p := map[string]any{
			"registry": map[string]any{"enabled": true},
			"kaname":   map[string]any{},
		}
		if source != "" {
			p["global"] = map[string]any{"kacho": map[string]any{
				"registry": map[string]any{"serviceAud": source},
			}}
		}
		if regSide != "" {
			p["registry"].(map[string]any)["serviceAud"] = regSide
		}
		if iamSide != "" {
			p["kaname"] = map[string]any{"config": map[string]any{
				"apiServer": map[string]any{"registryToken": map[string]any{"service": iamSide}},
			}}
		}
		return p
	}
	scan := func(name string, p map[string]any, b map[string]any) ([]laneSideFinding, laneCensus) {
		return scanRegistryLaneSides(map[string]map[string]any{name: p}, b)
	}

	// (а) законный близнец: профиль не объявляет ничего — величину даёт база.
	// Это и есть штатный вид пяти профилей из шести, и он обязан быть СУДИМ.
	got, c := scan("inherits.yaml", profile("", "", ""), base)
	if len(got) != 0 || c.serving != 1 || c.judged != 1 {
		t.Fatalf("унаследованный от базы источник обязан судиться и молчать, получено %v (поднимает %d, судимо %d)",
			got, c.serving, c.judged)
	}

	// (б) второй законный близнец: обе стороны объявлены и СХОДЯТСЯ с источником.
	got, c = scan("agree.yaml", profile("lane.example", "lane.example", "lane.example"), base)
	if len(got) != 0 || c.judged != 1 {
		t.Fatalf("сошедшиеся с источником объявления обязаны молчать, получено %v", got)
	}

	// (в) оси расхождения — каждая отдельно.
	for name, p := range map[string]map[string]any{
		"reg-drift.yaml": profile("lane.example", "other.example", ""),
		"iam-drift.yaml": profile("lane.example", "", "other.example"),
	} {
		got, c = scan(name, p, base)
		if len(got) != 1 || c.judged != 1 {
			t.Fatalf("%s: ось обязана быть находкой, получено %v (судимо %d)", name, got, c.judged)
		}
		if !strings.Contains(got[0].String(), name) || !strings.Contains(got[0].String(), "other.example") {
			t.Fatalf("%s: находка не называет ни координату, ни разошедшуюся величину: %s", name, got[0])
		}
	}

	// (г) источника нет вовсе — находка: обе стороны выводились бы из своих умолчаний.
	got, c = scan("no-source.yaml", profile("", "", ""), map[string]any{})
	if len(got) != 1 || c.judged != 1 {
		t.Fatalf("необъявленный единый источник обязан быть находкой, получено %v (судимо %d)", got, c.judged)
	}

	// (д) предмета нет — не находка и не «судимо»: профиль без реестра.
	got, c = scan("off.yaml", map[string]any{"registry": map[string]any{"enabled": false}}, base)
	if len(got) != 0 || c.serving != 0 || c.judged != 0 {
		t.Fatalf("выключенный реестр — не предмет, получено %v (поднимает %d, судимо %d)", got, c.serving, c.judged)
	}
}

// TestBothLaneTemplatesReadTheSingleSource — связь держится ТЕМ, что оба
// шаблона называют один ключ; проверка выше судит значения и о самой связи не
// утверждает ничего. Сняв обращение к источнику в одном подчарте, его сторона
// молча вернулась бы к собственному умолчанию, а все профили остались бы
// зелёными.
func TestBothLaneTemplatesReadTheSingleSource(t *testing.T) {
	sides := map[string]string{
		"сторона реестра":  filepath.Join("..", "services", "registry", "deploy", "templates", "_helpers.tpl"),
		"сторона личности": filepath.Join("helm", "umbrella", "charts", "kaname", "templates", "_helpers.tpl"),
	}
	read := 0
	for side, path := range sides {
		b, err := os.ReadFile(path) // #nosec G304 -- путь фиксирован в тесте
		if err != nil {
			t.Fatalf("%s: не прочитан %s: %v", side, path, err)
		}
		read++
		if !laneTemplateReadsTheSource(string(b)) {
			t.Errorf("%s (%s) не читает единый источник %s — сторона вернулась бы к собственному умолчанию молча",
				side, path, laneSourceKeyPath)
		}
	}
	t.Logf("перепись: шаблонов сторон прочитано %d из %d", read, len(sides))
	if read != len(sides) {
		t.Fatalf("прочитано %d шаблонов из %d — «ноль находок» стало бы «ноль прочитанного»", read, len(sides))
	}
}

// laneTemplateReadsTheSource — предикат: текст шаблона обращается к единому
// источнику. Ключ в шаблоне пишется через точку по вложенности, поэтому ищем
// его сегменты подряд, а не строку пути.
func laneTemplateReadsTheSource(tpl string) bool {
	return strings.Contains(tpl, "global") &&
		strings.Contains(tpl, "kacho") &&
		strings.Contains(tpl, "registry") &&
		strings.Contains(tpl, "serviceAud")
}

// TestLaneSourcePredicateSeesItsAbsence — инъекция для предиката выше: он
// обязан краснеть на шаблоне, вернувшемся к собственной ручке, и молчать на
// законной форме обращения к источнику.
func TestLaneSourcePredicateSeesItsAbsence(t *testing.T) {
	if laneTemplateReadsTheSource(`{{- define "x" -}}{{ .Values.serviceAud }}{{- end -}}`) {
		t.Error("предикат не увидел шаблон, вернувшийся к собственной ручке")
	}
	if !laneTemplateReadsTheSource(`{{ (((.Values.global).kacho).registry).serviceAud }}`) {
		t.Error("предикат не распознал законную форму обращения к единому источнику")
	}
}
