// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package moduleseedparity — сверка раздела `seed` манифеста модуля с ЖИВОЙ
// базой (задача #1891, вторая половина её предиката).
//
// # Сверяются ВСЕ ЧЕТЫРЕ подраздела; наружу выведена только НЕВЫРАЗИМОСТЬ
//
// Раздел несёт четыре подраздела — служебные записи, группы, выдачи и
// вступления, — и сверка судит каждый. Из-под вердикта выведена не половина
// раздела, а один ВИД строки: тот, который форма манифеста выразить не может.
//
// Вид назван и ВЫВОДИТСЯ из самой формы, а не объявлен списком: у выдачи есть
// ключ `roleId` и нет ни одного ключа для ОТНОШЕНИЯ, поэтому выдача отношением
// необъявима ни при каком написании; группа необъявима по следствию — валидатор
// связности требует, чтобы заведённая группа была названа выдачей манифеста
// (`ErrGroupNeverGranted`), а такой выдачи у неё нет. Разбор и исходы — #1936.
//
// Невыразимое НЕ ОТБРАСЫВАЕТСЯ молча: [Compare] возвращает его отдельным
// перечнем, потребитель печатает каждую строку по имени, а перепись называет
// его числом. Появится у выдачи ключ отношения — проба предпосылки
// (`TestBindingFormStillCannotExpressARelationGrant`) покраснеет и потребует
// расширить сверку. Ведомости прощённых у гейта нет: прощать нечего, пока форма
// не изменилась, и прощение не понадобится, когда изменится.
//
// # Число границы считается ПО ВЛАДЕЛЬЦУ, и это не педантизм
//
// Здесь стояло одно число на все живые строки — «выдач живых 8, из них
// выразимых формой 0», — и оно складывало два разных предмета. Строка без
// модуля-владельца (`kacho-api-gateway`, `kacho-bootstrap-admin`, `user:*`,
// владельческая привязка системного аккаунта) манифестом МОДУЛЯ невыразима
// by construction: объявлять её некому, и её отсутствие среди объявленного —
// верно, а не пробел. Пробел — только строка, у которой владелец ЕСТЬ, а формы
// нет; таких из восьми **две** (`kacho-compute` и `kacho-vpc` → `system_viewer`).
// Перепись поэтому печатает по каждому подразделу четыре величины: живых ·
// без модуля-владельца · с владельцем · из них формой невыразимых.
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

// Group — группа, заводимая установкой модуля.
type Group struct {
	Account     string
	Name        string
	Description string
}

func (g Group) String() string {
	return fmt.Sprintf("%s/%s %q", g.Account, g.Name, g.Description)
}

// Binding — выдача, которую делает установка модуля.
//
// Вид субъекта пишется по-МАНИФЕСТНОМУ (`serviceAccount`, `group`): второе
// написание того же предмета разошлось бы с первым, поэтому перевод живой
// строки (`service_account`) делается ОДИН раз, на чтении, а не по месту
// сравнения.
//
// Якорь области хранится в точечной форме (`iam.cluster`) — так его пишет
// манифест, и так же переводится живая пара `resource_type`/`resource_id`.
type Binding struct {
	SubjectType string
	SubjectName string
	// RoleID — выдача РОЛЬЮ: единственная форма, которую манифест умеет
	// объявить.
	RoleID string
	// Relation — выдача ОТНОШЕНИЕМ. Ключа для неё у формы манифеста нет ни
	// одного, поэтому строка с непустым Relation невыразима by construction
	// (#1936). Поле здесь ради того, чтобы это было ВИДНО, а не выпадало из
	// чтения молча.
	Relation  string
	ScopeType string
	ScopeID   string
}

func (b Binding) String() string {
	granted := "роль " + b.RoleID
	if b.Relation != "" {
		granted = "отношение " + b.Relation
	}
	return fmt.Sprintf("%s %s → %s на %s/%s", b.SubjectType, b.SubjectName, granted, b.ScopeType, b.ScopeID)
}

// ExpressibleByForm — умеет ли форма манифеста объявить эту выдачу.
//
// Предикат ВЫВЕДЕН из формы, а не перечисляет исключения: у выдачи есть ключ
// роли и нет ключа отношения, поэтому выдача отношением необъявима при любом
// написании. Истечёт вместе с формой: появится ключ — проба предпосылки
// покраснеет, и этот предикат обязан быть снят вместе с ней.
func (b Binding) ExpressibleByForm() bool { return b.Relation == "" }

