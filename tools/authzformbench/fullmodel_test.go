// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Разбор боевой модели и его перепись — без контейнеров.
//
// Проба быстрая намеренно: она держит предпосылку, на которой стоит всё
// остальное, и обязана падать раньше, чем поднимется хоть один контейнер. Если
// разбор перестал понимать модель, доказательство эквивалентности не «покраснеет
// на паре вопросов» — оно станет утверждением о другом предмете.

func parseCanonical(t *testing.T) *Model {
	t.Helper()
	path, canon, err := ResolveCanonicalModel()
	require.NoError(t, err)
	require.NotEmpty(t, canon, "канонический файл модели пуст: %s", path)
	m, err := ParseModel(string(canon))
	require.NoError(t, err, "разбор канонической модели %s", path)
	return m
}

// TestCanonicalModelIsParsedWhole — перепись разобранного против переписи файла.
//
// Обе стороны считаются РАЗНЫМИ способами: слева дерево разбора, справа строки
// файла. Совпадение означает, что разбор не потерял объявлений молча; расхождение
// назовёт, сколько именно потеряно. Без правой стороны «разобралось» было бы
// неотличимо от «разобралась половина».
func TestCanonicalModelIsParsedWhole(t *testing.T) {
	_, canon, err := ResolveCanonicalModel()
	require.NoError(t, err)
	m := parseCanonical(t)
	c := m.Census()

	var fileTypes, fileDefines, fileVerbs int
	for _, ln := range strings.Split(string(canon), "\n") {
		tr := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(tr, "#"):
		case strings.HasPrefix(ln, "type "):
			fileTypes++
		case strings.HasPrefix(tr, "define "):
			fileDefines++
			if strings.HasPrefix(strings.TrimPrefix(tr, "define "), VerbPrefix) {
				fileVerbs++
			}
		}
	}
	require.Equal(t, fileTypes, c.Types, "разобрано типов %d, в файле объявлено %d", c.Types, fileTypes)
	require.Equal(t, fileDefines, c.Declarations,
		"разобрано объявлений %d, в файле %d — разбор потерял часть модели молча", c.Declarations, fileDefines)
	require.Equal(t, fileVerbs, c.VerbDeclaration)
	require.Positive(t, c.VerbTypes)
	require.Positive(t, c.Pointers)

	t.Logf("перепись модели: типов %d · объявлений %d · типов с глаголами %d · глаголов %d · "+
		"указателей %d · условий %d", c.Types, c.Declarations, c.VerbTypes, c.VerbDeclaration,
		c.Pointers, c.Conditions)
}

// TestEveryTermOfEveryRelationIsClassified — у классификатора НЕТ корзины
// «прочее».
//
// Компилятор обязан отнести каждый терм каждого отношения к названному виду.
// Неотнесённое печатается поимённо и роняет пробу: молчаливое проглатывание дало
// бы форму E, «совпавшую» с движком ровно потому, что про непонятое её не
// спросили. Условия на кортеже — отдельная категория, они НЕ ошибка разбора и
// проверяются своей пробой ниже.
func TestEveryTermOfEveryRelationIsClassified(t *testing.T) {
	m := parseCanonical(t)
	require.NoError(t, m.assertOnePointerPerParentType())

	var unclassified, conditioned []string
	total, expressible := 0, 0
	for _, ty := range m.Types {
		for _, r := range ty.Relations {
			if m.IsPointer(ty.Name, r.Name) {
				continue
			}
			total++
			p, err := m.Compile(ty.Name, r.Name)
			require.NoErrorf(t, err, "компиляция %s.%s", ty.Name, r.Name)
			require.NotEmptyf(t, p.Atoms, "%s.%s: план пуст — ни одного источника разрешения",
				ty.Name, r.Name)
			if p.Expressible() {
				expressible++
			} else {
				unclassified = append(unclassified, p.Unclassified...)
			}
			conditioned = append(conditioned, p.Conditioned...)
		}
	}
	sort.Strings(conditioned)
	t.Logf("отношений, решающих доступ: %d · выразимо целиком: %d · с условием на кортеже: %d",
		total, expressible, len(conditioned))
	for _, c := range conditioned {
		t.Logf("  условие: %s", c)
	}
	require.Emptyf(t, unclassified, "термы, не отнесённые ни к одному виду:\n%s",
		strings.Join(unclassified, "\n"))
	require.Positive(t, total, "ни одного отношения не рассмотрено — «ноль находок» здесь означало бы ноль прочитанного")
	require.NotEmpty(t, conditioned,
		"в модели не нашлось ни одного условия на кортеже, хотя она объявляет условия — "+
			"либо разбор их не видит, либо модель изменилась, и запись про невыразимость условий "+
			"истекла вместе со своим предметом")
}

// TestParserRefusesWhatItCannotUnderstand — отрицательный контроль разбора.
//
// Разбор, молча пропускающий незнакомую конструкцию, произвёл бы план, который
// «совпал» с движком потому, что о пропущенном не спросили. Здесь проверяется,
// что пересечение, вычитание и нераспознанная строка ОТВЕРГАЮТСЯ, а рядом —
// положительный контроль: законная модель той же формы разбирается.
func TestParserRefusesWhatItCannotUnderstand(t *testing.T) {
	ok := "model\n  schema 1.1\n\ntype user\n\ntype doc\n  relations\n    define viewer: [user]\n"
	m, err := ParseModel(ok)
	require.NoError(t, err, "положительный контроль: законная модель обязана разбираться")
	require.Len(t, m.Types, 2)

	for name, bad := range map[string]string{
		"пересечение":           "type user\n\ntype doc\n  relations\n    define viewer: [user] and editor\n    define editor: [user]\n",
		"вычитание":             "type user\n\ntype doc\n  relations\n    define viewer: [user] but not editor\n    define editor: [user]\n",
		"мусор":                 "type user\n\ntype doc\n  relations\n    define viewer: [user]\n    непонятная строка\n",
		"неизвестное отношение": "type user\n\ntype doc\n  relations\n    define viewer: nosuch\n",
	} {
		_, err := ParseModel(bad)
		require.Errorf(t, err, "разбор принял %s — он обязан отвергать непонятое, а не делать вид, что понял", name)
	}
}

