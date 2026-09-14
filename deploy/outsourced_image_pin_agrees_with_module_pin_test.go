// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourced_image_pin_agrees_with_module_pin_test.go — часть, вынесенная в свой
// репозиторий, пинится ЭТИМ деревом ДВАЖДЫ, и оба пина обязаны называть одну
// ревизию.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА ПИНА ОДНОЙ СЛУЖБЫ, И РАЗОЙТИСЬ ОНИ МОГУТ МОЛЧА
//
//	go.mod                                → `github.com/PRO-Robotech/kaname v0.4.0`
//	deploy/helm/umbrella/values.yaml      → `prorobotech/kaname:main-efa5d1f3`
//
// Первый решает, ЧТО ДЕРЕВО ЧИТАЕТ о службе: типы, стабы, а с заведением словаря
// видов подписки — ещё и перечень её видов (`ui-future/deploy/console_stream_kind_dictionary_test.go`
// читает журнал прямо из пиненного модуля). Второй решает, ЧТО СТЕНД ИСПОЛНЯЕТ.
//
// Ни сборка, ни рендер, ни проверки посадки их не сравнивают: каждый по
// отдельности разрешается, досягаем и верен. Расхождение видно только там, где
// дерево заявляет о службе то, чего исполняемый образ ещё не умеет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА (kacho#2650)
//
// Пин модуля подняли до `v0.4.0` — ревизии, несущей сервер подписки службы.
// Пин образа остался на `main-efa5d1f3` — ревизии двумя сутками старше, где
// каталога `internal/subscriptionjournal` нет ни одним файлом. Тем же изменением
// край объявил службу шестым владельцем подписки, а консоль подписалась на семь
// её видов. На стенде это дало:
//
//	GET /subscription/v1/events?owner=iam
//	501 {"code":12,"message":"this owner does not serve the platform subscription verb"}
//
// Отказ пришёл В КОНСОЛЬ БРАУЗЕРА, то есть не на своём вызове модуля, а рядом:
// проба `ui-future/e2e/specs/modules.spec.ts` упала на модуле «сети», хотя ни
// сети, ни vpc к предмету отношения не имели вовсе. Установить это стоило
// подъёма стенда в конвейере (22 минуты) и разбора трассы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она судит СОГЛАСИЕ ДВУХ ОБЪЯВЛЕНИЙ, а не содержимое образа: тег может
// называть верную версию и указывать на сборку, собранную из другого дерева.
// Тождество содержимого не доказывает ни одна ссылка — это граница инструмента,
// названная и у провенанса стенда (`deploy/scripts/stand-provenance.sh`).
// Досягаемость ссылки держит сосед — published_image_pin_is_reachable_test.go;
// здесь про досягаемость не спрашивается ничего.
//
// Популяция сегодня ОДНА пара, и это сказано числом переписи, а не умолчано:
// проверка держит свойство ВПЕРЁД — на вторую вынесенную часть, которой ещё нет.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// outsourcedPin — образ вынесенной части рядом с пином её Go-модуля.
type outsourcedPin struct {
	Stand     string // стенд, чья сложенная цепочка объявила образ
	Component string // координата объявления внутри профиля
	Repo      string // репозиторий образа
	Tag       string // объявленный тег
	Module    string // путь Go-модуля той же части
	Version   string // версия модуля, закреплённая go.mod этого дерева
}

// Ref — ссылка образа, как её читает стенд.
func (p outsourcedPin) Ref() string { return p.Repo + ":" + p.Tag }

// outsourcedPinCensus — объём осмотренного. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», поэтому печатаются ОБЕ величины: сколько модулей
// продукта пинит дерево и сколько пар удалось сопоставить.
type outsourcedPinCensus struct {
	Modules     int // модулей продукта в go.mod
	Images      int // объявлений образа с таким же именем (стенд × координата)
	Pairs       int // из них пар, у которых есть обе стороны
	WithoutTag  int // объявлений без тега — согласие называть нечем
	Pseudo      int // пар, где версия модуля псевдо (сверка по хешу)
	Disagreeing int // пар, где стороны называют РАЗНОЕ
}

// productModulePins — модули продукта, пиненные ЭТИМ деревом: последний сегмент
// пути → объявленная версия.
//
// Читается текстом закоммиченного go.mod, а не `go list`: ответ уже лежит в
// дереве, и сеть предикату не нужна. Ключ — последний сегмент пути модуля,
// потому что ИМЕННО ОН совпадает с именем образа той же части
// (`github.com/PRO-Robotech/kaname` ↔ `prorobotech/kaname`), и это свойство
// дерева, а не совпадение: имя части одно, его производит `productnaming`.
func productModulePins(t *testing.T, root string) map[string]string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("чтение go.mod: %v", err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], productModuleprefix) {
			continue
		}
		out[fields[0][strings.LastIndex(fields[0], "/")+1:]] = fields[1]
	}
	return out
}

// productModuleprefix — корень путей модулей продукта.
const productModuleprefix = "github.com/PRO-Robotech/"

