// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package engineplaces

import (
	"fmt"
	"sort"
	"strings"
)

// Report — человекочитаемая перепись.
//
// Печатает ДВЕ ЕДИНИЦЫ СЧЁТА РАЗДЕЛЬНО (мест · файлов), разложение по роду
// вопроса, каждый вычет с причиной и единицей, объём осмотренного и границы.
// Одна единица без другой не отвечает ни на «сколько чинить», ни на «сколько
// трогать»; перепись без объёма осмотренного не отличает «ноль находок» от
// «ноль прочитанного»; перепись без границ утверждает полноту, которой не имеет.
func (c *Census) Report() string {
	var b strings.Builder

	fmt.Fprintf(&b, "ПЕРЕПИСЬ МЕСТ ОБРАЩЕНИЯ К ВНЕШНЕМУ ДВИЖКУ ПРАВ (Г1, R7-3-01)\n")
	fmt.Fprintf(&b, "дерево: %s\nшаблоны: %s\n", c.Root, strings.Join(c.Patterns, " "))
	fmt.Fprintf(&b, "якорь: %s — объявлений %d, дом %s (резолвит компилятор), дерево дома %s\n\n",
		c.Anchor, c.AnchorDeclarations, c.AnchorPkg, c.AnchorHomeTree)

	if c.Void() {
		fmt.Fprintf(&b, "!! ПЕРЕПИСЬ НЕГОДНА: предпосылка не выполнена (%d)\n", len(c.Errors))
		for _, e := range c.Errors {
			fmt.Fprintf(&b, "   %s\n", e)
		}
		b.WriteString("   «мест 0» и «пакет не загрузился» — РАЗНЫЕ исходы; этот — второй\n\n")
	}

	fmt.Fprintf(&b, "── ПЕРЕПИСЬ (две единицы счёта, раздельно)\n")
	fmt.Fprintf(&b, "мест обращения : %d\n", len(c.Places))
	fmt.Fprintf(&b, "файлов         : %d\n\n", c.FileCount())

	fmt.Fprintf(&b, "── ПО РОДУ ВОПРОСА (снятие требует от каждого рода РАЗНОГО)\n")
	kc := c.KindCounts()
	for _, k := range Kinds() {
		mark := ""
		if k == KindPlumbing {
			mark = "   (не вопрос)"
		}
		fmt.Fprintf(&b, "%-30s %4d%s\n", k, kc[k], mark)
	}
	if kc[""] > 0 {
		fmt.Fprintf(&b, "%-30s %4d   ← НАХОДКА: род не назван\n", "без рода", kc[""])
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "── МЕТОДЫ ЯКОРНОГО ТИПА: перечень ВЫВЕДЕН ИЗ ТИПА (%d)\n", len(c.Methods)+len(c.UnclassifiedMethods))
	byKind := map[string][]string{}
	for _, m := range c.Methods {
		byKind[m.Kind] = append(byKind[m.Kind], m.Method)
	}
	for _, k := range Kinds() {
		if len(byKind[k]) == 0 {
			continue
		}
		sort.Strings(byKind[k])
		fmt.Fprintf(&b, "%-30s %s\n", k, strings.Join(byKind[k], " "))
	}
	if len(c.UnclassifiedMethods) > 0 {
		fmt.Fprintf(&b, "НАХОДКА — методы без рода (%d): %s\n",
			len(c.UnclassifiedMethods), strings.Join(c.UnclassifiedMethods, " "))
		b.WriteString("   перечень родов разошёлся с типом; выписанный перечень расходится МОЛЧА\n")
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "── ПОРТЫ, КОТОРЫМ ЯКОРНЫЙ ТИП УДОВЛЕТВОРЯЕТ СТРУКТУРНО (%d)\n", len(c.Ports))
	amb := 0
	for _, p := range c.Ports {
		if p.Ambiguous {
			amb++
		}
	}
	fmt.Fprintf(&b, "из них структурно неоднозначных (есть реализация вне дома движка): %d\n", amb)
	for _, p := range c.Ports {
		flag := " "
		foreign := "все реализации в доме движка"
		if p.Ambiguous {
			flag = "!"
			foreign = "вне дома: " + strings.Join(shortAll(p.ForeignImpls), ", ")
		}
		fmt.Fprintf(&b, " %s %-70s методов %2d  реализаций %2d  %s\n",
			flag, shortPkg(p.Type), p.Methods, len(p.Impls), foreign)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "── ВЫЧТЕНО (закрытый перечень категорий, каждое — с причиной)\n")
	byCat := map[string][]Subtraction{}
	for _, s := range c.Subtractions {
		byCat[s.Category] = append(byCat[s.Category], s)
	}
	for _, cat := range Categories() {
		list := byCat[cat]
		places, files := 0, 0
		for _, s := range list {
			if s.Unit == UnitFile {
				files++
			} else {
				places++
			}
		}
		fmt.Fprintf(&b, "%-26s всего %4d  (мест %d · файлов %d)\n", cat, len(list), places, files)
		if len(list) == 0 {
			b.WriteString("     — предмета на этом дереве нет; послабление истекает само\n")
			continue
		}
		for i, s := range list {
			if i == 3 {
				fmt.Fprintf(&b, "     … ещё %d\n", len(list)-3)
				break
			}
			if s.Line > 0 {
				fmt.Fprintf(&b, "     %s:%d %s — %s\n", s.File, s.Line, s.Method, s.Reason)
			} else {
				fmt.Fprintf(&b, "     %s — %s\n", s.File, s.Reason)
			}
		}
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "── СЕГМЕНТЫ ОСНАСТКИ ПРОБ (перечень истекает сам)\n")
	for _, s := range c.TestRigSegments {
		note := ""
		if s.Matched == 0 {
			note = "   ← НАХОДКА: сегменту нечего исключать"
		}
		fmt.Fprintf(&b, " %-20s пакетов %d%s\n", s.Segment, s.Matched, note)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "── ТОТ ЖЕ ВОПРОС НАИВНЫМ ПРЕДИКАТОМ (другая единица счёта, НЕ вычитается)\n")
	fmt.Fprintf(&b, "подстрока %q: непроверочных файлов %d, проверочных %d\n",
		c.NameOnly.Needle, c.NameOnly.Files, c.NameOnly.TestFiles)
	r := c.NameOnly.Reconciled
	fmt.Fprintf(&b, "  из них: с местами %d · дом адаптера %d · связывание %d · порождённых %d · "+
		"только проза %d · оснастка проб %d · второй клиент %d\n",
		r.WithPlaces, r.AdapterHome, r.Wiring, r.Generated, r.Prose, r.TestRig, r.SecondClient)
	fmt.Fprintf(&b, "  и ОБРАТНО: файлов с настоящими местами, которых имя НЕ назвало бы — %d "+
		"(перепись по имени занижена, и занижение не видно)\n\n", r.MissedByName)

	fmt.Fprintf(&b, "── ГРАНИЦЫ ПЕРЕПИСИ (то, чего она не видит, названо вслух)\n")
	for _, bd := range c.Boundaries {
		fmt.Fprintf(&b, " • %s: %d\n   %s\n", bd.Name, bd.Count, bd.Note)
		for i, it := range bd.Items {
			if i == 10 {
				fmt.Fprintf(&b, "     … ещё %d\n", len(bd.Items)-10)
				break
			}
			fmt.Fprintf(&b, "     %s\n", it)
		}
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "── ОБЪЁМ ОСМОТРЕННОГО («ноль находок» ≠ «ноль прочитанного»)\n")
	fmt.Fprintf(&b, "пакетов своего дерева запрошено %d, загружено и протипизировано %d\n",
		c.Scan.Requested, c.Scan.Loaded)
	fmt.Fprintf(&b, "непроверочных файлов прочитано %d · вызовов-методов осмотрено %d · именованных типов %d\n",
		c.Scan.ProdFiles, c.Scan.CallSites, c.Scan.NamedTypes)
	fmt.Fprintf(&b, "пакетов без непроверочных файлов %d · ошибок типизации %d\n",
		len(c.Scan.SkippedPkgs), len(c.Errors))

	return b.String()
}

// Findings — перечень мест, пригодный для чтения человеком построчно.
func (c *Census) Findings() string {
	var b strings.Builder
	for _, p := range c.Places {
		via := "прямо"
		if p.Via != "" {
			via = "через " + p.Via
			if p.ViaAmbiguous {
				via += " (порт неоднозначен)"
			}
		}
		fmt.Fprintf(&b, "%s:%d  %s  [%s]  %s\n", p.File, p.Line, p.Method, p.Kind, via)
	}
	return b.String()
}
