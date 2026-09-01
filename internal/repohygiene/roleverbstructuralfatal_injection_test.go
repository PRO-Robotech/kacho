// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// Инъекция гейта IAM-RV-1-08 (загрузочная половина) — В ОБЕ СТОРОНЫ.
//
// Признак воспроизводится над СИНТЕТИЧЕСКИМ композиционным корнем: правкой
// настоящего дерева инъекция не ставится — она рвала бы чужие прогоны в общей
// рабочей копии. Синтетика берёт ту же оболочку, что у соседнего гейта полосы
// старта (`bootSrc`), и оболочка эта не декоративна: она заворачивает тело в
// литерал функции, то есть воспроизводит ровно ту раскладку корня, на которой
// признак ошибся при первом прогоне (см. ось «граница литерала» ниже).
//
// Осей шесть, и у каждой находки есть законный близнец: без близнецов молчание
// гейта на дереве было бы неотличимо от его смерти.

// TestIAMRV108_InjectionRedWhenTheStructuralBranchIsGone — блок структурной
// полосы снят: перепись присвоена, ветки нет → находка.
//
// Это тот самый вход, которым свойство снималось молча: шесть строк из корня,
// сборка проходит, соседние гейты полосы зелены.
func TestIAMRV108_InjectionRedWhenTheStructuralBranchIsGone(t *testing.T) {
	src := bootSrc(`		verbs, verr := seed.ReseedSystemRoleVerbs(ctx, kachoRepo, pool, obs)
		if verr != nil {
			logger.Error("пересчёт отказал", slog.Any("err", verr))
		}
		logger.Info("перепись", slog.Int("roles_examined", verbs.Examined))`)
	rep, err := inspectStructuralFatal("zz_boot.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(rep.ReseedCensusIdents) != 1 {
		t.Fatalf("признак не привязал перепись досева: %v — судить о структурной полосе "+
			"нечем, и находка ниже была бы получена не по той причине", rep.ReseedCensusIdents)
	}
	if len(rep.StructuralBranches) != 0 {
		t.Fatalf("признак нашёл структурную ветку там, где её нет: %v — гейт не способен "+
			"покраснеть на входе, которым свойство и снимается", rep.StructuralBranches)
	}
}

// TestIAMRV108_InjectionRedOnABranchThatOnlyReports — ветка структурной полосы
// есть, но старт она не прерывает → находка.
//
// Ось коварнее первой: блок на месте, читается как исполненное решение, и
// отличить его от исполненного можно только по тому, ЧТО в теле.
func TestIAMRV108_InjectionRedOnABranchThatOnlyReports(t *testing.T) {
	src := bootSrc(`		verbs, _ := seed.ReseedSystemRoleVerbs(ctx, kachoRepo, pool, obs)
		if verbs.Structural() {
			logger.Error("пересеяно 0", slog.Int("roles_examined", verbs.Examined))
		}`)
	rep, err := inspectStructuralFatal("zz_boot.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(rep.StructuralBranches) != 1 {
		t.Fatalf("признак не опознал ветку структурной полосы: %v", rep.StructuralBranches)
	}
	if len(rep.NonAborting) != 1 {
		t.Fatalf("признак МОЛЧИТ на ветке, которая только сообщает (не прерывающих %d) — "+
			"полоса неотличима от транзиентной, и различие между ними и есть предмет",
			len(rep.NonAborting))
	}
}

// TestIAMRV108_InjectionSilentOnTheLawfulForm — ЗАКОННЫЙ БЛИЗНЕЦ: ветка есть и
// возвращает ненулевую ошибку. Гейт обязан молчать.
//
// Транзиентная ветка стоит рядом и старт НЕ прерывает — это тоже часть близнеца:
// гейт, требующий прерывания от всякой ветки после вызова, краснел бы на ней, а
// она обязана оставаться непрерывающей.
func TestIAMRV108_InjectionSilentOnTheLawfulForm(t *testing.T) {
	src := bootSrc(`		verbs, verr := seed.ReseedSystemRoleVerbs(ctx, kachoRepo, pool, obs)
		if verr != nil {
			logger.Error("пересчёт отказал", slog.Any("err", verr))
		}
		if verbs.Structural() {
			return fmt.Errorf("осмотрено %d, пересеяно 0: %w", verbs.Examined, verr)
		}`)
	rep, err := inspectStructuralFatal("zz_boot.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(rep.StructuralBranches) != 1 || len(rep.NonAborting) != 0 {
		t.Fatalf("гейт краснеет на ЗАКОННОЙ форме: структурных веток %v, не прерывающих %v — "+
			"он ловит форму, а не существо, и первый же ложный срабат его отключит",
			rep.StructuralBranches, rep.NonAborting)
	}
	if rep.SiblingBranches == 0 {
		t.Fatal("перепись прочих веток пуста: транзиентная ветка не осмотрена вовсе, значит " +
			"близнец «непрерывающая ветка обязана быть пропущена» ничего не доказал")
	}
}

