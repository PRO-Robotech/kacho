// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"fmt"
	"sort"
	"strings"
)

// Мир, в котором задаётся вопросник по ПОЛНОЙ модели, и его перевод на обе
// стороны сравнения.
//
// # Почему мир один, а раскладки две
//
// Сторона движка получает мир РАЗВЁРНУТЫМ: структурные указатели, строки фактов и
// материализованный состав — то, что сегодня пишет реконсайлер (привязка ×
// объекты под её селектором × глаголы её роли × её субъекты). Сторона формы E
// получает тот же мир НЕразвёрнутым: зеркало, рёбра родительства, те же строки
// фактов и сама привязка со своим правилом-селектором.
//
// Разворот на стороне движка считается ЗДЕСЬ, на Go, по графу мира; содержание
// области у формы E считается в SQL рекурсивным обходом рёбер. Это две
// независимые реализации одного понятия «объект лежит в области выдачи», и их
// согласие — содержательная часть доказательства. Взять одну и подать обеим
// значило бы доказать, что программа равна самой себе.
//
// # Что мир обязан содержать, чтобы вопрос не был вырожден
//
// Два арендатора (иначе конъюнкт области тождественно истинен — на этом первый
// заход XC-10 и был отвергнут), вложенная группа, владелец аккаунта отдельно от
// его администратора, администратор кластера, объект, к которому не ведёт ни
// одна привязка (тип, привязанный к кластеру), объект с публичным чтением через
// подстановочный субъект, и зеркальный субъект чужого арендатора — он обязан
// получать разрешение ТАМ, где свой получает отказ. Без зеркального субъекта
// «отказано на чужом» неотличимо от «отказано всем, потому что засев не доехал».

// Идентификаторы мира. Фиксированные строки: сравниваются формы, а разница в
// идентификаторах была бы разницей в данных.
const (
	fmClusterObj = "cluster:cluster_kacho_root"

	fmHomeAccount    = "account:acc-home"
	fmForeignAccount = "account:acc-foreign"
	fmHomeProject    = "project:prj-home"
	fmForeignProject = "project:prj-foreign"

	fmGroupOuter = "group:grp-outer"

	// Субъекты вопросника.
	fmSubjDirect        = "user:u-direct" // назван в привязке проекта
	fmSubjDirectSA      = "service_account:sa-direct"
	fmSubjInGroup       = "user:u-in-group"       // член внешней группы
	fmSubjAccountAdmin  = "user:u-account-admin"  // каскад, уровень 3
	fmSubjAccountOwner  = "user:u-account-owner"  // структурный источник на своём аккаунте
	fmSubjClusterAdmin  = "user:u-cluster-admin"  // каскад, уровни 1-2
	fmSubjObjectOwner   = "user:u-object-owner"   // владелец реестра и репозитория
	fmSubjStranger      = "user:u-stranger"       // ни в одной привязке и ни в одном факте
	fmSubjForeignDirect = "user:u-foreign-direct" // зеркальный: назван в привязке ЧУЖОГО проекта

	// Роли мира и их глаголы. Роль-читатель заведена отдельной привязкой на
	// область АККАУНТА, чтобы область проекта и область аккаунта различались
	// наблюдаемо, а не только в схеме.
	fmRoleEditor = "role.bench.editor"
	fmRoleLister = "role.bench.lister"

	// Метка набора — та же и у зеркала, и у правила-селектора.
	fmLabelKey   = "authzformbench"
	fmLabelValue = "full-model"
)

// fmTenant — принадлежность объекта арендатору. Объект, привязанный к кластеру,
// не принадлежит ни одному, и это не пробел фикстуры, а свойство типа.
type fmTenant string

const (
	tenantHome    fmTenant = "home"
	tenantForeign fmTenant = "foreign"
	tenantCluster fmTenant = "cluster"
	// tenantUnlabelled — не арендатор, а роль объекта в фикстуре: он лежит у
	// СВОЕГО арендатора, но вне набора, который сужает правило-селектор. Значение
	// используется только при построении мира; принадлежность такого объекта
	// вычисляется как у своего.
	tenantUnlabelled fmTenant = "unlabelled"
)