// pseudoVersion — форма псевдоверсии Go: хвост несёт отметку времени и короткий
// хеш ревизии. Вторая законная форма пина, и распознаватель обязан её знать —
// иначе всё, записанное ею, уходит из-под наблюдения, не давая ни красного, ни
// зелёного.
var pseudoVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-0)?\.?[-.]?[0-9]{14}-([0-9a-f]{12})$`)

// tagNamesVersion — называет ли тег образа ту же ревизию, что пин модуля.
//
// ДВЕ законные формы, и обе доказаны инъекцией:
//
//	версия выпуска    `v0.4.0`                          ↔ тег `v0.4.0` (дословно)
//	псевдоверсия      `v0.0.0-20260914001934-236058295b3e` ↔ тег `main-236058295b3e`
//	                                                       либо `main-23605829`
//
// Во второй форме сверяется ХЕШ: тег несёт его хвостом за разделителем, и
// короткий хеш тега обязан быть префиксом хеша версии (конвейеры тегуют разной
// длиной). Ниже семи знаков совпадение перестаёт что-либо доказывать, поэтому
// такой хвост согласием не считается.
func tagNamesVersion(tag, version string) bool {
	tag, version = strings.TrimSpace(tag), strings.TrimSpace(version)
	if tag == "" || version == "" {
		return false
	}
	if tag == version {
		return true
	}
	m := pseudoVersion.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	hash := m[1]
	tail := tag
	if i := strings.LastIndex(tag, "-"); i >= 0 {
		tail = tag[i+1:]
	}
	if len(tail) < 7 || len(tail) > len(hash) {
		return false
	}
	return strings.HasPrefix(hash, tail)
}

// outsourcedPinDisagreements — РЕШЕНИЕ чистой функцией, без ввода-вывода.
//
// Вынесено затем, чтобы доказательство падучести подавало сюда настоящий вход, а
// не подделывало дерево.
func outsourcedPinDisagreements(pins []outsourcedPin) []string {
	var out []string
	for _, p := range pins {
		if tagNamesVersion(p.Tag, p.Version) {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s: стенд %q исполняет %s, а дерево пинит модуль %s %s — два пина одной "+
				"вынесенной части называют РАЗНЫЕ ревизии, и разошлись они молча: "+
				"каждый по отдельности разрешается и досягаем",
			p.Component, p.Stand, p.Ref(), p.Module, p.Version))
	}
	sort.Strings(out)
	return out
}

// outsourcedImagePins — пары, собранные ИЗ ДЕРЕВА: пины образов сложенных
// профилей рядом с пинами модулей go.mod.
func outsourcedImagePins(t *testing.T, root string) ([]outsourcedPin, outsourcedPinCensus) {
	t.Helper()
	modules := productModulePins(t, root)
	images, _ := collectUmbrellaImagePins(t)

	census := outsourcedPinCensus{Modules: len(modules)}
	var out []outsourcedPin
	for _, img := range images {
		name := img.Repo[strings.LastIndex(img.Repo, "/")+1:]
		version, pinned := modules[name]
		if !pinned {
			continue
		}
		census.Images++
		if strings.TrimSpace(img.Tag) == "" {
			// Пин дайджестом согласие называть НЕЧЕМ: дайджест ревизии не несёт.
			// Это не находка и не молчание — это отдельная строка переписи.
			census.WithoutTag++
			continue
		}
		census.Pairs++
		if pseudoVersion.MatchString(version) {
			census.Pseudo++
		}
		out = append(out, outsourcedPin{
			Stand: img.Stand, Component: img.Component, Repo: img.Repo,
			Tag: img.Tag, Module: productModuleprefix + name, Version: version,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Component != out[j].Component {
			return out[i].Component < out[j].Component
		}
		return out[i].Stand < out[j].Stand
	})
	return out, census
}

// TestOutsourcedImagePinAgreesWithItsModulePin — оба пина вынесенной части
// называют одну ревизию.
func TestOutsourcedImagePinAgreesWithItsModulePin(t *testing.T) {
	pins, census := outsourcedImagePins(t, "..")
	findings := outsourcedPinDisagreements(pins)
	census.Disagreeing = len(findings)

	t.Logf("осмотрено: модулей продукта в go.mod %d, объявлений их образа %d, "+
		"пар к сверке %d (из них псевдоверсией %d), без тега %d",
		census.Modules, census.Images, census.Pairs, census.Pseudo, census.WithoutTag)

	// ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ САМА. Модулей продукта у дерева минимум два
	// (фундамент и вынесенная служба); ноль означает, что go.mod прочитан не
	// тот либо форма объявления сменилась, — и тогда молчание проверки не
	// является утверждением о согласии.
	if census.Modules == 0 {
		t.Fatal("go.mod не объявляет ни одного модуля продукта — обход пуст, " +
			"и «находок нет» здесь означало бы «не прочитано ничего»")
	}
	if census.Pairs == 0 && census.WithoutTag == 0 {
		t.Fatal("ни один профиль не объявляет образа вынесенной части — сверять " +
			"нечего, и вердикт беспредметен")
	}

	for _, f := range findings {
		t.Error(f)
	}
}