// TestPlanNamesItsSourcesForEachShapeOfAnchoring — план обязан РАЗЛИЧАТЬ типы,
// привязанные по-разному.
//
// Это положительная половина контроля к отрицанию «формы совпали»: если бы
// компилятор выдавал один и тот же план всем типам, совпадение вердиктов
// означало бы, что и вопросник, и форма E одинаково слепы. Проверяются три
// заведомо разные привязки, названные поимённо.
func TestPlanNamesItsSourcesForEachShapeOfAnchoring(t *testing.T) {
	m := parseCanonical(t)

	has := func(p Plan, kind AtomKind, parent, rel string) bool {
		for _, a := range p.Atoms {
			if a.Kind == kind && a.ParentType == parent && a.Relation == rel {
				return true
			}
		}
		return false
	}

	leaf, err := m.Compile("vpc_network", "v_get")
	require.NoError(t, err)
	require.True(t, has(leaf, AtomBinding, "", "v_get"), "лист обязан разрешаться выдачей")
	require.True(t, has(leaf, AtomFact, "account", "admin"), "лист обязан наследовать каскад администратора аккаунта")
	require.True(t, has(leaf, AtomFact, "cluster", "system_admin"), "лист обязан наследовать каскад кластера")

	pool, err := m.Compile("vpc_address_pool", "v_get")
	require.NoError(t, err)
	require.True(t, has(pool, AtomFact, "cluster", "system_admin"))
	require.Falsef(t, has(pool, AtomFact, "account", "admin"),
		"тип, привязанный к кластеру, получил источник уровня аккаунта — "+
			"привязка типов перестала различаться, и совпадение вердиктов ничего бы не значило")

	acc, err := m.Compile("account", "v_get")
	require.NoError(t, err)
	require.True(t, has(acc, AtomFact, "", "owner"), "владелец — структурный источник на своём аккаунте")
	require.Falsef(t, has(acc, AtomFact, "", "admin"),
		"администратор аккаунта получил глаголы на САМ аккаунт — модель этого намеренно не даёт")

	tgt, err := m.Compile("nlb_target_group", "v_addtargets")
	require.NoError(t, err)
	require.Truef(t, has(tgt, AtomBinding, "", "v_update"),
		"надмножество `v_addtargets ⊇ v_update` не доехало в план: держатель права менять группу "+
			"перестал бы управлять её составом")

	repo, err := m.Compile("registry_repository", "v_get")
	require.NoError(t, err)
	require.True(t, has(repo, AtomFact, "", "owner"))
	require.Truef(t, has(repo, AtomFact, "registry_registry", "super_admin") ||
		has(repo, AtomFact, "account", "admin"),
		"репозиторий не унаследовал ничего через свой реестр — цепь родительства длиной больше одного "+
			"звена не пройдена")
}

// TestWorldIsBuiltFromTheModelAndIsNotDegenerate — предпосылка вопросника.
//
// Мир строится ИЗ модели, поэтому его полнота — свойство разбора, а не памяти
// автора. Проверяется то, отсутствие чего делает вопросник вырожденным: два
// арендатора, объект на каждый тип с глаголами, вложенная группа, объект вне
// всякой области выдачи и материализованный состав, который не пуст.
func TestWorldIsBuiltFromTheModelAndIsNotDegenerate(t *testing.T) {
	m := parseCanonical(t)
	w, err := buildWorld(m)
	require.NoError(t, err)

	byType := map[string][]fmObject{}
	tenants := map[fmTenant]int{}
	for _, o := range w.Objects {
		byType[o.Type] = append(byType[o.Type], o)
		tenants[o.Tenant]++
	}
	for _, ty := range m.Types {
		if len(ty.Verbs()) == 0 {
			continue
		}
		require.NotEmptyf(t, byType[ty.Name], "у типа %q с глаголами нет ни одного объекта — "+
			"его глаголы не были бы спрошены ни разу", ty.Name)
	}
	require.Positive(t, tenants[tenantHome])
	require.Positivef(t, tenants[tenantForeign],
		"в мире один арендатор — конъюнкт области тождественно истинен, и доказательство вырождено")
	require.Positivef(t, tenants[tenantCluster],
		"в мире нет объекта вне арендаторов — привязка к кластеру не была бы отличима от привязки к проекту")

	mat := w.materialize()
	require.NotEmptyf(t, mat, "реконсайлер не развернул ни одного кортежа — положительная половина "+
		"вопросника была бы пуста, а отказ на всё совпал бы у обеих форм")

	// Развёрнутый состав обязан НЕ накрывать чужого арендатора для своего субъекта
	// и накрывать своего — иначе «совпало» держится на том, что не выдано ничего.
	var home, foreign int
	for _, tup := range mat {
		if tup.User != fmSubjDirect {
			continue
		}
		if strings.Contains(tup.Object, "foreign") {
			foreign++
		} else {
			home++
		}
	}
	require.Positive(t, home, "субъект своей привязки не получил ни одного объекта")
	require.Zerof(t, foreign, "субъект своей привязки получил объект ЧУЖОГО арендатора — "+
		"развёртка области неверна, и вопросник сравнивал бы две ошибки")

	t.Logf("мир: %s", strings.TrimSpace(w.describeWorld()))
	t.Logf("вопросов в полном вопроснике: %d", len(w.Questions()))
}