// fmObject — один объект мира.
type fmObject struct {
	Type     string
	ID       string
	Tenant   fmTenant
	Pointers map[string]string // отношение-указатель → "тип:идентификатор"
	Labelled bool
}

func (o fmObject) Ref() string { return o.Type + ":" + o.ID }

// fmFact — строка факта: то, что iam держит у себя колонкой или таблицей
// (владелец аккаунта, администратор кластера, членство, публичное чтение), а
// движок — кортежем.
type fmFact struct {
	Object   string // "тип:идентификатор"
	Relation string
	Subject  string
}

// fmBinding — выдача в НЕразвёрнутом виде.
type fmBinding struct {
	ID        string
	ScopeType string // account | project
	ScopeID   string // полный "тип:идентификатор"
	Role      string
	Subjects  []string
	// SelectorTypes — типы объектов, которые накрывает правило-селектор.
	// Заполняется ВСЕМИ типами с глаголами: сужение здесь оставило бы часть
	// модели без единого вопроса, и «совпало» читалось бы шире, чем спрошено.
	SelectorTypes []string
}

// fmWorld — мир целиком.
type fmWorld struct {
	Model     *Model
	Objects   []fmObject
	Facts     []fmFact
	Bindings  []fmBinding
	RoleVerbs map[string][]string
	Groups    map[string][]string // группа → члены (полные строки субъектов)

	byRef map[string]fmObject
}

