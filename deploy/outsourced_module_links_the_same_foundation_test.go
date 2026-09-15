// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourced_module_links_the_same_foundation_test.go — вынесенная часть обязана
// линковать ТОТ ЖЕ фундамент, что это дерево.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СОГЛАСИЯ ДВУХ ПИНОВ СЛУЖБЫ НЕДОСТАТОЧНО, И ЭТО ИЗМЕРЕНО (kacho#2650)
//
// Сосед (`outsourced_image_pin_agrees_with_module_pin_test.go`) сверяет, что пин
// модуля и пин образа называют ОДНУ ревизию службы. Он верен и нужен — но он
// сверяет службу со службой, а не с ФУНДАМЕНТОМ, и потому молчит на этом классе:
//
//	go.mod этого дерева   → kaname v0.4.0, corelib v1.7.0
//	go.mod самой kaname   → corelib v1.5.0            ← НИКЕМ НЕ ЧИТАЛОСЬ
//	образ стенда          → prorobotech/kaname:v0.4.0
//
// Оба пина службы называют `v0.4.0`, согласие ВЫПОЛНЕНО, сосед зелёный — а стенд
// отвечает `501`. Причина в третьей строке: `corelib v1.7.0` переименовал пакет
// контракта подписки, и имя метода на проводе разошлось —
//
//	край зовёт   /corelib.subscription.InternalSubscriptionService/Subscribe
//	образ служит /kacho.cloud.subscription.InternalSubscriptionService/Subscribe
//
// gRPC отвечает на незарегистрированное имя `Unimplemented`, край переводит его
// в `501 this owner does not serve the platform subscription verb`, и консоль
// показывает пустые счётчики. Класс стоил ДВУХ подъёмов стенда в конвейере.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СБОРКА ЭТОГО НЕ ЛОВИТ — BY CONSTRUCTION
//
// Go выбирает версию зависимости как МАКСИМУМ требований (MVS): дерево требует
// v1.7.0, служба — v1.5.0, побеждает v1.7.0, и ЭТО ДЕРЕВО собирается верно. Но
// образ службы собран в ЕЁ репозитории, где максимум считался без нас, — и там
// победила v1.5.0. То есть расхождение невыразимо в сборке ни одной из сторон:
// каждая по отдельности исправна, и молчат обе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она судит ОБЪЯВЛЕНИЕ пиненного модуля, а не содержимое образа: образ мог быть
// собран из другого дерева. Тождество содержимого не доказывает ни одна ссылка —
// та же граница названа и у соседа, и у провенанса стенда.
//
// Равенство здесь СТРОГОЕ, и это решение, а не строгость ради строгости. Два
// довода: переезд контракта фундамента НЕДЕЛИМ по пакету (промежуточного
// состояния, где совпали бы обе формы имени, не существует), а «версии
// различаются, но переименований между ними не было» — утверждение о ЧУЖОЙ
// истории выпусков, у которого в этом дереве нет производителя. Поэтому дерево
// не уходит вперёд той службы, чей образ оно же и разворачивает: подняли пин
// фундамента — служба выпускает ревизию на нём же, и оба пина едут вместе.
package deploy_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// foundationPart — последний сегмент пути модуля ФУНДАМЕНТА. Сам он вынесенной
// частью не является: он и есть та величина, по которой сверяются остальные.
const foundationPart = "corelib"

// foundationLink — одна вынесенная часть и версия фундамента, которую она линкует.
type foundationLink struct {
	Part       string // последний сегмент пути модуля вынесенной части
	Version    string // версия части, закреплённая go.mod ЭТОГО дерева
	Foundation string // версия фундамента, закреплённая go.mod САМОЙ части
}

// foundationLinkCensus — объём осмотренного. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», поэтому печатаются все три величины.
type foundationLinkCensus struct {
	Parts       int // вынесенных частей продукта в go.mod (фундамент не в счёт)
	Resolved    int // из них тех, чей собственный go.mod удалось прочитать
	Disagreeing int // из них тех, кто линкует ДРУГОЙ фундамент
}

// String — перепись одной строкой, пригодной для журнала конвейера.
func (c foundationLinkCensus) String() string {
	return fmt.Sprintf("вынесенных частей %d, их go.mod прочитано %d, расходятся по фундаменту %d",
		c.Parts, c.Resolved, c.Disagreeing)
}

// requirementIn — версия требуемого модуля в тексте go.mod, либо пусто.
//
// Читается по ПАРЕ полей строки, а не подстрокой имени: путь фундамента
// встречается и в строке `module` самого фундамента, и в комментарии, — а
// предикат по подстроке краснел бы на собственном объяснении.
func requirementIn(gomod, modulePath string) string {
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		line = strings.TrimPrefix(line, "require ")
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == modulePath && strings.HasPrefix(fields[1], "v") {
			return fields[1]
		}
	}
	return ""
}