// TestQuestionSetCoversEveryVerbOfEveryType — вопросник СТРОИТСЯ из модели, и
// это проверяется перечислением, а не доверием.
//
// Выписанный вопросник покрыл бы то, что автор помнит. Здесь требуется, чтобы
// каждый глагол каждого типа был спрошен, и чтобы у типа, ПРИНАДЛЕЖАЩЕГО
// арендатору, был вопрос и про свой объект, и про чужой: без второго отказ
// неотличим от «спросили не про то».
//
// Требование «оба арендатора» намеренно не предъявляется типу, привязанному к
// КЛАСТЕРУ: у такого типа чужого арендатора нет by construction — его объект не
// лежит ни в одном аккаунте, — и требование к нему было бы требованием
// сконструировать состояние, которого модель не допускает. Ему предъявляется
// другое, проверяемое: не меньше двух различных объектов, чтобы вопрос не
// держался на единственной строке.
func TestQuestionSetCoversEveryVerbOfEveryType(t *testing.T) {
	m := parseCanonical(t)
	w, err := buildWorld(m)
	require.NoError(t, err)

	byRef := map[string]fmObject{}
	tenantsOfType := map[string]map[fmTenant]bool{}
	for _, o := range w.Objects {
		byRef[o.Ref()] = o
		if tenantsOfType[o.Type] == nil {
			tenantsOfType[o.Type] = map[fmTenant]bool{}
		}
		tenantsOfType[o.Type][o.Tenant] = true
	}

	seenTenants := map[string]map[fmTenant]bool{}
	seenObjects := map[string]map[string]bool{}
	for _, q := range w.Questions() {
		if !q.IsVerb {
			continue
		}
		k := q.Type + "." + q.Relation
		if seenTenants[k] == nil {
			seenTenants[k] = map[fmTenant]bool{}
			seenObjects[k] = map[string]bool{}
		}
		seenTenants[k][byRef[q.Object].Tenant] = true
		seenObjects[k][q.Object] = true
	}

	var missing []string
	verbs, clusterAnchored := 0, 0
	for _, ty := range m.Types {
		tenantScoped := tenantsOfType[ty.Name][tenantHome] || tenantsOfType[ty.Name][tenantForeign]
		if len(ty.Verbs()) > 0 && !tenantScoped {
			clusterAnchored++
		}
		for _, v := range ty.Verbs() {
			verbs++
			k := ty.Name + "." + v
			switch {
			case seenTenants[k] == nil:
				missing = append(missing, k+" — не спрошен ни разу")
			case len(seenObjects[k]) < 2:
				missing = append(missing, k+" — спрошен про единственный объект")
			case tenantScoped && !(seenTenants[k][tenantHome] && seenTenants[k][tenantForeign]):
				missing = append(missing, k+" — тип принадлежит арендатору, но спрошен не у обоих")
			}
		}
	}
	require.Emptyf(t, missing, "вопросник не покрывает модель:\n%s", strings.Join(missing, "\n"))
	require.Positive(t, verbs)
	t.Logf("покрыто глаголов: %d у %d типов; из них привязанных к кластеру (чужого арендатора "+
		"нет by construction): %d", verbs, len(seenTenants), clusterAnchored)
}

// ── доказательство на живом движке ────────────────────────────────────────────

// fullVerdicts — то, что обе стороны отвечают на один вопросник.
type fullVerdicts struct {
	Engine       []bool
	Rel          []bool
	RelAnswered  []bool // ложь означает «форма E не выражает это отношение»
	Questions    []FullQuestion
	Unexpressed  map[string]string
	EngineAllows int
	EngineDenies int
}

// coverage — по каждому отношению: спрашивали ли его так, что движок отвечал
// «разрешено», и так, что отвечал «запрещено».
//
// Величина названа отдельно, потому что «вердикты совпали» на отношении, где
// движок отказал ВСЕМ, — утверждение о половине предмета: любая форма,
// отвечающая «нет», с ним совпадёт. Отношения без единого разрешения
// перечисляются поимённо, а не сворачиваются в число.
func (v fullVerdicts) coverage() (both, denyOnly, allowOnly []string) {
	allow := map[string]bool{}
	deny := map[string]bool{}
	for i, q := range v.Questions {
		k := q.Type + "." + q.Relation
		if v.Engine[i] {
			allow[k] = true
		} else {
			deny[k] = true
		}
	}
	keys := map[string]bool{}
	for k := range allow {
		keys[k] = true
	}
	for k := range deny {
		keys[k] = true
	}
	var all []string
	for k := range keys {
		all = append(all, k)
	}
	sort.Strings(all)
	for _, k := range all {
		switch {
		case allow[k] && deny[k]:
			both = append(both, k)
		case deny[k]:
			denyOnly = append(denyOnly, k)
		default:
			allowOnly = append(allowOnly, k)
		}
	}
	return both, denyOnly, allowOnly
}