// buildWorld собирает мир ИЗ модели: перечень типов, их указатели и их глаголы
// берутся разбором, а не выписываются. Добавят в модель тип — он появится в мире
// и в вопроснике сам.
func buildWorld(m *Model) (*fmWorld, error) {
	if err := m.AssertOnePointerPerParentType(); err != nil {
		return nil, err
	}
	w := &fmWorld{
		Model:     m,
		RoleVerbs: map[string][]string{},
		Groups: map[string][]string{
			// Вложенность групп СНЯТА вместе со свойством: модель сужена до
			// схемы (задача #734), и `group#member` субъектом членства больше
			// не принимается — хранилище отвергает такой кортеж проверкой
			// формы. Проба снимается вместе со своим предметом, а не
			// подгоняется: субъект `u-nested` был заведён под эту ветвь и
			// другой не имеет.
			fmGroupOuter: {fmSubjInGroup},
		},
		byRef: map[string]fmObject{},
	}

	add := func(o fmObject) {
		if o.Pointers == nil {
			o.Pointers = map[string]string{}
		}
		w.Objects = append(w.Objects, o)
		w.byRef[o.Ref()] = o
	}

	// Каркас: кластер, два аккаунта, два проекта.
	add(fmObject{Type: "cluster", ID: "cluster_kacho_root", Tenant: tenantCluster})
	add(fmObject{Type: "account", ID: "acc-home", Tenant: tenantHome, Labelled: true,
		Pointers: map[string]string{"cluster": fmClusterObj}})
	add(fmObject{Type: "account", ID: "acc-foreign", Tenant: tenantForeign, Labelled: true,
		Pointers: map[string]string{"cluster": fmClusterObj}})
	add(fmObject{Type: "project", ID: "prj-home", Tenant: tenantHome, Labelled: true,
		Pointers: map[string]string{"account": fmHomeAccount, "cluster": fmClusterObj}})
	add(fmObject{Type: "project", ID: "prj-foreign", Tenant: tenantForeign, Labelled: true,
		Pointers: map[string]string{"account": fmForeignAccount, "cluster": fmClusterObj}})
	add(fmObject{Type: "group", ID: "grp-outer", Tenant: tenantHome})

	// По объекту каждого арендатора на КАЖДЫЙ тип с глаголами. Порядок обхода —
	// порядок объявления в модели, поэтому тип-контейнер (реестр) заводится
	// раньше своего ребёнка (репозитория), и указатель ребёнка резолвится.
	var verbTypes []string
	for _, t := range m.Types {
		if len(t.Verbs()) == 0 {
			continue
		}
		verbTypes = append(verbTypes, t.Name)
		if t.Name == "account" || t.Name == "project" {
			continue // уже в каркасе
		}
		// Три объекта на тип, и третий — не пустое место: он лежит у СВОЕГО
		// арендатора и не несёт метки набора. Без него правило-селектор проверено
		// ровно на одном объекте всего мира, а конъюнкт, у которого один свидетель,
		// почти тождественно истинен — снятие его меняло бы вердикт в пяти вопросах
		// из тысяч, и «проба способна упасть» держалось бы на волоске.
		for _, tn := range []fmTenant{tenantHome, tenantForeign, tenantUnlabelled} {
			labelled, anchor := true, tn
			if tn == tenantUnlabelled {
				labelled, anchor = false, tenantHome
			}
			o := fmObject{Type: t.Name, ID: fmt.Sprintf("obj-%s-%s", t.Name, tn),
				Tenant: anchor, Labelled: labelled, Pointers: map[string]string{}}
			for ptr := range m.Pointers[t.Name] {
				targets := m.PointerTargets(t.Rel(ptr))
				if len(targets) != 1 {
					return nil, fmt.Errorf("указатель %s.%s ведёт в %d типов — мир не знает, куда его привязать",
						t.Name, ptr, len(targets))
				}
				ref, ok := w.anchorFor(targets[0], anchor)
				if !ok {
					return nil, fmt.Errorf("указатель %s.%s ведёт в тип %q, для которого в мире нет якоря",
						t.Name, ptr, targets[0])
				}
				o.Pointers[ptr] = ref
			}
			if len(o.Pointers) == 0 {
				return nil, fmt.Errorf("тип %q с глаголами не несёт ни одного указателя — "+
					"объект такого типа не привязан ни к чему, и вопрос про область к нему неприменим", t.Name)
			}
			o.Tenant = tenantOf(w, o)
			add(o)
		}
	}
	if len(verbTypes) == 0 {
		return nil, fmt.Errorf("в модели не нашлось ни одного типа с глаголами — вопросник был бы пуст")
	}

	// Объект каждому типу, который объявляет отношения, но глаголов не несёт
	// (сегодня это тип-полномочие прокси). Без него его отношение не было бы
	// спрошено НИ РАЗУ — а «эквивалентность на всей модели» с непрошенным
	// отношением означает «на всей, кроме той, о которой забыли».
	for _, t := range m.Types {
		if len(t.Relations) == 0 || len(byTypeCount(w, t.Name)) > 0 {
			continue
		}
		o := fmObject{Type: t.Name, ID: "obj-" + t.Name + "-home", Tenant: tenantHome,
			Labelled: true, Pointers: map[string]string{}}
		for ptr := range m.Pointers[t.Name] {
			targets := m.PointerTargets(t.Rel(ptr))
			if len(targets) != 1 {
				return nil, fmt.Errorf("указатель %s.%s ведёт в %d типов", t.Name, ptr, len(targets))
			}
			ref, ok := w.anchorFor(targets[0], tenantHome)
			if !ok {
				return nil, fmt.Errorf("указатель %s.%s ведёт в тип %q без якоря", t.Name, ptr, targets[0])
			}
			o.Pointers[ptr] = ref
		}
		add(o)
	}

	// Публичный репозиторий: тот же тип, третий объект, накладной кортеж
	// подстановочного чтения. Заводится ровно потому, что подстановочный субъект
	// объявлен в модели ОДНИМ отношением одного типа: без своего объекта эта
	// ветвь модели не была бы спрошена ни разу.
	if pub, ok := w.publicRepoObject(m); ok {
		add(pub)
		w.Facts = append(w.Facts, fmFact{Object: pub.Ref(), Relation: "v_get", Subject: "user:*"})
	}

	// Строки фактов. Каждая — то, что iam хранит у себя, а движок кортежем.
	w.Facts = append(w.Facts,
		fmFact{Object: fmHomeAccount, Relation: "owner", Subject: fmSubjAccountOwner},
		fmFact{Object: fmHomeAccount, Relation: "admin", Subject: fmSubjAccountAdmin},
		fmFact{Object: fmForeignAccount, Relation: "owner", Subject: "user:u-foreign-owner"},
		fmFact{Object: fmClusterObj, Relation: "system_admin", Subject: fmSubjClusterAdmin},
	)
	for _, o := range w.Objects {
		if o.Type == "account" || o.Type == "project" {
			continue // владелец аккаунта заведён отдельным субъектом выше
		}
		if m.Type(o.Type) != nil && m.Type(o.Type).Rel("owner") != nil && o.Tenant == tenantHome {
			w.Facts = append(w.Facts, fmFact{Object: o.Ref(), Relation: "owner", Subject: fmSubjObjectOwner})
		}
	}
	// Членство в группах — такая же строка факта, как владение: у iam это таблица
	// членов, у движка кортеж. Отдельной таблицы под членство здесь НЕТ намеренно —
	// два места об одном предмете разошлись бы, а замыкание субъекта по группам
	// читает ровно эти же строки.
	for _, g := range sortedKeys(w.Groups) {
		for _, mem := range w.Groups[g] {
			w.Facts = append(w.Facts, fmFact{Object: g, Relation: "member", Subject: mem})
		}
	}
	// Подстановочное чтение справочника: `cluster:<root>#viewer@user:*` — то, что
	// бутстрап кластера пишет намеренно (каждый аутентифицированный арендатор
	// обязан читать глобальный справочник). Без этой строки ветвь `user:*` у
	// `cluster.viewer` не была бы спрошена ни разу, а она — ровно тот случай, где
	// отношение выполнимо подстановкой и не сужает ничего.
	if cl := m.Type("cluster"); cl != nil && cl.Rel("viewer") != nil {
		w.Facts = append(w.Facts, fmFact{Object: fmClusterObj, Relation: "viewer", Subject: "user:*"})
	}

	// Положительная сторона КАЖДОГО отношения, решающего доступ.
	//
	// Отношение, у которого спрошены только отказы, — это половина предмета:
	// «формы совпали» на нём означает «обе одинаково молчат». Поэтому на каждое
	// не-глагольное отношение, принимающее прямого субъекта, кладётся один факт —
	// ВЫВЕДЕННО из модели, а не выписанным перечнем, чтобы новое отношение
	// получало свою положительную сторону само.
	//
	// Не кладётся туда, где факт уже есть (владелец аккаунта, администратор,
	// членство — они заведены выше со СВОИМИ субъектами, и перекрытие смазало бы
	// их роль), и туда, где ЕДИНСТВЕННЫЕ записи прямого списка условны: такой
	// кортеж без условия движок не принимает вовсе, а с условием требует
	// контекста запроса, которого у формы E нет. Это и есть исход (в), и он
	// доказан отдельной пробой, а не обойдён здесь молча.
	seeded := map[string]bool{}
	for _, f := range w.Facts {
		seeded[f.Object+"#"+f.Relation] = true
	}
	for _, t := range m.Types {
		for _, r := range t.Relations {
			if IsVerb(r.Name) || m.IsPointer(t.Name, r.Name) {
				continue
			}
			subj := unconditionedSubject(r)
			if subj == "" {
				continue
			}
			for _, o := range w.Objects {
				// Метка здесь ни при чём: она сужает правило-селектор ВЫДАЧИ, а
				// факт от неё не зависит. Требование метки оставило бы без
				// положительной стороны отношения объекта, который меткой не
				// помечается вовсе, — например кластера.
				if o.Type != t.Name || o.Tenant == tenantForeign {
					continue
				}
				if seeded[o.Ref()+"#"+r.Name] {
					continue
				}
				seeded[o.Ref()+"#"+r.Name] = true
				w.Facts = append(w.Facts, fmFact{Object: o.Ref(), Relation: r.Name, Subject: subj})
				break
			}
		}
	}

	w.RoleVerbs[fmRoleEditor] = []string{"v_get", "v_update"}
	w.RoleVerbs[fmRoleLister] = []string{"v_list"}

	w.Bindings = []fmBinding{
		{ID: "ab-home-project", ScopeType: "project", ScopeID: fmHomeProject, Role: fmRoleEditor,
			Subjects: []string{fmSubjDirect, fmSubjDirectSA}, SelectorTypes: verbTypes},
		{ID: "ab-home-account", ScopeType: "account", ScopeID: fmHomeAccount, Role: fmRoleLister,
			Subjects: []string{fmGroupOuter + "#member"}, SelectorTypes: verbTypes},
		{ID: "ab-foreign-project", ScopeType: "project", ScopeID: fmForeignProject, Role: fmRoleEditor,
			Subjects: []string{fmSubjForeignDirect}, SelectorTypes: verbTypes},
	}
	return w, nil
}