// TestIAMRV108_InjectionSilentOnProcessExitForm — ВТОРАЯ законная форма
// прерывания: завершение процесса вместо возврата ошибки.
//
// Распознаватель, знающий одну форму, оставил бы вторую вне наблюдения — и всё
// записанное в ней оказалось бы не нарушением, а невидимостью.
func TestIAMRV108_InjectionSilentOnProcessExitForm(t *testing.T) {
	src := bootSrc(`		verbs, _ := seed.ReseedSystemRoleVerbs(ctx, kachoRepo, pool, obs)
		if verbs.Structural() {
			logger.Fatalf("осмотрено %d, пересеяно 0", verbs.Examined)
		}`)
	rep, err := inspectStructuralFatal("zz_boot.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(rep.NonAborting) != 0 {
		t.Fatalf("завершение процесса не признано прерыванием старта: %v", rep.NonAborting)
	}
}

// TestIAMRV108_InjectionSilentOnAForeignStructuralCall — ЗАКОННЫЙ БЛИЗНЕЦ:
// одноимённый метод НА ЧУЖОМ предмете рядом с исправной веткой.
//
// Метод `Structural` в дереве не один — так же назван метод сценария у стенда
// замеров. Признак, судящий по ИМЕНИ МЕТОДА, зачёл бы чужой вызов за исполненное
// решение; признак, судящий по имени вместе с ПОЛУЧАТЕЛЕМ переписи, — не зачтёт.
func TestIAMRV108_InjectionSilentOnAForeignStructuralCall(t *testing.T) {
	src := bootSrc(`		verbs, _ := seed.ReseedSystemRoleVerbs(ctx, kachoRepo, pool, obs)
		if scenario.Structural() {
			logger.Info("чужой предмет, старт не роняет")
		}
		if verbs.Structural() {
			return fmt.Errorf("пересеяно 0")
		}`)
	rep, err := inspectStructuralFatal("zz_boot.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(rep.StructuralBranches) != 1 {
		t.Fatalf("чужой вызов `Structural` зачтён полосой этого досева либо потерян: %v — "+
			"привязка к получателю переписи не работает", rep.StructuralBranches)
	}
	if len(rep.NonAborting) != 0 {
		t.Fatalf("непрерывающая ветка ЧУЖОГО предмета объявлена находкой: %v", rep.NonAborting)
	}
}

// TestIAMRV108_InjectionCensusIsBoundInsideTheClosureNotOutside — ГРАНИЦА
// ЛИТЕРАЛА ФУНКЦИИ.
//
// Задача старта заводится как `tasks = append(tasks, func() error { … })`, и это
// присваивание СОДЕРЖИТ вызов досева текстуально, не получая его переписи.
// Признак, спускавшийся внутрь литерала, привязывал перепись к `tasks` и требовал
// структурную ветку от него — то есть краснел на исправном дереве. Найдено
// собственной инъекцией, а не чтением, и записано осью, чтобы не вернулось.
func TestIAMRV108_InjectionCensusIsBoundInsideTheClosureNotOutside(t *testing.T) {
	src := bootSrc(`		verbs, _ := seed.ReseedSystemRoleVerbs(ctx, kachoRepo, pool, obs)
		if verbs.Structural() {
			return fmt.Errorf("пересеяно 0")
		}`)
	rep, err := inspectStructuralFatal("zz_boot.go", src)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(rep.ReseedCensusIdents) != 1 || rep.ReseedCensusIdents[0] != "verbs" {
		t.Fatalf("перепись привязана не к тому имени: %v — ожидалось ровно `[verbs]`. "+
			"Имя снаружи литерала переписи не получает, и требовать от него структурную "+
			"ветку значит краснеть на исправном дереве", rep.ReseedCensusIdents)
	}
}

// TestIAMRV108_InjectionPremiseIsCheckedNotAssumed — ПРЕДПОСЫЛКА: корень без
// вызовов пакета досева.
//
// Перепись обязана давать ноль вызовов, чтобы «ноль структурных веток» было
// отличимо от «файл не разобран»: на этом входе гейт объявляет свою предпосылку
// неверной, а не молчит.
func TestIAMRV108_InjectionPremiseIsCheckedNotAssumed(t *testing.T) {
	rep, err := inspectStructuralFatal("zz_boot.go", bootSrc(`		logger.Info("ничего не досеваем")`))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if rep.SeedCalls != 0 || len(rep.ReseedCensusIdents) != 0 {
		t.Fatalf("признак видит досев там, где его нет: вызовов %d, переписей %v",
			rep.SeedCalls, rep.ReseedCensusIdents)
	}
}