// runFullQuestionnaire задаёт вопросник обеим сторонам.
func runFullQuestionnaire(t *testing.T, st *Store, rel *fullRelStore, w *fmWorld) fullVerdicts {
	t.Helper()
	ctx := t.Context()
	qs := w.Questions()
	out := fullVerdicts{Questions: qs, Unexpressed: map[string]string{}}
	out.Engine = make([]bool, len(qs))
	out.Rel = make([]bool, len(qs))
	out.RelAnswered = make([]bool, len(qs))

	// Вопросы группируются по (субъект, отношение, тип): движок отвечает пакетом,
	// форма E — одним запросом на страницу. Так спрашивается ровно то, чем обе
	// стороны отвечают в продукте, а не искусственно пообъектный обход.
	type key struct{ subj, rel, typ string }
	groups := map[key][]int{}
	var order []key
	for i, q := range qs {
		k := key{q.Subject, q.Relation, q.Type}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}

	for _, k := range order {
		idx := groups[k]
		objs := make([]string, 0, len(idx))
		ids := make([]string, 0, len(idx))
		for _, i := range idx {
			objs = append(objs, qs[i].Object)
			_, id, err := splitObject(qs[i].Object)
			require.NoError(t, err)
			ids = append(ids, id)
		}
		eng, err := st.BatchCheck(ctx, k.subj, k.rel, objs)
		require.NoErrorf(t, err, "движок на %s.%s для %s", k.typ, k.rel, k.subj)
		for n, i := range idx {
			out.Engine[i] = eng[n]
			if eng[n] {
				out.EngineAllows++
			} else {
				out.EngineDenies++
			}
		}

		res, rerr := rel.Allowed(ctx, k.subj, k.rel, k.typ, ids)
		if rerr != nil {
			require.ErrorIsf(t, rerr, ErrPlanNotExpressible,
				"форма E отказала не по невыразимости на %s.%s: %v", k.typ, k.rel, rerr)
			out.Unexpressed[k.typ+"."+k.rel] = rerr.Error()
			continue
		}
		for n, i := range idx {
			out.Rel[i] = res[ids[n]]
			out.RelAnswered[i] = true
		}
	}
	return out
}

// diff собирает расхождения ПОИМЁННО: тип, глагол, субъект, объект и обе стороны.
func (v fullVerdicts) diff() []string {
	var out []string
	for i, q := range v.Questions {
		if !v.RelAnswered[i] || v.Engine[i] == v.Rel[i] {
			continue
		}
		out = append(out, fmt.Sprintf("%s: движок %v, форма E %v", q.Name(), v.Engine[i], v.Rel[i]))
	}
	return out
}

// bootFullModel поднимает обе стороны на одном мире.
func bootFullModel(t *testing.T, name string) (*Store, *fullRelStore, *fmWorld) {
	t.Helper()
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	m, err := ParseModel(canon)
	require.NoError(t, err)
	w, err := buildWorld(m)
	require.NoError(t, err)

	modelJSON, err := TransformDSL(canon)
	require.NoError(t, err, "трансформ канонической модели в JSON")
	st, err := stack.NewStore(ctx, name, modelJSON)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Delete(ctx) })

	tuples := w.engineTuples()
	_, err = st.WriteTuples(ctx, tuples)
	require.NoError(t, err, "засев движка полным миром")

	rel, err := newFullRelStore(ctx, stack.RelDSN, "fm_"+strings.ReplaceAll(name, "-", "_"), m)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rel.Close(ctx) })
	rows, err := w.relRows()
	require.NoError(t, err)
	require.NoError(t, rel.Seed(ctx, rows), "засев формы E полным миром")
	return st, rel, w
}

// TestFullModelVerdictsAgree — предмет задания: вердикты обеих форм на ПОЛНОЙ
// модели, на каждом глаголе каждого типа, обеими сторонами ответа.
//
// Проба утверждает три вещи, и ни одна не выводится из двух других:
//   - расхождений нет (и каждое, если есть, названо поимённо);
//   - спрошено не ноль, и обе стороны ответа непусты — форма, отвечающая «да» на
//     всё, совпала бы с движком ровно на половине вопросов, поэтому доля
//     разрешений печатается и проверяется на невырожденность;
//   - невыразимое названо отдельной категорией и НЕ зачтено в совпадение.
func TestFullModelVerdictsAgree(t *testing.T) {
	if testing.Short() {
		t.Skip("доказательство на живом OpenFGA; -short")
	}
	st, rel, w := bootFullModel(t, "fullmodel-agree")
	v := runFullQuestionnaire(t, st, rel, w)

	asked := 0
	for _, a := range v.RelAnswered {
		if a {
			asked++
		}
	}
	t.Logf("вопросов задано: %d · форма E ответила: %d · невыразимых отношений: %d",
		len(v.Questions), asked, len(v.Unexpressed))
	t.Logf("движок ответил «разрешено» %d раз, «запрещено» %d раз",
		v.EngineAllows, v.EngineDenies)
	for _, k := range sortedKeys(v.Unexpressed) {
		t.Logf("  невыразимо: %s — %s", k, v.Unexpressed[k])
	}

	require.Positive(t, asked, "форма E не ответила ни на один вопрос — «ноль расхождений» здесь "+
		"означало бы ноль спрошенного")
	require.Positivef(t, v.EngineAllows, "движок не разрешил НИ ОДНОГО вопроса — вопросник спрашивает "+
		"только про отказы, и любая форма, отвечающая «нет» на всё, с ним совпадёт")
	require.Positivef(t, v.EngineDenies, "движок разрешил ВСЁ — отрицательная половина вопросника пуста")

	// Покрытие по отношениям: у КАЖДОГО ГЛАГОЛА обязаны быть спрошены обе стороны
	// ответа. Совпадение на отношении, где движок отказал всем, — утверждение о
	// половине предмета, и его нельзя зачитывать в «эквивалентность доказана».
	both, denyOnly, allowOnly := v.coverage()
	t.Logf("отношений спрошено %d: обе стороны ответа %d · только отказ %d · только разрешение %d",
		len(both)+len(denyOnly)+len(allowOnly), len(both), len(denyOnly), len(allowOnly))
	var verbOneSided []string
	for _, k := range append(append([]string{}, denyOnly...), allowOnly...) {
		if IsVerb(k[strings.LastIndexByte(k, '.')+1:]) {
			verbOneSided = append(verbOneSided, k)
		}
	}
	require.Emptyf(t, verbOneSided, "у %d глаголов спрошена только одна сторона ответа — "+
		"на них «совпало» означает «обе формы одинаково молчат»:\n%s",
		len(verbOneSided), strings.Join(verbOneSided, "\n"))
	// Одностороннее отношение допустимо ТОЛЬКО со структурной причиной: все записи
	// его прямого списка условны, а условный кортеж без контекста запроса движок не
	// принимает вовсе. Всякое другое одностороннее — пробел фикстуры, и он роняет
	// пробу. Послабление самоистекает: снимут условие с записи — отношение станет
	// без причины односторонним, и проба назовёт его.
	var unexplained []string
	for _, k := range denyOnly {
		i := strings.LastIndexByte(k, '.')
		ty := w.Model.Type(k[:i])
		r := ty.Rel(k[i+1:])
		if r == nil || unconditionedSubject(r) != "" {
			unexplained = append(unexplained, k)
			continue
		}
		t.Logf("  только отказ, причина структурна (все записи условны): %s", k)
	}
	require.Emptyf(t, unexplained, "у %d отношений спрошена только отрицательная сторона, и причина "+
		"этому не структурна — на них «совпало» означает «обе формы одинаково молчат»:\n%s",
		len(unexplained), strings.Join(unexplained, "\n"))

	d := v.diff()
	require.Emptyf(t, d, "вердикты разошлись на %d вопросах:\n%s", len(d), strings.Join(d, "\n"))

	writeFullModelReport(t, v, w, both, denyOnly, allowOnly)
}