// ModuleState — обе стороны сверки по одному модулю.
//
// Живые строки приходят сюда УЖЕ отнесёнными к модулю-владельцу: строки без
// владельца сверке не подлежат и считаются переписью отдельно.
type ModuleState struct {
	Module          string
	ManifestFile    string
	DeclaredSA      []ServiceAccount
	LiveSA          []ServiceAccount
	DeclaredGroup   []Group
	LiveGroup       []Group
	DeclaredBinding []Binding
	LiveBinding     []Binding
	DeclaredJoin    []Join
	LiveJoin        []Join
}

// Subsection — перепись одного подраздела посева.
//
// Величин ЧЕТЫРЕ, и ни одну нельзя выбросить, не потеряв различия, ради
// которого перепись и печатается: `Live` отвечает, читалось ли вообще что-то;
// `Ownerless` отделяет невыразимое by construction (объявлять некому) от
// пробела; `Owned` называет предмет сверки; `Inexpressible` — ту его часть,
// которой форма не умеет. Одно число вместо четырёх скрывало бы ровно тот
// случай, ради которого граница названа.
type Subsection struct {
	Declared      int
	Live          int
	Ownerless     int
	Owned         int
	Inexpressible int
}

func (s Subsection) String() string {
	return fmt.Sprintf("объявлено %d · живых %d · без модуля-владельца %d · с владельцем %d, из них формой невыразимы %d",
		s.Declared, s.Live, s.Ownerless, s.Owned, s.Inexpressible)
}

// Census — объём осмотренного. Печатается ВСЕГДА и ДО вердикта: «находок ноль»
// обязано быть отличимо от «прочитано ноль».
type Census struct {
	Manifests int
	SA        Subsection
	Groups    Subsection
	Bindings  Subsection
	Joins     Subsection
}

func (c Census) String() string {
	return fmt.Sprintf(
		"манифестов %d\n    служебные записи: %s\n    группы:           %s\n"+
			"    выдачи:           %s\n    вступления:       %s",
		c.Manifests, c.SA, c.Groups, c.Bindings, c.Joins)
}

// Result — исход сверки. ДВА перечня, а не один, и это несущее различие:
// расхождение чинится правкой манифеста, невыразимое — правкой ФОРМЫ, и
// разными людьми в разных изменениях. Сложив их в один список, гейт требовал бы
// от автора манифеста починить то, чего он написать не может.
type Result struct {
	// Findings — расхождения объявленного с живым. Гейт на них падает.
	Findings []string
	// Inexpressible — живые строки модуля, которые форма манифеста выразить не
	// может (#1936). Гейт их ПЕЧАТАЕТ и на них не падает: пока формы нет, автор
	// манифеста бессилен. Молча отбросить их нельзя — тогда пробел был бы
	// неотличим от согласия.
	Inexpressible []string
}