// byTypeCount — объекты названного типа, уже заведённые в мире.
func byTypeCount(w *fmWorld, typeName string) []fmObject {
	var out []fmObject
	for _, o := range w.Objects {
		if o.Type == typeName {
			out = append(out, o)
		}
	}
	return out
}

// unconditionedSubject — субъект вопросника, годный в прямую запись отношения,
// либо пустая строка, если годной записи нет.
//
// Условные записи ПРОПУСКАЮТСЯ намеренно: кортеж под них движок принимает только
// вместе с условием, а ответ на него зависит от контекста запроса. Отношение,
// у которого условны ВСЕ записи, положительной стороны в этом вопроснике не
// получает — и это его свойство, а не пробел фикстуры.
func unconditionedSubject(r *Relation) string {
	for _, term := range r.Terms {
		if term.Kind != TermDirect {
			continue
		}
		for _, d := range term.Direct {
			if d.Condition != "" || d.Wildcard || d.Userset != "" {
				continue
			}
			switch d.Type {
			case "user":
				return fmSubjObjectOwner
			case "service_account":
				return fmSubjDirectSA
			}
		}
	}
	return ""
}

// publicRepoObject — третий объект типа, чей глагол принимает подстановочный
// субъект. Тип НЕ выписан: он находится по модели, поэтому исчезнет из мира
// вместе с подстановкой, если её когда-нибудь снимут.
func (w *fmWorld) publicRepoObject(m *Model) (fmObject, bool) {
	for _, t := range m.Types {
		for _, r := range t.Relations {
			if !IsVerb(r.Name) {
				continue
			}
			for _, term := range r.Terms {
				if term.Kind != TermDirect {
					continue
				}
				for _, d := range term.Direct {
					if !d.Wildcard {
						continue
					}
					src, ok := w.byRef[t.Name+":"+fmt.Sprintf("obj-%s-%s", t.Name, tenantHome)]
					if !ok {
						return fmObject{}, false
					}
					pub := fmObject{Type: t.Name, ID: "obj-" + t.Name + "-public", Tenant: tenantHome,
						Labelled: false, Pointers: map[string]string{}}
					for k, v := range src.Pointers {
						pub.Pointers[k] = v
					}
					return pub, true
				}
			}
		}
	}
	return fmObject{}, false
}

