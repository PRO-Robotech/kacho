// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// registry_token_audience_declaration_test.go — адресат, которому докерная
// полоса выдачи чеканит, объявлен профилем ВНУТРИ перечня адресатов платформы
// (задача #1184).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Полос выдачи по ключу служебной учётки ДВЕ, и обе чеканят от имени одной
// платформы. Перечень адресатов платформы объявляет `authn.clientToken.
// allowedAudiences`; адресат докерной полосы — `apiServer.registryToken.
// service`. Разойдясь, эти два объявления дают состояние, которого никто не
// выбирал: одна полоса выпускает удостоверение туда, куда вторая его отвергает.
//
// Страж старта на такой посадке отказывает в пуске — и лучше узнать об этом
// здесь, чем при выкатке.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседнего client_token_declaration_test.go: контракт —
// то, что профиль ОБЪЯВЛЯЕТ. Проверке не нужны ни helm, ни скачанные
// зависимости чартов, поэтому она не умеет пропуститься.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - проверяются профили, назвавшие перечень платформы. Не назвавший сверять
//     не с чем — его внешней границей остаётся собственный объявленный адресат
//     полосы. Адресат при этом берётся ДЕЙСТВУЮЩИЙ: собственное объявление
//     подчарта либо единый источник `global.kacho.registry.serviceAud`. Прежде
//     профиль без собственного объявления был здесь НЕ СУДИМ — адресат
//     приезжал из умолчания, жившего в закрытом для этого пакета дереве
//     сервиса. Умолчания больше нет, величина живёт в каталоге развёртывания,
//     и «унаследовано» перестало означать «не проверено»;
//   - проверяется ЧЛЕНСТВО, а не содержание: какой именно адресат у стенда —
//     решение профиля;
//   - перечень профилей берётся КАТАЛОГОМ: новый профиль приходит под проверку
//     без правки этого файла.
package deploy_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// registryAudienceFinding — одна находка с координатой, по которой её чинят.
type registryAudienceFinding struct {
	profile string
	what    string
}

func (f registryAudienceFinding) String() string { return f.profile + ": " + f.what }

// scanRegistryTokenAudience — ядро проверки, вынесенное отдельной функцией,
// чтобы самопроверка ниже подала ему синтетический вход, а не подделывала
// дерево.
//
// Возвращает находки И число профилей, у которых сверять БЫЛО ЧТО: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
func scanRegistryTokenAudience(profiles map[string]map[string]any, base map[string]any) (findings []registryAudienceFinding, comparable, unresolved int) {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		service := laneAddressee(profiles[name], base)
		ct, _ := dig(profiles[name], "kaname", "config", "authn", "clientToken").(map[string]any)
		if ct == nil {
			continue
		}
		if on, _ := ct["enabled"].(bool); !on {
			continue
		}
		raw, _ := ct["allowedAudiences"].(string)
		// ТОТ ЖЕ разбор перечня, что у соседней проверки членства умолчания
		// (client_token_default_audience_membership_test.go): две копии
		// разошлись бы на вырожденном значении — одинокой запятой — и разошлись
		// бы молча.
		declared := declaredAudiences(raw)
		if len(declared) == 0 {
			// Пустой перечень при включённом эндпоинте — предмет соседней
			// проверки, и она о нём говорит. Второе сообщение о том же
			// предмете разошлось бы с первым.
			continue
		}
		if service == "" {
			// Адресат не резолвится НИ ВО ЧТО: ни собственного объявления, ни
			// единого источника. Подставить его нечем — умолчания у величины
			// нет ни в чарте, ни в коде, — поэтому сверять здесь не с чем, а
			// отказ в пуске выносит страж старта процесса. Считается и
			// печатается отдельно: пропуск, невидимый в переписи, неотличим от
			// проверенного.
			unresolved++
			continue
		}
		comparable++

		found := false
		for _, a := range declared {
			if a == service {
				found = true
				break
			}
		}
		if !found {
			findings = append(findings, registryAudienceFinding{name,
				"адресат докерной полосы " + service + " вне перечня адресатов платформы " + raw})
		}
	}
	return findings, comparable, unresolved
}