// writeFullModelReport кладёт отчёт файлом, когда путь назван переменной среды.
//
// По умолчанию проба в дерево не пишет: тест, молча правящий репозиторий, делает
// прогон невоспроизводимым и мешает соседу. Путь называется явно тем, кто снимает
// отчёт.
func writeFullModelReport(t *testing.T, v fullVerdicts, w *fmWorld,
	both, denyOnly, allowOnly []string) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("AUTHZFORMBENCH_FULLMODEL_REPORT"))
	if path == "" {
		return
	}
	c := w.Model.Census()
	var b strings.Builder
	fmt.Fprintf(&b, "модель: типов %d · объявлений %d · типов с глаголами %d · глаголов %d · "+
		"указателей %d · условий %d\n", c.Types, c.Declarations, c.VerbTypes, c.VerbDeclaration,
		c.Pointers, c.Conditions)
	fmt.Fprintf(&b, "мир: %s", w.describeWorld())
	fmt.Fprintf(&b, "вопросов задано %d · форма E ответила %d · невыразимых отношений %d\n",
		len(v.Questions), len(v.Questions)-countUnanswered(v), len(v.Unexpressed))
	fmt.Fprintf(&b, "движок: разрешено %d · запрещено %d\n", v.EngineAllows, v.EngineDenies)
	fmt.Fprintf(&b, "расхождений: %d\n", len(v.diff()))
	fmt.Fprintf(&b, "отношений с обеими сторонами ответа %d · только отказ %d · только разрешение %d\n",
		len(both), len(denyOnly), len(allowOnly))
	for _, k := range denyOnly {
		fmt.Fprintf(&b, "  только отказ: %s\n", k)
	}
	for _, k := range allowOnly {
		fmt.Fprintf(&b, "  только разрешение: %s\n", k)
	}
	for _, d := range v.diff() {
		fmt.Fprintf(&b, "  расхождение: %s\n", d)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	t.Logf("отчёт записан: %s", path)
}

func countUnanswered(v fullVerdicts) int {
	n := 0
	for _, a := range v.RelAnswered {
		if !a {
			n++
		}
	}
	return n
}

