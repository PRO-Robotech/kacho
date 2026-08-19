// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package relverdict_test

// s2_blockers_integration_test.go — S2: ЧЕТЫРЕ БЛОКЕРА СОБСТВЕННОЙ КОНСТАНТЫ.
//
// Сценарии R7-1-10 · 12 · 13 · 14 · 18 приёмки R7-1. Здесь утверждается ИСХОД —
// СТРОК ПРОЧИТАНО, — а не форма плана. «В плане есть index scan» есть
// утверждение о выборе планировщика на данной статистике: оно зеленеет ровно
// тогда, когда статистика удобна, то есть в точности в том случае, ради
// которого проба и написана.
//
// Величина берётся ОБЪЯВЛЕННЫМ прибором (`pg/planrows`) с ЗАХВАЧЕННОГО у
// продукта оператора: проба, собирающая текст запроса своей рукой, планировала
// бы другой запрос и молчала бы об этом.

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/planrows"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/relverdict"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/scalegrid"
)

// probeObjectID — объект, о котором задаётся вопрос во всех пробах файла.
const probeObjectID = "repo-0000000"

// askAndExplain — задать НАСТОЯЩИЙ вопрос вердикта и снять с него разложение.
//
// Оператор захватывается трассировщиком у вызова `Ask` и сверяется дословно с
// текстом продукта — иначе доказано было бы лишь то, что пакет что-то отправил.
func askAndExplain(t *testing.T, ctx context.Context, tx pgx.Tx, cap *verdictCapture,
	objectID string) (relverdict.Verdict, planrows.Measurement) {
	t.Helper()
	cap.reset()
	v, _, err := relverdict.Ask(ctx, tx, relverdict.Query{
		Subject:    probeSubject,
		ObjectType: probeModelType,
		ObjectID:   objectID,
		Relation:   probeRelation,
	})
	if err != nil {
		t.Fatalf("вопрос вердикта об %s: %v", objectID, err)
	}
	axis, err := relverdict.LabelAxisForTest(probeModelType)
	if err != nil {
		t.Fatalf("ось меток: %v", err)
	}
	stmts := cap.matching(relverdict.VerdictQuerySQLForTest(axis))
	if len(stmts) != 1 {
		t.Fatalf("захвачено %d операторов, тождественных запросу вердикта, ожидался один: "+
			"ноль означает, что продукт исполняет другой текст, больше одного — что мерятся два вопроса",
			len(stmts))
	}
	var raw []byte
	if err := tx.QueryRow(ctx,
		"EXPLAIN (ANALYZE, BUFFERS, VERBOSE, FORMAT JSON) "+stmts[0].sql, stmts[0].args...).Scan(&raw); err != nil {
		t.Fatalf("снятие плана: %v", err)
	}
	m, err := planrows.Extract(raw, probeWantRelations)
	if err != nil {
		t.Fatalf("прибор отказал на живом плане: %v", err)
	}
	return v, m
}

// rowsOf — строк, отнесённых прибором к названному отношению.
func rowsOf(m planrows.Measurement, relation string) int64 {
	var n int64
	for _, a := range m.Accesses {
		if a.Relation == relation {
			n += a.Rows
		}
	}
	return n
}

// ── R7-1-18: ОБХОД ЦЕПИ ОБЛАСТЕЙ ПЕРЕСТАЁТ ПЕРЕВЫЧИСЛЯТЬ ЗАМЫКАНИЕ ──────────