// anchorFor — объект-якорь названного типа для названного арендатора.
func (w *fmWorld) anchorFor(typeName string, tn fmTenant) (string, bool) {
	switch typeName {
	case "cluster":
		return fmClusterObj, true
	case "account":
		if tn == tenantHome {
			return fmHomeAccount, true
		}
		return fmForeignAccount, true
	case "project":
		if tn == tenantHome {
			return fmHomeProject, true
		}
		return fmForeignProject, true
	}
	ref := typeName + ":" + fmt.Sprintf("obj-%s-%s", typeName, tn)
	if _, ok := w.byRef[ref]; ok {
		return ref, true
	}
	return "", false
}

// tenantOf — арендатор объекта, ВЫВЕДЕННЫЙ по цепи родительства: тип, чьи
// указатели ведут только в кластер, не принадлежит ни одному арендатору, и
// вопросы про область к нему обязаны отвечать отказом у обеих сторон.
func tenantOf(w *fmWorld, o fmObject) fmTenant {
	for _, ref := range o.Pointers {
		if ref == fmHomeAccount || ref == fmHomeProject {
			return tenantHome
		}
		if ref == fmForeignAccount || ref == fmForeignProject {
			return tenantForeign
		}
		if p, ok := w.byRef[ref]; ok && p.Type != "cluster" {
			return tenantOf(w, p)
		}
	}
	return tenantCluster
}

// ancestors возвращает объект и всех его предков по цепи указателей.
func (w *fmWorld) ancestors(ref string) map[string]bool {
	out := map[string]bool{}
	var walk func(string, int)
	walk = func(r string, depth int) {
		if depth > MaxPointerDepth+2 || out[r] {
			return
		}
		out[r] = true
		o, ok := w.byRef[r]
		if !ok {
			return
		}
		for _, p := range o.Pointers {
			walk(p, depth+1)
		}
	}
	walk(ref, 0)
	return out
}