// goModCache — корень кэша модулей.
//
// Написание пути в кэше даёт `escapeModulePath` — помощник ЭТОГО ЖЕ пакета
// (`pool_out_of_pool_test.go`), у которого уже есть своя инъекция. Второй копии
// здесь не заводится: она разошлась бы с первой молча, а расходятся такие копии
// в сторону «файл не найден», то есть в сторону непрочитанного.
func goModCache(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// foundationLinks — что линкует каждая вынесенная часть, пиненная этим деревом.
func foundationLinks(t *testing.T, root string) ([]foundationLink, foundationLinkCensus) {
	t.Helper()
	pins := productModulePins(t, root)

	treeFoundation := pins[foundationPart]
	if treeFoundation == "" {
		t.Fatalf("go.mod этого дерева не требует фундамента %q — обход беспредметен, "+
			"и «расхождений нет» здесь означало бы «сверять было не с чем»",
			productModuleprefix+foundationPart)
	}

	cache := goModCache(t)
	var (
		out    []foundationLink
		census foundationLinkCensus
	)
	for part, version := range pins {
		if part == foundationPart {
			continue
		}
		census.Parts++
		path := productModuleprefix + part
		modPath := filepath.Join(cache, "cache", "download", escapeModulePath(path), "@v", version+".mod")
		body, err := os.ReadFile(modPath) // #nosec G304 -- путь собран из пина go.mod, не из ввода
		if err != nil {
			// НЕ ВЫПОЛНИЛОСЬ, а не «согласны»: молчание непрочитанного не
			// является утверждением о согласии.
			t.Fatalf("go.mod вынесенной части %s@%s не прочитан (%v).\n"+
				"Кэш модулей наполняется сборкой: прогони `go mod download %s` "+
				"и повтори — иначе вердикт этой проверки беспредметен", path, version, err, path)
		}
		census.Resolved++
		out = append(out, foundationLink{
			Part: part, Version: version,
			Foundation: requirementIn(string(body), productModuleprefix+foundationPart),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Part < out[j].Part })
	return out, census
}

// foundationDisagreements — находки: часть линкует не тот фундамент, что дерево.
//
// Вынесена отдельной чистой функцией, чтобы инъекция подавала ей синтетический
// вход и не трогала ни дерева, ни кэша модулей.
func foundationDisagreements(links []foundationLink, treeFoundation string) []string {
	var findings []string
	for _, l := range links {
		if l.Foundation == treeFoundation {
			continue
		}
		if l.Foundation == "" {
			findings = append(findings, fmt.Sprintf(
				"вынесенная часть %s@%s НЕ ОБЪЯВЛЯЕТ фундамента вовсе, а дерево линкует %s. "+
					"Либо часть перестала им пользоваться — тогда снимите её из этой сверки "+
					"вместе с предметом, — либо обход прочитал не тот go.mod",
				l.Part, l.Version, treeFoundation))
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"вынесенная часть %s@%s линкует фундамент %s, а это дерево — %s. "+
				"Контракты, чей владелец фундамент, у них РАЗНЫЕ: имя службы на проводе "+
				"производится пакетом контракта, поэтому вызов края уйдёт по имени, "+
				"которого образ не регистрировал, и вернётся `Unimplemented` → `501`. "+
				"Сборка этого не покажет (MVS берёт максимум требований, и здесь "+
				"побеждает %s). Чинится выпуском части на фундаменте %s и подъёмом "+
				"ОБОИХ её пинов — модуля в go.mod и образа в профилях",
			l.Part, l.Version, l.Foundation, treeFoundation, treeFoundation, treeFoundation))
	}
	return findings
}

// TestOutsourcedModuleLinksTheSameFoundation — вынесенная часть и это дерево
// линкуют один фундамент.
func TestOutsourcedModuleLinksTheSameFoundation(t *testing.T) {
	links, census := foundationLinks(t, "..")
	treeFoundation := productModulePins(t, "..")[foundationPart]
	findings := foundationDisagreements(links, treeFoundation)
	census.Disagreeing = len(findings)

	t.Logf("осмотрено: %s; дерево линкует фундамент %s", census, treeFoundation)
	for _, l := range links {
		t.Logf("  %s@%s → фундамент %s", l.Part, l.Version, l.Foundation)
	}

	// ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ САМА. Вынесенная часть у дерева есть (служба
	// доступа); ноль означает, что go.mod прочитан не тот либо форма объявления
	// сменилась, — и тогда молчание проверки не является утверждением о согласии.
	if census.Parts == 0 {
		t.Fatal("go.mod не объявляет ни одной вынесенной части продукта — обход пуст, " +
			"и «расхождений нет» здесь означало бы «не прочитано ничего»")
	}

	for _, f := range findings {
		t.Error(f)
	}
}