// TestFullModelProofRedsWhenASourceIsTaken — доказательство инъекцией, в обе
// стороны.
//
// Первая половина: у формы E снимается по одному источнику разрешения — и проба
// обязана покраснеть, назвав координату. Вторая половина, без которой первая
// ничего не значит: рядом ставится ЗАКОННЫЙ близнец — правка, ничего не меняющая
// по существу (порядок атомов и заведомо избыточный дубликат), — и проба обязана
// смолчать.
func TestFullModelProofRedsWhenASourceIsTaken(t *testing.T) {
	if testing.Short() {
		t.Skip("доказательство на живом OpenFGA; -short")
	}
	st, rel, w := bootFullModel(t, "fullmodel-injection")

	base := runFullQuestionnaire(t, st, rel, w)
	require.Empty(t, base.diff(), "инъекция проверяется на состоянии, где расхождений нет")

	// ── законный близнец: тот же смысл, другая запись ──────────────────────────
	rel.mutate = func(p Plan) Plan {
		out := Plan{Type: p.Type, Relation: p.Relation, Conditioned: p.Conditioned}
		for i := len(p.Atoms) - 1; i >= 0; i-- {
			out.Atoms = append(out.Atoms, p.Atoms[i])
		}
		out.Atoms = append(out.Atoms, p.Atoms...) // дубликат: дизъюнкция идемпотентна
		return out
	}
	twin := runFullQuestionnaire(t, st, rel, w)
	require.Emptyf(t, twin.diff(),
		"проба покраснела на ЗАКОННОЙ переписи плана — она ловит форму записи, а не существо, "+
			"и первое же безобидное изменение её отключит:\n%s", strings.Join(twin.diff(), "\n"))

	// ── снятие ОДНОГО конъюнкта внутри источника ───────────────────────────────
	//
	// Снятие атома целиком доказывает, что источник спрашивают. Оно НЕ доказывает,
	// что его спрашивают правильно: дефект, на котором был отвергнут первый заход
	// XC-10, сидел внутри атома выдачи — условие области было тождественно
	// истинным, и все пробы оставались зелёными. Поэтому каждый конъюнкт снимается
	// поимённо и по одному.
	// `weakenParentDepth` в этот перечень НЕ входит, и это не забывчивость:
	// на КАНОНИЧЕСКОЙ модели у него нет предмета — ни один тип не указывает на
	// свой собственный тип, поэтому фильтр по типу предка и так не пускает сам
	// объект. Требовать от него покраснения здесь значило бы требовать
	// сконструировать состояние, которого модель не допускает; вместо этого он
	// доказан там, где предмет есть, — на синтетической модели с самовложением
	// (`TestParentFactConjunctIsLoadBearingWhereItHasASubject`). Появится в
	// каноне тип, указывающий на себя, — конъюнкт станет действующим здесь сам.
	for _, wk := range []weakening{weakenScope, weakenSelector, weakenGroupClosure} {
		t.Run("снят конъюнкт: "+string(wk), func(t *testing.T) {
			rel.mutate = nil
			rel.weaken = wk
			defer func() { rel.weaken = weakenNothing }()
			got := runFullQuestionnaire(t, st, rel, w)
			d := got.diff()
			require.NotEmptyf(t, d, "снятие конъюнкта %q не изменило НИ ОДНОГО вердикта — "+
				"конъюнкт тождественно истинен на этой фикстуре, и совпадение вердиктов "+
				"держится не им", wk)
			t.Logf("снят конъюнкт %q → расхождений %d, первое: %s", wk, len(d), d[0])
		})
	}
	rel.weaken = weakenNothing

	// ── снятие предмета: каждый источник по отдельности ────────────────────────
	cases := []struct {
		name string
		drop func(Atom) bool
	}{
		{"выдача", func(a Atom) bool { return a.Kind == AtomBinding }},
		{"каскад аккаунта", func(a Atom) bool { return a.Kind == AtomFact && a.ParentType == "account" }},
		{"каскад кластера", func(a Atom) bool { return a.Kind == AtomFact && a.ParentType == "cluster" }},
		{"факты на самом объекте", func(a Atom) bool { return a.Kind == AtomFact && a.ParentType == "" }},
	}
	for _, c := range cases {
		t.Run("снят источник: "+c.name, func(t *testing.T) {
			rel.mutate = func(p Plan) Plan {
				out := Plan{Type: p.Type, Relation: p.Relation, Conditioned: p.Conditioned}
				for _, a := range p.Atoms {
					if c.drop(a) {
						continue
					}
					out.Atoms = append(out.Atoms, a)
				}
				if len(out.Atoms) == 0 {
					// План без источников — законный исход снятия; чтобы запрос
					// собрался, ставится заведомо невыполнимый атом.
					out.Atoms = []Atom{{Kind: AtomFact, Relation: "nothing_grants_this"}}
				}
				return out
			}
			got := runFullQuestionnaire(t, st, rel, w)
			d := got.diff()
			require.NotEmptyf(t, d, "снятие источника %q не изменило НИ ОДНОГО вердикта — "+
				"либо источник мёртв, либо вопросник его не спрашивает; в обоих случаях "+
				"совпадение на нём ничего не доказывает", c.name)
			t.Logf("снят источник %q → расхождений %d, первое: %s", c.name, len(d), d[0])
		})
	}
	rel.mutate = nil
}

// TestNarrowAndFullFormEAgreeOnTheBenchScenario связывает две реализации формы E.
//
// Измеренная в XC-10 (`relational.go`) выражена под один тип и её числа
// опубликованы; полномодельная собирается из разбора. Две реализации одного
// предмета расходятся молча — поэтому на сценарии, который измерялся, обе обязаны
// отвечать одинаково. Без этой пробы полномодельная форма могла бы быть верной, а
// опубликованные числа — относиться к другому поведению.
func TestNarrowAndFullFormEAgreeOnTheBenchScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("поднимает контейнеры; -short")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)
	m, err := ParseModel(canon)
	require.NoError(t, err)

	sc := NewScenario(6, 3, 3, "editor", DefaultVerbs())
	cfg := DefaultConfig()
	cfg.Ns = []int{sc.N}
	cfg.Verbs = sc.Verbs
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)
	narrow, err := r.NewSeededStore(ctx, FormE, sc, true, "narrow-vs-full")
	require.NoError(t, err)
	defer func() { _ = narrow.Teardown(ctx) }()

	// Полномодельная форма засевается ТЕМ ЖЕ сценарием, переведённым в её схему.
	full, err := newFullRelStore(ctx, stack.RelDSN, "fm_narrow_vs_full", m)
	require.NoError(t, err)
	defer func() { _ = full.Close(ctx) }()
	require.NoError(t, full.Seed(ctx, benchScenarioRows(sc)))

	qs := questionSet(sc, "granted")
	var mismatch []string
	for _, q := range qs {
		wantNarrow, _, cerr := narrow.Check(ctx, q.Subject, q.Verb, q.Object)
		require.NoError(t, cerr)
		ot, oid, serr := splitObject(q.Object)
		require.NoError(t, serr)
		got, gerr := full.Allowed(ctx, q.Subject, q.Verb, ot, []string{oid})
		require.NoErrorf(t, gerr, "полномодельная форма на %s", q.Name)
		if got[oid] != wantNarrow {
			mismatch = append(mismatch, fmt.Sprintf("%s: измеренная %v, полномодельная %v",
				q.Name, wantNarrow, got[oid]))
		}
		require.Equalf(t, q.Want, wantNarrow, "измеренная форма E разошлась с намерением фикстуры на %s", q.Name)
	}
	require.Emptyf(t, mismatch, "две реализации формы E разошлись на измерявшемся сценарии:\n%s",
		strings.Join(mismatch, "\n"))
	t.Logf("сверено вопросов сценария XC-10: %d", len(qs))
}