// TestR7_1_18_ScopeChainIsReadOnceNotRecomputed — R7-1-18, задача #732.
//
// `resource_parent_edge` хранит ЗАМЫКАНИЕ: у объекта лежит строка на КАЖДОГО
// предка, а не только на непосредственного (первичный ключ уникален по глубине,
// сама глубина ограничена схемой). Рекурсивный обход по этой таблице читает
// замыкание ЗАНОВО на каждом шаге и тем самым перевычисляет уже известное:
// строк выходит `1 + d(d+1)/2` там, где различных сущностей на цепи `1 + d`.
//
// Утверждается ИСХОД: строк, прочитанных из таблицы рёбер за один вердикт,
// не больше, чем строк замыкания у самого объекта. Это и есть «одно обращение».
func TestR7_1_18_ScopeChainIsReadOnceNotRecomputed(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	tx, cap := openProbeTx(t, ctx)
	f := newGridFixture(t, ctx, tx)
	f.growN(t, ctx, 200)
	f.setR(t, ctx, 1, scalegrid.RecruitDirect)
	analyze(t, ctx, tx)

	// ПРЕДПОСЫЛКА: цепь объекта полна и глубока. На вырожденной цепи (d = 1)
	// перевычисление ненаблюдаемо by construction — обе величины совпадают.
	var closure, distinctAncestors int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int, count(DISTINCT (parent_type, parent_id))::int
		  FROM kacho_iam.resource_parent_edge
		 WHERE object_type = $1 AND object_id = $2`,
		probeModelType, probeObjectID).Scan(&closure, &distinctAncestors); err != nil {
		t.Fatalf("замыкание объекта: %v", err)
	}
	if closure != probeChainDepth || distinctAncestors != probeChainDepth {
		t.Fatalf("предпосылка не выполнена: строк замыкания %d, различных предков %d, "+
			"ожидалось по %d. На вырожденной цепи проба мерила бы случай, в котором "+
			"перевычисления не бывает вовсе", closure, distinctAncestors, probeChainDepth)
	}

	v, m := askAndExplain(t, ctx, tx, cap, probeObjectID)

	// Положительный контроль: мерится ВЕРНО отвеченный вопрос. Иначе «дёшево»
	// означало бы «не сработало».
	if v != relverdict.Allow {
		t.Fatalf("вердикт %s там, где право выдано ролью на проекте цепи", v)
	}

	// Утверждение НА УЗЕЛ, а не на сумму: прибор намеренно НЕ схлопывает пару
	// «Bitmap Heap Scan + Bitmap Index Scan» по имени отношения (иначе он скрыл
	// бы два ДЕЙСТВИТЕЛЬНО разных скана), поэтому одно обращение читается в
	// сумме как удвоенное. Свойство же требуется от обращения: замыкание берётся
	// ОДНИМ проходом (циклов один) и отдаёт не больше строк, чем у объекта их
	// есть.
	var edge []planrows.Access
	for _, a := range m.Accesses {
		if a.Relation == "resource_parent_edge" {
			edge = append(edge, a)
		}
	}
	if len(edge) == 0 {
		t.Fatalf("в плане нет ни одного узла по таблице рёбер при непустом замыкании: прибор смотрел "+
			"не туда, и «дёшево» здесь означало бы «не измерено».\n%s", m.Census)
	}
	total := rowsOf(m, "resource_parent_edge")
	t.Logf("цепь d=%d: строк замыкания у объекта %d · узлов по таблице рёбер %d · "+
		"строк за вердикт всего %d", probeChainDepth, closure, len(edge), total)
	for _, a := range edge {
		t.Logf("  %s: строк %d, циклов %d", a.NodeType, a.Rows, a.Loops)
		if a.Loops != 1 {
			t.Errorf("узел %s по таблице рёбер идёт в %d циклов: замыкание перевычисляется, "+
				"а не читается один раз", a.NodeType, a.Loops)
		}
		if a.Rows > int64(closure) {
			t.Errorf("узел %s по таблице рёбер отдал %d строк при %d строках замыкания у объекта: "+
				"обход читает не цепь объекта, а таблицу", a.NodeType, a.Rows, closure)
		}
	}
}

// ── R7-1-10: ЧУЖИЕ ВЫДАЧИ НЕ ВХОДЯТ В СТОИМОСТЬ ВЕРДИКТА ────────────────────

// foreignBindingsCeiling — потолок прироста строк за вердикт, объявленный ДО
// прогона.
//
// Стоимость вердикта складывается из постоянной части (сам объект, его правило,
// цепь областей) и из выдач, НАЗЫВАЮЩИХ спрашиваемого. Ни одна из них от числа
// ЧУЖИХ выдач не зависит, поэтому идеальный прирост — ноль. Потолок в 64 строки
// — запас на дребезг статистики и на смену плана при переходе от одной выдачи к
// десяти тысячам; деградация этого класса даёт порядки (замер S1: 183 → 1 001 049),
// и шестьдесят четыре отделяют её от дребезга с огромным запасом.
const foreignBindingsCeiling = 64

// TestR7_1_10_ForeignBindingsDoNotEnterVerdictCost — R7-1-10, ОБЕ стороны оси B.
//
// Ось имеет две стороны, и раскладка, проверяющая одну, зелена при сломанной
// другой:
//
//	сторона-1 — чужие выдачи в ЭТОЙ области (заход со стороны области читает их все);
//	сторона-2 — выдачи ЭТОМУ субъекту в ДРУГИХ областях (заход со стороны субъекта
//	            читает их все).
//
// Индекс по одному лишь субъекту закрывает первую и ОТКРЫВАЕТ вторую. Поэтому
// обе прогоняются против одной контрольной раскладки.
func TestR7_1_10_ForeignBindingsDoNotEnterVerdictCost(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	const foreign = 10000

	ctx := context.Background()
	tx, cap := openProbeTx(t, ctx)
	f := newGridFixture(t, ctx, tx)
	f.growN(t, ctx, 200)
	f.setR(t, ctx, 1, scalegrid.RecruitDirect)
	analyze(t, ctx, tx)

	vCtl, mCtl := askAndExplain(t, ctx, tx, cap, probeObjectID)
	ctl := mCtl.Rows
	t.Logf("контрольная раскладка: одна выдача, называющая спрашиваемого · вердикт %s · строк %d",
		vCtl, ctl)

	// ── сторона-1: чужие выдачи НА ТОЙ ЖЕ области ────────────────────────────
	f.growB(t, ctx, foreign)
	analyze(t, ctx, tx)
	v1, m1 := askAndExplain(t, ctx, tx, cap, probeObjectID)
	t.Logf("сторона-1 (%d чужих выдач на цепи областей): вердикт %s · строк %d · прирост %d",
		foreign, v1, m1.Rows, m1.Rows-ctl)

	// ── сторона-2: выдачи ТОМУ ЖЕ субъекту в ДРУГИХ областях ────────────────
	seedElsewhereBindings(t, ctx, tx, f, foreign)
	analyze(t, ctx, tx)
	v2, m2 := askAndExplain(t, ctx, tx, cap, probeObjectID)
	t.Logf("сторона-2 (%d выдач спрашиваемому ВНЕ цепи областей): вердикт %s · строк %d · прирост %d",
		foreign, v2, m2.Rows, m2.Rows-ctl)

	// Ответ во всех трёх обязан совпасть — иначе замер сравнивает разные вопросы.
	if vCtl != v1 || vCtl != v2 {
		t.Fatalf("вердикты разошлись: контроль %s, сторона-1 %s, сторона-2 %s — "+
			"замер сравнивал бы разные вопросы", vCtl, v1, v2)
	}
	if vCtl != relverdict.Allow {
		t.Fatalf("контрольная раскладка дала %s: мерилась бы стоимость неверного ответа", vCtl)
	}

	for _, c := range []struct {
		name string
		got  int64
	}{{"сторона-1 (чужие выдачи в этой области)", m1.Rows},
		{"сторона-2 (выдачи этому субъекту вне этой области)", m2.Rows}} {
		if d := c.got - ctl; d > foreignBindingsCeiling {
			t.Errorf("%s: строк за вердикт %d против %d в контрольной, прирост %d при потолке %d. "+
				"Чужие выдачи входят в стоимость вердикта", c.name, c.got, ctl, d, foreignBindingsCeiling)
		}
	}
}

// seedElsewhereBindings — выдачи, называющие СПРАШИВАЕМОГО, в областях, которых
// НЕТ на цепи спрашиваемого объекта.
//
// Ровно та величина, на которую переезжает неограниченность, если субъект
// сделать единственным входом: проектов в облаке неограниченно, и у группы,
// используемой во многих проектах, их много и на практике.
func seedElsewhereBindings(t *testing.T, ctx context.Context, tx pgx.Tx, f *gridFixture, n int) {
	t.Helper()
	s := scalegrid.NewSeeder(tx)
	for i := 0; i < n; i++ {
		pid := fmt.Sprintf("prj-e%07d", i)
		must(t, s.QueueRaw(ctx,
			`INSERT INTO kacho_iam.projects (id, account_id, name) VALUES ($1, 'acc-1', $1)`, pid))
	}
	// Роли — своим пулом: действующая выдача уникальна по (субъект, роль, область),
	// а область здесь у каждой своя, поэтому хватает одной роли на все.
	for i := 0; i < n; i++ {
		bid := fmt.Sprintf("acb-e%07d", i)
		pid := fmt.Sprintf("prj-e%07d", i)
		must(t, s.QueueRaw(ctx,
			`INSERT INTO kacho_iam.access_bindings
			   (id, subject_type, subject_id, role_id, resource_type, resource_id, status)
			 VALUES ($1, 'user', 'usr-1', 'rol-anchor', 'project', $2, 'ACTIVE')`, bid, pid))
		must(t, s.QueueRaw(ctx,
			`INSERT INTO kacho_iam.access_binding_subjects (binding_id, subject_type, subject_id)
			 VALUES ($1, 'user', 'usr-1')`, bid))
	}
	must(t, s.Flush(ctx))
}

// analyze — собрать статистику ВНУТРИ посева точки.
//
// Без неё планировщик выбирает план по оценкам пустой таблицы, и разбирался бы
// план, которого на развёрнутой базе не бывает.
func analyze(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if _, err := tx.Exec(ctx, `ANALYZE kacho_iam.access_bindings, kacho_iam.access_binding_subjects,
		kacho_iam.resource_mirror, kacho_iam.resource_parent_edge, kacho_iam.relation_fact,
		kacho_iam.role_verb, kacho_iam.role_rule_selectors, kacho_iam.group_members`); err != nil {
		t.Fatalf("сбор статистики: %v", err)
	}
}

// ── R7-1-12: ИНДЕКС СУЩЕСТВУЕТ, ПРИМЕНЁН И ОБСЛУЖИВАЕТ ВЕРДИКТ ──────────────

// TestR7_1_12_SubjectAndScopeResolveOnOneIndex — R7-1-12.
//
// Утверждается СВОЙСТВО — «пара субъект + область разрешается одним индексом на
// одном отношении», — а не раскладка колонок: любая другая, дающая то же
// свойство, ему удовлетворяет, и пинить порядок значило бы краснеть на верной
// правке.
//
// Свойство предъявляется ИСХОДОМ и ИНЪЕКЦИЕЙ. Объявленный, но не применяемый
// индекс — тот же «объявленный и никем не читаемый страж»: проба обязана уметь
// отличить его от работающего, поэтому индекс снимается прямо в транзакции, и
// та же раскладка обязана стать дороже. Без этого плеча зелёное означало бы
// лишь «строк мало», а не «мало ИЗ-ЗА индекса».
func TestR7_1_12_SubjectAndScopeResolveOnOneIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	const foreign = 10000

	ctx := context.Background()
	tx, cap := openProbeTx(t, ctx)
	f := newGridFixture(t, ctx, tx)
	f.growN(t, ctx, 200)
	f.setR(t, ctx, 1, scalegrid.RecruitDirect)
	f.growB(t, ctx, foreign)
	analyze(t, ctx, tx)

	// ПРЕДПОСЫЛКА: оба предиката стоят на ОДНОМ отношении. Иначе одного индекса,
	// обслуживающего оба, не существует by construction, и «свойство есть»
	// означало бы «план сегодня удобен».
	var carried int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int FROM information_schema.columns
		 WHERE table_schema = 'kacho_iam' AND table_name = 'access_binding_subjects'
		   AND column_name IN ('subject_type','subject_id','resource_type','resource_id')`).Scan(&carried); err != nil {
		t.Fatalf("состав колонок: %v", err)
	}
	if carried != 4 {
		t.Fatalf("на строке субъекта выдачи %d из четырёх колонок пары «субъект + область»: "+
			"предикаты стоят на разных отношениях, и утверждать свойство не о чем", carried)
	}

	vWith, mWith := askAndExplain(t, ctx, tx, cap, probeObjectID)
	if vWith != relverdict.Allow {
		t.Fatalf("вердикт %s: мерилась бы стоимость неверного ответа", vWith)
	}
	subjWith := rowsOf(mWith, "access_binding_subjects")
	t.Logf("с индексом: строк за вердикт %d, из них по строкам субъектов выдач %d "+
		"(в облаке %d чужих выдач)", mWith.Rows, subjWith, foreign)

	// ── ИНЪЕКЦИЯ: индекс снят, всё прочее не тронуто ─────────────────────────
	if _, err := tx.Exec(ctx,
		`DROP INDEX kacho_iam.access_binding_subjects_subject_scope_idx`); err != nil {
		t.Fatalf("снятие индекса: имя изменилось или индекса нет — инъекция не воспроизводит "+
			"состояние «объявлен, но не применяется»: %v", err)
	}
	analyze(t, ctx, tx)
	vNo, mNo := askAndExplain(t, ctx, tx, cap, probeObjectID)
	subjNo := rowsOf(mNo, "access_binding_subjects")
	t.Logf("без индекса: строк за вердикт %d, из них по строкам субъектов выдач %d", mNo.Rows, subjNo)

	// Ответ обязан совпасть: инъекция меняет СТОИМОСТЬ, а не смысл.
	if vNo != vWith {
		t.Errorf("снятие индекса изменило ответ (%s против %s): инъекция сравнивала бы "+
			"разные вопросы", vNo, vWith)
	}
	if subjNo <= subjWith {
		t.Errorf("без индекса по строкам субъектов выдач прочитано %d, с индексом %d — "+
			"инъекция не воспроизвела дефекта, и зелёное с индексом ничего не доказывает: "+
			"оно означало бы «строк мало», а не «мало ИЗ-ЗА индекса»", subjNo, subjWith)
	}
	if subjWith > foreignBindingsCeiling {
		t.Errorf("с индексом по строкам субъектов выдач прочитано %d при потолке %d: "+
			"индекс объявлен, но набор им не сужается", subjWith, foreignBindingsCeiling)
	}
}
