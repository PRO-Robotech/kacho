// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// knobDebtEntry — ключ, объявленный в профиле без читателя, которого ещё не
// закрыли фиксом.
//
// # Ведомость ПУСТА, и это состояние, а не предпосылка
//
// Заведённая проверкой ведомость несла 52 записи. Все они закрыты фиксом: у
// двенадцати ключей появился читатель (модули консоли получили автоскейлер,
// границы которого объявлялись и не читались), сорок были сняты из профилей как
// объявления настроек, которых в продукте нет — подсистема отчётов, секции
// конфигурации, отброшенные разбором, копия ветки родителя, которую helm внутрь
// сабчарта не пропагирует, и адресация чарта, переименованного два релиза назад.
//
// Пустая ведомость НЕ делает проверку вакуумной: предмет у неё — дерево, а не
// перечень. Способность обеих половин упасть доказана инъекцией на синтетике
// (TestKnobDebtRulesCatchABareEntry), а не тем, что когда-то падало.
//
// # Что обязана нести запись, если она понадобится снова
//
// Координату (File+Key), ПИСЬМЕННОЕ ОБОСНОВАНИЕ (Why) и ПРЕДИКАТ СНЯТИЯ (Until)
// — наблюдаемое условие, при котором запись уходит. Без обоснования запись
// неотличима от упущения; без предиката снимать её будет некому, и она переживёт
// свой предмет — тот самый класс, который проверка и ловит.
//
// Решать за чужой чарт, «мёртв ключ или его забыли провязать», вправе владелец
// чарта. Ведомость существует ради этого случая: долг не прячется, а называется
// и считается — новая находка роняет прогон немедленно, запись без предмета
// роняет его тоже.
type knobDebtEntry struct {
	// File — путь профиля относительно корня дерева.
	File string
	// Key — путь ключа в том виде, в каком он объявлен.
	Key string
	// Why — почему ключ ещё не закрыт: чего не хватает и кто это решает.
	Why string
	// Until — предикат снятия: наблюдаемое условие, при котором записи здесь
	// больше не место. «Когда дойдут руки» предикатом не является.
	Until string
}

// knobDebt — ведомость. Отсортирована по файлу и ключу.
var knobDebt = []knobDebtEntry{}

func knobDebtKey(file, key string) string { return file + "\x00" + key }

// knobDebtDefects — что не так с самой ведомостью, безотносительно дерева.
//
// Отдельная функция, а не тело теста: с пустой ведомостью тело утверждало бы
// свойство ни о чём, и его способность упасть нечем было бы показать. Здесь она
// показывается инъекцией синтетических записей — тем же кодом, что судит дерево.
func knobDebtDefects(entries []knobDebtEntry) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range entries {
		if d.File == "" || d.Key == "" {
			out = append(out, fmt.Sprintf("запись без координаты: %+v", d))
			continue
		}
		k := knobDebtKey(d.File, d.Key)
		if seen[k] {
			out = append(out, fmt.Sprintf("дубль в ведомости: %s / %s", d.File, d.Key))
		}
		seen[k] = true
		if strings.TrimSpace(d.Why) == "" {
			out = append(out, fmt.Sprintf("%s: %s — запись без письменного обоснования: "+
				"неотличима от упущения, и снять её потом будет не по чему", d.File, d.Key))
		}
		if strings.TrimSpace(d.Until) == "" {
			out = append(out, fmt.Sprintf("%s: %s — запись без предиката снятия: послабление, "+
				"которое не умеет истечь, переживает свой предмет", d.File, d.Key))
		}
	}
	sort.Strings(out)
	return out
}

// TestDeclaredKnobHasAReader — ключ, объявленный в профиле развёртывания,
// обязан иметь читателя в шаблоне чарта либо в шаблоне его родителя.
//
// Прогон печатает объём осмотренного всегда: «ноль новых находок» обязано быть
// отличимо от «ноль прочитанного».
func TestDeclaredKnobHasAReader(t *testing.T) {
	findings, census, err := auditProfileKnobReaders(repoRoot(t))
	if err != nil {
		t.Fatalf("перепись профилей не выполнена: %v", err)
	}
	t.Log(census.String())
	t.Logf("ведомость долга: %d записей", len(knobDebt))

	if census.KeysEnforced == 0 {
		t.Fatal("под требованием читателя ноль ключей — проверка ничего не осмотрела, " +
			"и её молчание не является утверждением о дереве")
	}
	if census.TemplateFiles == 0 {
		t.Fatal("прочитано ноль файлов шаблонов — читателя искать было негде, " +
			"поэтому «читателя нет» сказано обо всём дереве сразу")
	}

	var fresh []string
	debt := map[string]bool{}
	for _, d := range knobDebt {
		debt[knobDebtKey(d.File, d.Key)] = true
	}
	for _, f := range findings {
		if !debt[knobDebtKey(f.File, f.Key)] {
			fresh = append(fresh, f.String())
		}
	}
	sort.Strings(fresh)

	if len(fresh) > 0 {
		t.Fatalf("объявлено без читателя — %d НОВЫХ ключей (всего находок %d, из них в ведомости %d):\n%s\n\n"+
			"Ключ профиля, которого не читает ни шаблон чарта, ни его родитель, до процесса не "+
			"доедет НИКОГДА: значение остаётся в файле. Для поверхности безопасности это хуже "+
			"отсутствия ключа — оператор читает строку как заявление о посадке и распоряжается "+
			"тем, чего нет. Исходов три: провязать читателя, снять ключ из профиля, либо (если "+
			"ключ читает сам helm) объявить его `condition:` зависимости.\n%s",
			len(fresh), len(findings), len(findings)-len(fresh),
			strings.Join(fresh, "\n"), census.String())
	}
}