// benchScenarioRows переводит сценарий XC-10 в строки полномодельной схемы.
func benchScenarioRows(sc Scenario) relRows {
	labels := fmt.Sprintf(`{%q:%q}`, "authzformbench", "in-set")
	var r relRows
	obj := func(ref string, l string, ptr, parentRef string) {
		ot, oid, err := splitObject(ref)
		if err != nil {
			panic(err)
		}
		r.Mirror = append(r.Mirror, [3]string{ot, oid, l})
		if ptr != "" {
			pt, pid, perr := splitObject(parentRef)
			if perr != nil {
				panic(perr)
			}
			r.Edges = append(r.Edges, [5]string{ot, oid, ptr, pt, pid})
		}
	}
	obj(clusterObj, "{}", "", "")
	obj(accountObj, "{}", "cluster", clusterObj)
	obj(otherAccountObj, "{}", "cluster", clusterObj)
	obj(projectObj, "{}", "account", accountObj)
	obj(otherProjectObj, "{}", "account", otherAccountObj)
	for i := 0; i < sc.N+sc.Spare; i++ {
		l := "{}"
		if i < sc.N {
			l = labels
		}
		obj(sc.Object(i), l, "project", projectObj)
	}
	obj(sc.ForeignObject(), labels, "project", otherProjectObj)

	r.Bindings = append(r.Bindings, [4]string{"ab-bench", "project", strings.TrimPrefix(projectObj, "project:"), sc.Role})
	for _, s := range sc.Subjects {
		r.BindSubj = append(r.BindSubj, [2]string{"ab-bench", s})
	}
	r.Selectors = append(r.Selectors, [3]string{"ab-bench", BenchType, labels})
	for _, v := range sc.Verbs {
		r.RoleVerbs = append(r.RoleVerbs, [2]string{sc.Role, v})
	}
	return r
}

// TestConditionedTupleHasNoExpressionInFormE — исход (в), доказанный, а не
// объявленный.
//
// Четыре записи прямых списков модели несут условие на кортеже (`with
// mfa_fresh`). У формы E места под условие НЕТ: её факт — строка, а вторая
// половина условия живёт не в хранилище, а в КОНТЕКСТЕ запроса (свежесть
// подтверждения личности, набор способов, время). Строка не умеет отвечать
// по-разному двум запросам, различающимся только контекстом.
//
// Проба показывает это исходом, а не разбором модели: движок отвечает на два
// запроса, отличающихся ТОЛЬКО контекстом, по-разному; форма E на них двоих
// отвечает одинаково. Разбор один и то же утверждал бы, но не отличался бы от
// утверждения о том, чего не пробовали.
//
// Здесь же — почему это не сворачивается в «форма E запрещает и потому
// безопасна»: она отвечает ОДИНАКОВО, и какое это «одинаково» — вопрос того, как
// её засеять. Если условный факт положить строкой, она разрешит и там, где движок
// отказал, то есть ОСЛАБИТ гейт свежести MFA; если не класть — откажет и там, где
// движок разрешил, то есть сломает штатный доступ. Обе половины проверяются.
func TestConditionedTupleHasNoExpressionInFormE(t *testing.T) {
	if testing.Short() {
		t.Skip("доказательство на живом OpenFGA; -short")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)
	m, err := ParseModel(canon)
	require.NoError(t, err)

	// Отношение с условием НАХОДИТСЯ в модели, а не выписывается: снимут условие —
	// проба перестанет иметь предмет и скажет об этом, а не позеленеет молча.
	var typeName, relName, condName string
	for _, ty := range m.Types {
		for _, r := range ty.Relations {
			for _, term := range r.Terms {
				for _, d := range term.Direct {
					if d.Condition != "" && d.Type == "user" && typeName == "" {
						typeName, relName, condName = ty.Name, r.Name, d.Condition
					}
				}
			}
		}
	}
	require.NotEmptyf(t, typeName, "в модели не осталось ни одной записи с условием на кортеже — "+
		"эта проба потеряла предмет; её надо снять вместе с записью об исходе (в), а не оставлять зелёной")
	t.Logf("предмет: %s.%s с условием %q", typeName, relName, condName)

	w, err := buildWorld(m)
	require.NoError(t, err)
	modelJSON, err := TransformDSL(canon)
	require.NoError(t, err)
	st, err := stack.NewStore(ctx, "fullmodel-condition", modelJSON)
	require.NoError(t, err)
	defer func() { _ = st.Delete(ctx) }()
	_, err = st.WriteTuples(ctx, w.engineTuples())
	require.NoError(t, err)

	// Объект того типа берётся из мира; субъект — новый, чтобы ответ держался
	// условным кортежем, а не чем-то ещё.
	var object string
	for _, o := range w.Objects {
		if o.Type == typeName {
			object = o.Ref()
			break
		}
	}
	require.NotEmpty(t, object)
	const subject = "user:u-mfa"
	_, err = st.WriteTuples(ctx, []Tuple{{User: subject, Relation: relName, Object: object,
		Condition: &TupleCondition{Name: condName}}})
	require.NoError(t, err, "запись условного кортежа")

	// `current_time` подаётся ЯВНО: движок этой версии его не подставляет сам и
	// отвечает отказом разбора («missing context parameters»), а не «запрещено».
	// Разница существенна — без явного параметра проба доказала бы, что форма E
	// расходится с ОШИБКОЙ, а не с вердиктом.
	now := time.Now().UTC()
	fresh := map[string]any{
		"acr_value":    "3",
		"amr_claims":   []string{"webauthn"},
		"current_time": now.Format(time.RFC3339),
		"mfa_at":       now.Format(time.RFC3339),
	}
	stale := map[string]any{
		"acr_value":    "3",
		"amr_claims":   []string{"webauthn"},
		"current_time": now.Format(time.RFC3339),
		"mfa_at":       now.Add(-2 * time.Hour).Format(time.RFC3339),
	}
	okFresh, err := st.CheckWithContext(ctx, subject, relName, object, fresh)
	require.NoError(t, err)
	okStale, err := st.CheckWithContext(ctx, subject, relName, object, stale)
	require.NoError(t, err)
	require.Truef(t, okFresh, "движок отказал при выполненном условии — фикстура условия неверна, "+
		"и вывод о невыразимости был бы сделан из сломанного контроля")
	require.Falsef(t, okStale, "движок разрешил при НЕвыполненном условии — условие не действует, "+
		"и разница, которую форме E предстоит не выразить, отсутствует")

	// Форма E: тот же факт строкой. У неё нет входа для контекста — сигнатура
	// вердикта его не принимает вовсе, и это не упущение реализации, а свойство
	// формы: строка не может отвечать по-разному на два одинаковых запроса.
	rel, err := newFullRelStore(ctx, stack.RelDSN, "fm_condition", m)
	require.NoError(t, err)
	defer func() { _ = rel.Close(ctx) }()
	rows, err := w.relRows()
	require.NoError(t, err)
	ot, oid, err := splitObject(object)
	require.NoError(t, err)
	rows.Facts = append(rows.Facts, [4]string{ot, oid, relName, subject})
	require.NoError(t, rel.Seed(ctx, rows))

	got, err := rel.Allowed(ctx, subject, relName, ot, []string{oid})
	require.NoError(t, err)
	require.Truef(t, got[oid], "форма E, засеянная условным фактом как строкой, отказала — "+
		"тогда её расхождение с движком было бы в другую сторону, и вывод ниже пришлось бы переписать")

	// Вот оно, расхождение: движок различил два запроса, форма E — нет.
	require.NotEqualf(t, okStale, got[oid],
		"форма E ответила на просроченное подтверждение личности так же, как движок — "+
			"значит условие она всё-таки выражает, и запись об исходе (в) неверна")
	t.Logf("движок: при свежем подтверждении %v, при просроченном %v; форма E: %v в обоих случаях "+
		"— условие на кортеже не выражается", okFresh, okStale, got[oid])
}

