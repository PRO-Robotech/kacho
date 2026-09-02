// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package moduleseedparity — сверка раздела `seed` манифеста модуля с ЖИВОЙ
// базой (задача #1891, вторая половина её предиката).
//
// # Что сверяется, и почему именно это
//
// Раздел несёт четыре подраздела. Сверяются ДВА — служебные записи модуля и его
// вступления в чужие группы, — и это не выбор удобства, а ГРАНИЦА ФОРМЫ,
// выведенная из самой формы, а не объявленная списком:
//
//   - `seed.groups` объявить нельзя: валидатор связности требует, чтобы
//     заведённая группа была названа хотя бы одной выдачей манифеста
//     (`ErrGroupNeverGranted`), а выдача обязана назвать `roleId`. Обе живые
//     группы платформы наделены ОТНОШЕНИЕМ (`quota_reader`, `fga_writer`), не
//     ролью, и ключа для отношения у выдачи нет ни одного;
//   - `seed.accessBindings` объявить нечем по той же причине: живых выдач
//     РОЛЬЮ, которые заводила бы установка модуля, ноль.
//
// Граница ВЫВОДИТСЯ: строка считается выразимой, если её вид формой описуем
// (выдача — по непустой роли, группа — по наличию у неё такой выдачи), и
// перепись печатает оба числа. Появится у выдачи ключ отношения — проба
// предпосылки (`TestBindingFormStillCannotExpressARelationGrant`) покраснеет и
// потребует расширить сверку. Ведомости прощённых у гейта поэтому НЕТ: прощать
// нечего, пока форма не изменилась, и прощение не понадобится, когда изменится.
//
// # Владелец живой строки выводится ИМЕНЕМ, а не приписывается
//
// Служебная запись модуля названа `kacho-<служба>`; служба переводится в модуль
// закрытого набора платформы (`pkg/platformmodules`). Запись, чьё имя этому не
// отвечает (`kacho-api-gateway`, `kacho-bootstrap-admin`), манифестом модуля
// невыразима by construction — у неё нет модуля-владельца, — и считается
// отдельно. Иначе её отсутствие среди объявленных читалось бы как неполнота.
package moduleseedparity

import (
	"fmt"
	"sort"
	"strings"
)

// ServiceAccount — служебная запись: то, что сверяется, и ничего сверх.
// Идентификатор сюда НЕ входит: живой его производит выражение внутри миграции
// (`'sva' || substr(md5(name), 1, 17)`), то есть он есть частность записи, а не
// то, что объявляет манифест. Сверять по нему значило бы требовать от манифеста
// воспроизвести случайность.
type ServiceAccount struct {
	Account     string
	Name        string
	Description string
}

func (s ServiceAccount) String() string {
	return fmt.Sprintf("%s/%s %q", s.Account, s.Name, s.Description)
}

// Join — вступление служебной записи в чужую группу. Обе стороны адресуются
// ПАРОЙ (аккаунт, имя): так они уникальны в продукте.
type Join struct {
	AccountName  string
	SAName       string
	GroupAccount string
	GroupName    string
}

func (j Join) String() string {
	return fmt.Sprintf("%s/%s → %s/%s", j.AccountName, j.SAName, j.GroupAccount, j.GroupName)
}

// ModuleState — обе стороны сверки по одному модулю.
type ModuleState struct {
	Module       string
	ManifestFile string
	DeclaredSA   []ServiceAccount
	LiveSA       []ServiceAccount
	DeclaredJoin []Join
	LiveJoin     []Join
}

// Census — объём осмотренного. Печатается ВСЕГДА и ДО вердикта: «находок ноль»
// обязано быть отличимо от «прочитано ноль».
type Census struct {
	Manifests int
	// DeclaredSA / DeclaredJoin — сколько объявлено манифестами.
	DeclaredSA   int
	DeclaredJoin int
	// LiveSA / LiveJoin — сколько лежит в базе.
	LiveSA   int
	LiveJoin int
	// OwnerlessSA / OwnerlessJoin — живое, у чего модуля-владельца нет:
	// манифестом невыразимо by construction, а не пропущено.
	OwnerlessSA   int
	OwnerlessJoin int
	// LiveGroups / ExpressibleGroups и LiveBindings / ExpressibleBindings —
	// граница формы, названная числом с обеих сторон.
	LiveGroups          int
	ExpressibleGroups   int
	LiveBindings        int
	ExpressibleBindings int
}

func (c Census) String() string {
	return fmt.Sprintf(
		"манифестов %d · служебных записей объявлено %d · живых %d (без модуля-владельца %d) · "+
			"вступлений объявлено %d · живых %d (без модуля-владельца %d) · "+
			"групп живых %d, из них выразимых формой %d · выдач живых %d, из них выразимых формой %d",
		c.Manifests, c.DeclaredSA, c.LiveSA, c.OwnerlessSA,
		c.DeclaredJoin, c.LiveJoin, c.OwnerlessJoin,
		c.LiveGroups, c.ExpressibleGroups, c.LiveBindings, c.ExpressibleBindings)
}

// Diff сравнивает объявленное с живым по каждому модулю и возвращает находки.
//
// Расхождение называется В ОБЕ СТОРОНЫ: строка живая и не объявленная —
// манифест неполон; объявленная и не живая — манифест обещает то, чего
// установка не завела. Второе не мягче первого: применитель, когда он появится,
// заведёт по объявлению.
func Diff(states []ModuleState) []string {
	var findings []string
	for _, st := range states {
		findings = append(findings, diffSet(st,
			"служебная запись ЖИВЁТ и не объявлена",
			"служебная запись ОБЪЯВЛЕНА и не живёт",
			keysOfSA(st.DeclaredSA), keysOfSA(st.LiveSA))...)
		findings = append(findings, diffSet(st,
			"вступление ЖИВЁТ и не объявлено",
			"вступление ОБЪЯВЛЕНО и не живёт",
			keysOfJoin(st.DeclaredJoin), keysOfJoin(st.LiveJoin))...)
	}
	sort.Strings(findings)
	return findings
}

// diffSet — обе стороны одного подраздела. Формулировки передаются целиком, а
// не склеиваются из имени подраздела: «запись» и «вступление» разного рода, и
// склейка дала бы отказ, который читается как опечатка в самом гейте.
func diffSet(st ModuleState, liveOnly, declaredOnly string, declared, live map[string]string) []string {
	var findings []string
	for k, text := range live {
		if _, ok := declared[k]; !ok {
			findings = append(findings, fmt.Sprintf("модуль %s (%s): %s: %s",
				st.Module, st.ManifestFile, liveOnly, text))
		}
	}
	for k, text := range declared {
		if _, ok := live[k]; !ok {
			findings = append(findings, fmt.Sprintf("модуль %s (%s): %s: %s",
				st.Module, st.ManifestFile, declaredOnly, text))
		}
	}
	return findings
}

func keysOfSA(in []ServiceAccount) map[string]string {
	out := map[string]string{}
	for _, s := range in {
		out[strings.Join([]string{s.Account, s.Name, s.Description}, "\x00")] = s.String()
	}
	return out
}

func keysOfJoin(in []Join) map[string]string {
	out := map[string]string{}
	for _, j := range in {
		out[strings.Join([]string{j.AccountName, j.SAName, j.GroupAccount, j.GroupName}, "\x00")] = j.String()
	}
	return out
}
