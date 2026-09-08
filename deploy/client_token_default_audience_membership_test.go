// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_token_default_audience_membership_test.go — адресат по умолчанию
// объявлен ВНУТРИ перечня адресатов того же профиля.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У токен-эндпоинта две величины об одном предмете: перечень адресатов, которым
// платформа вообще чеканит, и адресат, которым она чеканит, когда запрос его не
// назвал. Разойдясь, они дают неисполнимую возможность: умолчание отвергается
// НАШЕЙ ЖЕ сверкой, то есть выдача не работает ни при каком входе, где адресат
// не назван — при том что обе половины настройки по отдельности выглядят
// разумными (`api-conventions.md` §«ДВА ПРАВИЛА ОБ ОДНОМ ПОЛЕ»).
//
// Процесс это ловит: `ClientTokenConfig.Validate` отказывает в старте, и
// `client_token.New` отказывается строиться. Здесь тот же вопрос задаётся
// ПРОФИЛЮ — до того, как за ответом поедет подъём стенда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЦЕНА, ИЗ-ЗА КОТОРОЙ ПРОВЕРКА ЗАВЕДЕНА
//
// Задача #1184 перевела `allowedAudiences` на действующее имя докерной полосы и
// оставила `defaultAudience` со снятым встроенным умолчанием процесса. Ни одна
// проверка дерева не сравнивала эти две величины, поэтому расхождение доехало до
// подъёма: под iam не доходил до готовности, `values.dev-prod boots` падал
// «Progress deadline exceeded», а следом краснели все пять сквозных наборов и
// пробы консоли — стенда не было. Девять красных проверок из одной строки
// профиля.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседних client_token_declaration_test.go и
// registry_token_audience_declaration_test.go: контракт — то, что профиль
// ОБЪЯВЛЯЕТ. Проверке не нужны ни helm, ни скачанные зависимости чартов, поэтому
// она не умеет пропуститься. Обе величины лежат в ОДНОМ файле by construction:
// сосед требует, чтобы профиль, поднявший эндпоинт, объявил их сам, а не
// унаследовал.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - судятся ТОЛЬКО профили, включившие эндпоинт: требовать величин у того, кто
//     ими не пользуется, значит отказывать в старте без предмета;
//   - вырожденные и неназванные величины — предмет СОСЕДНЕЙ проверки, и она о них
//     говорит; второе сообщение о том же предмете разошлось бы с первым;
//   - проверяется ЧЛЕНСТВО, а не содержание: какой именно адресат у стенда —
//     решение профиля. Равенства адресату докерной полосы здесь НЕ требуется:
//     умолчание платформы законно может быть и её собственным доменом;
//   - перечень профилей берётся КАТАЛОГОМ: новый профиль приходит под проверку
//     без правки этого файла.
package deploy_test

import (
	"sort"
	"strings"
	"testing"
)

// defaultAudienceFinding — одна находка с координатой, по которой её чинят.
type defaultAudienceFinding struct {
	profile string
	what    string
}

func (f defaultAudienceFinding) String() string { return f.profile + ": " + f.what }

// declaredAudiences — перечень адресатов, считая ЭЛЕМЕНТЫ, а не длину строки.
//
// ОДИН предикат на обе проверки этого пакета: одинокая запятая непуста по длине
// и пуста по существу, и две копии разошлись бы ровно на ней — молча. Тем же
// разбором живёт страж процесса (`config.ParseCommaList`); импортировать его
// отсюда нельзя (дерево сервиса закрыто для этого пакета), поэтому копия здесь
// одна, а не по одной на проверку.
func declaredAudiences(raw string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// scanDefaultAudienceMembership — ядро проверки, вынесенное отдельной функцией,
// чтобы самопроверка ниже подала ему синтетический вход, а не подделывала дерево.
//
// Возвращает находки И число профилей, у которых сверять БЫЛО ЧТО: «ноль
// находок» обязано быть отличимо от «ноль прочитанного», а «судимо» — от
// «поднимает эндпоинт».
func scanDefaultAudienceMembership(profiles map[string]map[string]any) (findings []defaultAudienceFinding, serving, judged int) {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		ct, _ := dig(profiles[name], "kaname", "config", "authn", "clientToken").(map[string]any)
		if ct == nil {
			continue
		}
		if on, _ := ct["enabled"].(bool); !on {
			continue
		}
		serving++

		raw, _ := ct["allowedAudiences"].(string)
		declared := declaredAudiences(raw)
		def, _ := ct["defaultAudience"].(string)
		def = strings.TrimSpace(def)
		if len(declared) == 0 || def == "" {
			// Необъявленное и вырожденное — предмет соседней проверки
			// (client_token_declaration_test.go), и она о нём говорит.
			continue
		}
		judged++

		found := false
		for _, a := range declared {
			if a == def {
				found = true
				break
			}
		}
		if !found {
			findings = append(findings, defaultAudienceFinding{name,
				"defaultAudience " + def + " вне allowedAudiences " + raw +
					" — умолчание отвергалось бы нашей же сверкой, и выдача не работала бы " +
					"ни при каком входе, где адресат не назван; страж старта iam откажет в пуске"})
		}
	}
	return findings, serving, judged
}