// TestParentFactConjunctIsLoadBearingWhereItHasASubject — доказательство
// конъюнкта, у которого на каноне нет предмета.
//
// Атом факта на ПРЕДКЕ отбирает предка по типу и по ненулевой глубине. На
// канонической модели вторая половина не различает ничего: ни один тип не
// указывает на свой собственный тип, поэтому отбор по типу и так исключает сам
// объект — инъекция «снять глубину» не меняет там ни одного вердикта. Это НЕ
// повод объявить конъюнкт лишним и НЕ повод оставить его недоказанным: класс, от
// которого он защищает, — самовложение, и модель его допускает грамматически.
//
// Поэтому предмет конструируется: крошечная модель, где тип указывает на себя,
// и три ответа рядом — движка, формы E с конъюнктом и формы E без него. Средний
// обязан совпасть с левым, правый — разойтись. Без левого (движка) это было бы
// сравнение двух своих реализаций между собой.
func TestParentFactConjunctIsLoadBearingWhereItHasASubject(t *testing.T) {
	if testing.Short() {
		t.Skip("доказательство на живом OpenFGA; -short")
	}
	ctx := t.Context()
	stack, _ := bootForTest(ctx, t)

	const dsl = `model
  schema 1.1

type user

type folder
  relations
    define parent: [folder]
    define owner: [user]
    define super_admin: owner from parent
    define v_get: [user] or super_admin
`
	m, err := ParseModel(dsl)
	require.NoError(t, err)
	p, err := m.Compile("folder", "v_get")
	require.NoError(t, err)
	var selfTyped bool
	for _, a := range p.Atoms {
		if a.Kind == AtomFact && a.ParentType == "folder" {
			selfTyped = true
		}
	}
	require.Truef(t, selfTyped, "синтетическая модель не дала атома на предке СВОЕГО типа — "+
		"предмет конъюнкта не сконструирован, и проба ничего не доказывает")

	modelJSON, err := TransformDSL(dsl)
	require.NoError(t, err)
	st, err := stack.NewStore(ctx, "self-nested-folder", modelJSON)
	require.NoError(t, err)
	defer func() { _ = st.Delete(ctx) }()

	// Папка БЕЗ родителя, её владелец — user:x. Модель даёт `v_get` владельцу
	// РОДИТЕЛЯ, а не владельцу самой папки; родителя нет, значит ответ — отказ.
	require.NoError(t, func() error {
		_, werr := st.WriteTuples(ctx, []Tuple{{User: "user:x", Relation: "owner", Object: "folder:a"}})
		return werr
	}())
	engine, err := st.Check(ctx, "user:x", "v_get", "folder:a")
	require.NoError(t, err)
	require.Falsef(t, engine, "движок разрешил владельцу САМОЙ папки то, что модель даёт владельцу "+
		"её родителя — фикстура не воспроизводит класс, и вывод был бы сделан из сломанного контроля")

	rel, err := newFullRelStore(ctx, stack.RelDSN, "fm_self_nested", m)
	require.NoError(t, err)
	defer func() { _ = rel.Close(ctx) }()
	require.NoError(t, rel.Seed(ctx, relRows{
		Mirror: [][3]string{{"folder", "a", "{}"}},
		Facts:  [][4]string{{"folder", "a", "owner", "user:x"}},
	}))

	with, err := rel.Allowed(ctx, "user:x", "v_get", "folder", []string{"a"})
	require.NoError(t, err)
	require.Equalf(t, engine, with["a"],
		"форма E с конъюнктом разошлась с движком на самовложенном типе")

	rel.weaken = weakenParentDepth
	without, err := rel.Allowed(ctx, "user:x", "v_get", "folder", []string{"a"})
	rel.weaken = weakenNothing
	require.NoError(t, err)
	require.NotEqualf(t, with["a"], without["a"],
		"снятие конъюнкта глубины не изменило вердикт даже на типе, указывающем на СЕБЯ — "+
			"конъюнкт не различает ничего нигде, и его надо снять, а не оставлять как защиту")
	t.Logf("движок %v · форма E с конъюнктом %v · без него %v", engine, with["a"], without["a"])
}