// ── сторона движка: развёрнутый мир ───────────────────────────────────────────

// engineTuples раскладывает мир в кортежи так, как его сегодня держит движок.
//
// Материализация состава — отдельная функция (`materialize`), и это намеренно:
// она воспроизводит реконсайлер, а не спрашивает форму E. Если бы разворот
// считался тем же кодом, что и содержание области у формы E, согласие сторон
// было бы тождеством, а не результатом.
func (w *fmWorld) engineTuples() []Tuple {
	var out []Tuple
	for _, o := range w.Objects {
		ptrs := make([]string, 0, len(o.Pointers))
		for p := range o.Pointers {
			ptrs = append(ptrs, p)
		}
		sort.Strings(ptrs)
		for _, p := range ptrs {
			out = append(out, Tuple{User: o.Pointers[p], Relation: p, Object: o.Ref()})
		}
	}
	for _, f := range w.Facts {
		out = append(out, Tuple{User: f.Subject, Relation: f.Relation, Object: f.Object})
	}
	return append(out, w.materialize()...)
}

// materialize — воспроизведение реконсайлера: привязка × объекты под её
// селектором × глаголы её роли × её субъекты.
//
// Глагол, которого тип НЕ объявляет, пропускается — иначе запись была бы
// отвергнута движком, а у продукта пропуск делает набор глаголов роли атрибутом
// ТИПА, а не роли.
func (w *fmWorld) materialize() []Tuple {
	var out []Tuple
	for _, b := range w.Bindings {
		types := map[string]bool{}
		for _, t := range b.SelectorTypes {
			types[t] = true
		}
		for _, o := range w.Objects {
			if !types[o.Type] || !o.Labelled {
				continue
			}
			if !w.ancestors(o.Ref())[b.ScopeID] {
				continue
			}
			t := w.Model.Type(o.Type)
			for _, v := range w.RoleVerbs[b.Role] {
				if t == nil || t.Rel(v) == nil {
					continue
				}
				for _, s := range b.Subjects {
					out = append(out, Tuple{User: s, Relation: v, Object: o.Ref()})
				}
			}
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── вопросник ─────────────────────────────────────────────────────────────────

// FullQuestion — один вопрос полного вопросника.
type FullQuestion struct {
	Type     string
	Relation string
	Subject  string
	Object   string
	IsVerb   bool
}

// Name — имя вопроса в форме, годной для строки падения: тип, глагол, субъект,
// объект. Расхождение обязано называться поимённо, а не числом.
func (q FullQuestion) Name() string {
	return fmt.Sprintf("%s.%s | субъект %s | объект %s", q.Type, q.Relation, q.Subject, q.Object)
}

// fullSubjects — субъекты вопросника. Перечень закрыт и назван: каждый заведён
// под конкретную ветвь модели, и вырожденный (никем не наделённый) обязателен —
// без него «разрешено» и «разрешено всем» неразличимы.
func fullSubjects() []string {
	return []string{
		fmSubjDirect, fmSubjDirectSA, fmSubjInGroup,
		fmSubjAccountAdmin, fmSubjAccountOwner, fmSubjClusterAdmin,
		fmSubjObjectOwner, fmSubjStranger, fmSubjForeignDirect,
	}
}

// Questions строит вопросник ИЗ модели: каждое отношение каждого типа, кроме
// структурных указателей, — на каждом объекте этого типа и на каждом субъекте.
//
// Указатели исключены и это названо числом в переписи: они не решают доступ, а
// вопрос «является ли пользователь проектом» отвечал бы отказом у обеих сторон
// тождественно и разбавлял бы вопросник вопросами, которые не могут разойтись.
func (w *fmWorld) Questions() []FullQuestion {
	var qs []FullQuestion
	byType := map[string][]fmObject{}
	for _, o := range w.Objects {
		byType[o.Type] = append(byType[o.Type], o)
	}
	subs := fullSubjects()
	for _, t := range w.Model.Types {
		objs := byType[t.Name]
		if len(objs) == 0 {
			continue
		}
		for _, r := range t.Relations {
			if w.Model.IsPointer(t.Name, r.Name) {
				continue
			}
			for _, o := range objs {
				for _, s := range subs {
					qs = append(qs, FullQuestion{Type: t.Name, Relation: r.Name,
						Subject: s, Object: o.Ref(), IsVerb: IsVerb(r.Name)})
				}
			}
		}
	}
	return qs
}

// ── сторона формы E: неразвёрнутый мир ────────────────────────────────────────

// relRows — строки, которыми мир ложится в схему формы E.
type relRows struct {
	Mirror    [][3]string // тип · идентификатор · метки (JSON)
	Edges     [][5]string // тип · идентификатор · указатель · тип предка · идентификатор предка
	Facts     [][4]string // тип · идентификатор · отношение · субъект
	Bindings  [][4]string // идентификатор · тип области · идентификатор области · роль
	BindSubj  [][2]string
	Selectors [][3]string // привязка · тип объекта · метки (JSON)
	RoleVerbs [][2]string
}

// relRows переводит мир в строки формы E. Разворота состава здесь нет by
// construction — в этом и состоит форма.
func (w *fmWorld) relRows() (relRows, error) {
	var r relRows
	labels := fmt.Sprintf(`{%q:%q}`, fmLabelKey, fmLabelValue)
	for _, o := range w.Objects {
		l := "{}"
		if o.Labelled {
			l = labels
		}
		r.Mirror = append(r.Mirror, [3]string{o.Type, o.ID, l})
		for _, p := range sortedKeys(o.Pointers) {
			pt, pid, err := splitObject(o.Pointers[p])
			if err != nil {
				return relRows{}, err
			}
			r.Edges = append(r.Edges, [5]string{o.Type, o.ID, p, pt, pid})
		}
	}
	for _, f := range w.Facts {
		ot, oid, err := splitObject(f.Object)
		if err != nil {
			return relRows{}, err
		}
		r.Facts = append(r.Facts, [4]string{ot, oid, f.Relation, f.Subject})
	}
	for _, b := range w.Bindings {
		_, scopeID, err := splitObject(b.ScopeID)
		if err != nil {
			return relRows{}, err
		}
		r.Bindings = append(r.Bindings, [4]string{b.ID, b.ScopeType, scopeID, b.Role})
		for _, s := range b.Subjects {
			r.BindSubj = append(r.BindSubj, [2]string{b.ID, s})
		}
		for _, t := range b.SelectorTypes {
			r.Selectors = append(r.Selectors, [3]string{b.ID, t, labels})
		}
	}
	for _, role := range sortedKeys(w.RoleVerbs) {
		for _, v := range w.RoleVerbs[role] {
			r.RoleVerbs = append(r.RoleVerbs, [2]string{role, v})
		}
	}
	return r, nil
}

// subjectSeeds — что считается «этим субъектом» на входе вердикта: он сам и, для
// пользователя, подстановочная форма своего типа. Членство в группах добирается
// рекурсивно уже в запросе.
//
// Подстановка типизирована: `user:*` не накрывает служебную учётку. Ошибка здесь
// была бы не косметической — она отдала бы публичное чтение машинному
// принципалу, которому модель его не давала.
func subjectSeeds(subject string) []string {
	st, _, err := splitObject(subject)
	if err != nil {
		return []string{subject}
	}
	return []string{subject, st + ":*"}
}

// describeWorld — перепись мира для отчёта.
func (w *fmWorld) describeWorld() string {
	var b strings.Builder
	tuples := w.engineTuples()
	mat := w.materialize()
	fmt.Fprintf(&b, "объектов %d · рёбер родительства %d · строк фактов %d · привязок %d · групп %d\n",
		len(w.Objects), countEdges(w), len(w.Facts), len(w.Bindings), len(w.Groups))
	fmt.Fprintf(&b, "кортежей у движка %d, из них материализованный состав %d\n", len(tuples), len(mat))
	return b.String()
}

func countEdges(w *fmWorld) int {
	n := 0
	for _, o := range w.Objects {
		n += len(o.Pointers)
	}
	return n
}