// TestClientTokenDefaultAudienceIsInsideTheDeclaredList — сама проверка.
func TestClientTokenDefaultAudienceIsInsideTheDeclaredList(t *testing.T) {
	files := profileFiles(t)
	profiles := make(map[string]map[string]any, len(files))
	for _, f := range files {
		profiles[f] = readYAML(t, f)
	}

	findings, serving, judged := scanDefaultAudienceMembership(profiles)
	t.Logf("перепись: профилей осмотрено %d · поднимают эндпоинт %d · сверено здесь %d · находок %d",
		len(files), serving, judged, len(findings))

	if serving == 0 {
		// Предпосылка проверки: она обоснована тем, что эндпоинт где-то поднят.
		// Ноль поднимающих профилей означает, что она не читала ничего, — и это
		// находка, а не тишина.
		t.Fatal("ни один профиль не поднимает токен-эндпоинт платформы — проверке нечего судить")
	}
	if judged == 0 {
		t.Fatal("ни у одного поднимающего профиля обе величины не объявлены — сверять нечего")
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// TestDefaultAudienceScannerSeesTheDriftAndIsSilentOnMembership — инъекция в обе
// стороны по КАЖДОЙ оси на синтетическом входе.
//
// Без второй половины проверка ловила бы форму: сканер, объявляющий находкой
// всякий профиль, прошёл бы первую половину и был бы бесполезен.
func TestDefaultAudienceScannerSeesTheDriftAndIsSilentOnMembership(t *testing.T) {
	profile := func(audiences, def string, endpointOn bool) map[string]any {
		return map[string]any{"kaname": map[string]any{"config": map[string]any{
			"authn": map[string]any{"clientToken": map[string]any{
				"enabled":          endpointOn,
				"allowedAudiences": audiences,
				"defaultAudience":  def,
			}},
		}}}
	}
	scan := func(name string, p map[string]any) ([]defaultAudienceFinding, int, int) {
		return scanDefaultAudienceMembership(map[string]map[string]any{name: p})
	}

	// (а) законный близнец: умолчание — член перечня. Это и есть штатный вид
	// каждого профиля, и он обязан быть СУДИМ и молчать.
	got, serving, judged := scan("member.yaml",
		profile("https://api.kacho.cloud,lane.example", "lane.example", true))
	if len(got) != 0 || serving != 1 || judged != 1 {
		t.Fatalf("членство обязано судиться и молчать, получено %v (поднимает %d, судимо %d)", got, serving, judged)
	}

	// (б) второй законный близнец: умолчание — первый элемент перечня.
	// Ось отдельная: сканер, сравнивающий только с последним, прошёл бы (а).
	got, _, judged = scan("first.yaml",
		profile("lane.example,https://api.kacho.cloud", "lane.example", true))
	if len(got) != 0 || judged != 1 {
		t.Fatalf("умолчание первым элементом обязано молчать, получено %v", got)
	}

	// (в) РОВНО ТО РАСХОЖДЕНИЕ, ИЗ-ЗА КОТОРОГО ПРОВЕРКА ЗАВЕДЕНА: перечень
	// перевели на действующее имя полосы, умолчание осталось на снятом.
	got, _, judged = scan("drift.yaml",
		profile("https://api.kacho.cloud,registry.in-cloud.io", "registry.kacho.local", true))
	if len(got) != 1 || judged != 1 {
		t.Fatalf("расхождение обязано быть находкой, получено %v (судимо %d)", got, judged)
	}
	if !strings.Contains(got[0].String(), "drift.yaml") ||
		!strings.Contains(got[0].String(), "registry.kacho.local") ||
		!strings.Contains(got[0].String(), "registry.in-cloud.io") {
		t.Fatalf("находка не называет координату либо обе разошедшиеся величины: %s", got[0])
	}

	// (г) вырожденный перечень: одинокая запятая непуста по длине и пуста по
	// существу. Сканер, меряющий длину строки, объявил бы перечень заданным и
	// выдал бы ложную находку о членстве.
	got, serving, judged = scan("degenerate.yaml", profile(" , ", "lane.example", true))
	if len(got) != 0 || serving != 1 || judged != 0 {
		t.Fatalf("вырожденный перечень — предмет соседней проверки, здесь не судится: %v (судимо %d)", got, judged)
	}

	// (д) умолчание не названо — тоже предмет соседней проверки.
	got, serving, judged = scan("no-default.yaml", profile("lane.example", "  ", true))
	if len(got) != 0 || serving != 1 || judged != 0 {
		t.Fatalf("необъявленное умолчание — предмет соседней проверки: %v (судимо %d)", got, judged)
	}

	// (е) предмета нет — не находка и не «поднимает»: эндпоинт выключен.
	got, serving, judged = scan("off.yaml",
		profile("https://api.kacho.cloud", "registry.kacho.local", false))
	if len(got) != 0 || serving != 0 || judged != 0 {
		t.Fatalf("выключенный эндпоинт — не предмет: %v (поднимает %d, судимо %d)", got, serving, judged)
	}
}