// knobDebtStale — записи, которым больше нечего исключать: находки с такой
// координатой в дереве нет.
//
// Отдельная функция по той же причине, что и knobDebtDefects: на пустой
// ведомости тело теста молчит по построению, и показать его способность упасть
// можно только инъекцией в ТУ ЖЕ функцию.
func knobDebtStale(entries []knobDebtEntry, live map[string]bool) []string {
	var stale []string
	for _, d := range entries {
		if !live[knobDebtKey(d.File, d.Key)] {
			stale = append(stale, fmt.Sprintf("%s: %s", d.File, d.Key))
		}
	}
	sort.Strings(stale)
	return stale
}

// TestKnobDebtExpiresOnItsOwn — запись ведомости, которой больше нечего
// исключать, роняет прогон.
//
// Без этой половины ведомость пережила бы свой предмет: починенный ключ остался
// бы в списке, следующий читатель принял бы его за действующий долг, а
// освободившееся место унаследовал бы новый дефект с тем же путём.
func TestKnobDebtExpiresOnItsOwn(t *testing.T) {
	findings, census, err := auditProfileKnobReaders(repoRoot(t))
	if err != nil {
		t.Fatalf("перепись профилей не выполнена: %v", err)
	}
	t.Log(census.String())

	live := map[string]bool{}
	for _, f := range findings {
		live[knobDebtKey(f.File, f.Key)] = true
	}
	stale := knobDebtStale(knobDebt, live)
	if len(stale) > 0 {
		t.Fatalf("в ведомости %d записей, которым больше нечего исключать:\n%s\n\n"+
			"Ключ провязан или снят — запись обязана уйти из ведомости тем же изменением. "+
			"Оставленная, она объявляет живым закрытый долг и освобождает место новому "+
			"дефекту с тем же путём.", len(stale), strings.Join(stale, "\n"))
	}
}

// TestKnobDebtIsWellFormed — каждая запись ведомости несёт координату, письменное
// обоснование и предикат снятия, и ни одна не повторяется.
//
// Дубль скрывает, что записей на одну меньше, чем кажется; запись без
// обоснования неотличима от упущения; запись без предиката снятия не умеет
// истечь. Сегодня ведомость пуста, поэтому тест ничего не находит — его
// способность находить показана инъекцией ниже, а не этим прогоном.
func TestKnobDebtIsWellFormed(t *testing.T) {
	t.Logf("ведомость: %d записей", len(knobDebt))
	for _, bad := range knobDebtDefects(knobDebt) {
		t.Error(bad)
	}
}

// TestKnobDebtRulesCatchABareEntry — правила ведомости обязаны краснеть на
// голой записи и МОЛЧАТЬ на полной.
//
// Инъекция здесь не украшение: ведомость пуста, значит обе проверки над ней
// молчат по построению, и «зелено» ничего о них не говорит. Синтетика подаётся
// в ТУ ЖЕ функцию, которая судит дерево, — иначе проверялась бы копия правил, а
// не они сами.
func TestKnobDebtRulesCatchABareEntry(t *testing.T) {
	lawful := knobDebtEntry{
		File:  "deploy/helm/example/values.yaml",
		Key:   "sub.knob",
		Why:   "чарт-владелец решает сам: ручка объявлена под будущий шаблон",
		Until: "шаблон чарта читает .Values.knob либо ключ снят из профиля",
	}
	// (б) МОЛЧАЛИВОЕ НАПРАВЛЕНИЕ — полная запись претензий не вызывает.
	if got := knobDebtDefects([]knobDebtEntry{lawful}); len(got) != 0 {
		t.Errorf("полная запись объявлена дефектной: %v", got)
	}

	// (а) КРАСНОЕ НАПРАВЛЕНИЕ — каждая нехватка называется отдельно.
	noWhy, noUntil, noCoord := lawful, lawful, lawful
	noWhy.Why, noUntil.Until = "", "   "
	noCoord.Key = ""
	cases := map[string]knobDebtEntry{
		"без обоснования": noWhy,
		"без предиката":   noUntil,
		"без координаты":  noCoord,
	}
	for name, e := range cases {
		if got := knobDebtDefects([]knobDebtEntry{e}); len(got) == 0 {
			t.Errorf("запись %s принята: %+v", name, e)
		}
	}
	if got := knobDebtDefects([]knobDebtEntry{lawful, lawful}); len(got) == 0 {
		t.Error("дубль принят — ведомость может объявлять больше записей, чем в ней предметов")
	}

	// Самоистечение — та же пара направлений. Живая находка запись оправдывает;
	// исчезнувшая обязана её обнулить.
	liveKey := map[string]bool{knobDebtKey(lawful.File, lawful.Key): true}
	if got := knobDebtStale([]knobDebtEntry{lawful}, liveKey); len(got) != 0 {
		t.Errorf("запись с живой находкой объявлена просроченной: %v", got)
	}
	if got := knobDebtStale([]knobDebtEntry{lawful}, map[string]bool{}); len(got) == 0 {
		t.Error("запись, которой нечего исключать, принята: послабление переживает свой " +
			"предмет, а освободившееся место унаследует новый дефект с тем же путём")
	}
}