// Compare сравнивает объявленное с живым по каждому модулю.
//
// Расхождение называется В ОБЕ СТОРОНЫ: строка живая и не объявленная —
// манифест неполон; объявленная и не живая — манифест обещает то, чего
// установка не завела. Второе не мягче первого: применитель, когда он появится,
// заведёт по объявлению.
//
// Обход ОДИН: перечни находок и невыразимого производятся вместе, поэтому
// вызывающий не может прочитать один и забыть про другой.
func Compare(states []ModuleState) Result {
	var res Result
	for _, st := range states {
		res.Findings = append(res.Findings, diffSet(st,
			"служебная запись ЖИВЁТ и не объявлена",
			"служебная запись ОБЪЯВЛЕНА и не живёт",
			keysOfSA(st.DeclaredSA), keysOfSA(st.LiveSA))...)
		res.Findings = append(res.Findings, diffSet(st,
			"вступление ЖИВЁТ и не объявлено",
			"вступление ОБЪЯВЛЕНО и не живёт",
			keysOfJoin(st.DeclaredJoin), keysOfJoin(st.LiveJoin))...)

		expressibleBinding, inexpressibleBinding := SplitBindings(st.LiveBinding)
		res.Findings = append(res.Findings, diffSet(st,
			"выдача ЖИВЁТ и не объявлена",
			"выдача ОБЪЯВЛЕНА и не живёт",
			keysOfBinding(st.DeclaredBinding), keysOfBinding(expressibleBinding))...)
		res.Inexpressible = append(res.Inexpressible,
			nameEach(st, "выдача формой манифеста НЕВЫРАЗИМА (выдача отношением, #1936)",
				textsOfBinding(inexpressibleBinding))...)

		expressibleGroup, inexpressibleGroup := SplitGroups(st.LiveGroup, st.LiveBinding)
		res.Findings = append(res.Findings, diffSet(st,
			"группа ЖИВЁТ и не объявлена",
			"группа ОБЪЯВЛЕНА и не живёт",
			keysOfGroup(st.DeclaredGroup), keysOfGroup(expressibleGroup))...)
		res.Inexpressible = append(res.Inexpressible,
			nameEach(st, "группа формой манифеста НЕВЫРАЗИМА (наделена только отношением, #1936)",
				textsOfGroup(inexpressibleGroup))...)
	}
	sort.Strings(res.Findings)
	sort.Strings(res.Inexpressible)
	return res
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

// SplitBindings делит живые выдачи модуля на те, что форма манифеста объявить
// умеет, и те, что не умеет.
//
// Предикат ВЫВЕДЕН из формы (см. [Binding.ExpressibleByForm]), а не перечисляет
// имена: перечень имён пережил бы свой предмет молча, а предикат истечёт вместе
// с формой.
func SplitBindings(live []Binding) (expressible, inexpressible []Binding) {
	for _, b := range live {
		if b.ExpressibleByForm() {
			expressible = append(expressible, b)
			continue
		}
		inexpressible = append(inexpressible, b)
	}
	return expressible, inexpressible
}

// SplitGroups делит живые группы модуля тем же вопросом — но ответ на него
// даёт не сама группа, а её ВЫДАЧИ.
//
// Валидатор связности требует, чтобы заведённая группа была названа хотя бы
// одной выдачей манифеста (`ErrGroupNeverGranted`); значит группа объявима ровно
// тогда, когда объявима хоть одна выдача на неё. Группа, которую наделяют одним
// отношением, необъявима по СЛЕДСТВИЮ, а не отдельным правилом, — поэтому здесь
// нет второго перечня исключений, который разошёлся бы с первым.
func SplitGroups(live []Group, bindings []Binding) (expressible, inexpressible []Group) {
	granted := map[string]bool{}
	for _, b := range bindings {
		if b.SubjectType == SubjectTypeGroup && b.ExpressibleByForm() {
			granted[b.SubjectName] = true
		}
	}
	for _, g := range live {
		if granted[g.Name] {
			expressible = append(expressible, g)
			continue
		}
		inexpressible = append(inexpressible, g)
	}
	return expressible, inexpressible
}

// SubjectTypeGroup / SubjectTypeServiceAccount — написание вида субъекта,
// принятое МАНИФЕСТОМ. Живая строка переводится в него ОДИН раз, на чтении:
// второе написание того же предмета разошлось бы с первым молча.
const (
	SubjectTypeGroup          = "group"
	SubjectTypeServiceAccount = "serviceAccount"
)

// nameEach — строки, выведенные из-под вердикта, названные ПОИМЁННО и с
// модулем-владельцем. Число без имён нечитаемо: по нему нельзя ни проверить
// границу, ни заметить, что она перестала быть верной.
func nameEach(st ModuleState, what string, texts []string) []string {
	out := make([]string, 0, len(texts))
	for _, text := range texts {
		out = append(out, fmt.Sprintf("модуль %s (%s): %s: %s", st.Module, st.ManifestFile, what, text))
	}
	return out
}

func keysOfBinding(in []Binding) map[string]string {
	out := map[string]string{}
	for _, b := range in {
		out[strings.Join([]string{b.SubjectType, b.SubjectName, b.RoleID, b.Relation, b.ScopeType, b.ScopeID}, "\x00")] = b.String()
	}
	return out
}

func textsOfBinding(in []Binding) []string {
	out := make([]string, 0, len(in))
	for _, b := range in {
		out = append(out, b.String())
	}
	return out
}

func keysOfGroup(in []Group) map[string]string {
	out := map[string]string{}
	for _, g := range in {
		out[strings.Join([]string{g.Account, g.Name, g.Description}, "\x00")] = g.String()
	}
	return out
}

func textsOfGroup(in []Group) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		out = append(out, g.String())
	}
	return out
}