// TestRegistryTokenAudienceIsInsideThePlatformDeclaration — сама проверка.
func TestRegistryTokenAudienceIsInsideThePlatformDeclaration(t *testing.T) {
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

	findings, comparable, unresolved := scanRegistryTokenAudience(profiles, base)
	t.Logf("перепись: профилей осмотрено %d · сверено здесь %d · адресат не резолвится %d "+
		"(эти отвергает страж старта процесса)", len(files), comparable, unresolved)

	if comparable == 0 {
		// Предпосылка проверки: она обоснована тем, что обе величины где-то
		// объявлены. Ноль сравнимых профилей означает, что она не читала
		// ничего, — и это находка, а не тишина.
		t.Fatal("ни один профиль не объявляет обе величины — проверке нечего сравнивать")
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// TestRegistryTokenAudienceScannerSeesTheDriftAndIsSilentOnAgreement —
// инъекция в обе стороны на синтетическом входе.
//
// Без второй половины проверка ловила бы форму: сканер, объявляющий находкой
// всякий профиль, прошёл бы первую половину и был бы бесполезен.
func TestRegistryTokenAudienceScannerSeesTheDriftAndIsSilentOnAgreement(t *testing.T) {
	profile := func(service, audiences string, endpointOn bool) map[string]any {
		return map[string]any{"kaname": map[string]any{"config": map[string]any{
			"apiServer": map[string]any{"registryToken": map[string]any{"service": service}},
			"authn": map[string]any{"clientToken": map[string]any{
				"enabled": endpointOn, "allowedAudiences": audiences,
			}},
		}}}
	}

	// (а) законный близнец — молчание.
	noBase := map[string]any{}
	got, comparable, unresolved := scanRegistryTokenAudience(map[string]map[string]any{
		"agree.yaml": profile("registry.kacho.local", "https://api.kacho.cloud,registry.kacho.local", true),
	}, noBase)
	if len(got) != 0 || comparable != 1 || unresolved != 0 {
		t.Fatalf("на сошедшихся объявлениях сканер обязан молчать, получено %v (сравнимых %d)", got, comparable)
	}

	// (б) расхождение — находка, называющая профиль И оба значения.
	got, comparable, _ = scanRegistryTokenAudience(map[string]map[string]any{
		"drift.yaml": profile("sts.example.com", "https://api.kacho.cloud,registry.kacho.local", true),
	}, noBase)
	if len(got) != 1 || comparable != 1 {
		t.Fatalf("расхождение обязано быть находкой, получено %v (сравнимых %d)", got, comparable)
	}
	if !strings.Contains(got[0].String(), "drift.yaml") || !strings.Contains(got[0].String(), "sts.example.com") {
		t.Fatalf("находка не называет координату: %s", got[0])
	}

	// (в) предмета нет — не находка. Ни при выключенном эндпоинте, ни при
	// вырожденном перечне, ни при неназванном адресате полосы сверять не с чем,
	// и отказ здесь был бы отказом без предмета.
	for name, p := range map[string]map[string]any{
		"endpoint-off.yaml": profile("sts.example.com", "registry.kacho.local", false),
		"degenerate.yaml":   profile("sts.example.com", " , ", true),
	} {
		got, comparable, unresolved = scanRegistryTokenAudience(map[string]map[string]any{name: p}, noBase)
		if len(got) != 0 || comparable != 0 || unresolved != 0 {
			t.Fatalf("%s: предмета нет, но сканер сказал %v (сравнимых %d, нерезолвящихся %d)",
				name, got, comparable, unresolved)
		}
	}

	// (г) собственного объявления нет, но ЕДИНЫЙ ИСТОЧНИК объявлен базой —
	// профиль обязан быть СУДИМ, а не отложен: это штатный вид пяти профилей
	// из шести, и именно он прежде уходил из-под проверки.
	base := map[string]any{"global": map[string]any{"kacho": map[string]any{
		"registry": map[string]any{"serviceAud": "sts.example.com"},
	}}}
	got, comparable, unresolved = scanRegistryTokenAudience(map[string]map[string]any{
		"from-source.yaml": profile("", "registry.kacho.local", true),
	}, base)
	if len(got) != 1 || comparable != 1 || unresolved != 0 {
		t.Fatalf("адресат из единого источника обязан судиться, получено %v (сравнимых %d, нерезолвящихся %d)",
			got, comparable, unresolved)
	}
	if !strings.Contains(got[0].String(), "sts.example.com") {
		t.Fatalf("находка не называет действующий адресат: %s", got[0])
	}
	// ...и молчать, когда он в перечне: без этой половины предыдущая ловила бы форму.
	got, comparable, unresolved = scanRegistryTokenAudience(map[string]map[string]any{
		"from-source-ok.yaml": profile("", "sts.example.com,registry.kacho.local", true),
	}, base)
	if len(got) != 0 || comparable != 1 || unresolved != 0 {
		t.Fatalf("адресат из единого источника внутри перечня обязан молчать, получено %v", got)
	}

	// (д) адресат не резолвится НИ ВО ЧТО — отдельная величина переписи.
	got, comparable, unresolved = scanRegistryTokenAudience(map[string]map[string]any{
		"unresolved.yaml": profile("", "registry.kacho.local", true),
	}, noBase)
	if len(got) != 0 || comparable != 0 || unresolved != 1 {
		t.Fatalf("нерезолвящийся адресат обязан считаться отдельно, получено %v (сравнимых %d, нерезолвящихся %d)",
			got, comparable, unresolved)
	}
}
