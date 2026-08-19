// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package relverdict_test

// scopereach_integration_test.go — ОБЛАСТЬ ВЕРДИКТА ДОСТАЁТ ДО КОРНЯ НА ТОЙ
// ФОРМЕ ЦЕПИ, КОТОРУЮ ПРОИЗВОДЯТ ПРОИЗВОДИТЕЛИ.
//
// # Почему эта проба существует
//
// Обход цепи областей и одноразовое чтение таблицы рёбер равносильны РОВНО
// ТОГДА, когда у каждого объекта лежит строка на КАЖДОГО предка до корня. Это
// утверждение о ПРОИЗВОДИТЕЛЯХ, а не о схеме: ключ (объект, глубина) и проверка
// глубины 1..4 допускают обе формы — и замыкание, и список непосредственных
// рёбер.
//
// Проверено по дереву, и предпосылка ЛОЖНА:
//
//	pkg/ownerregister.ParentChain(nil, projectID, "") — ОДНО звено (проект);
//	registry шлёт [реестр, проект] — без аккаунта;
//	cluster не шлёт НИКТО.
//
// Комментарий самого консьюмера говорит это прямо: «аккаунт достигается с самого
// проекта его собственным ребром» — то есть транзитивным обходом.
//
// # Почему фикстура сеется ПРОИЗВОДИТЕЛЕМ, а не сырым SQL
//
// Сырой SQL позволяет положить цепь любой формы, в том числе той, которой в
// продукте не бывает. Тогда «формы равносильны» становится свойством ФИКСТУРЫ:
// она сама кладёт то, что потом проверяет. Ровно этот приём уже принят гейтом
// полноты цепи; здесь он применяется к достижимости.
//
// # Что проба утверждает
//
// На цепи, набранной производителем (сеть → проект, проект → аккаунт, аккаунт →
// кластер, по ОДНОМУ звену у каждого — как шлют в дереве), действуют:
//
//	· выдача роли на АККАУНТ — два звена вверх от объекта;
//	· прямой факт system_admin на КЛАСТЕРЕ — три звена вверх.
//
// Оба — верхние уровни `security.md` §«Три уровня супер-доступа», включая
// аварийный путь администратора облака. Если область схлопнулась до «объект +
// проект», оба отвечают отказом, и отказ этот НЕОТЛИЧИМ от честного.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/relverdict"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/resource_mirror"
)

// seedChainThroughProducer — цепь, набранная ЕДИНСТВЕННЫМ производителем рёбер
// дерева, каждым звеном по отдельности.
//
// Форма вызова — та, которой шлют регистрации. Но САМИ вызовы для проекта и
// аккаунта дерево не делает: их объектов не регистрирует никто. Здесь они
// досеяны, чтобы отделить свойство ЗАПРОСА (доходит ли обход до корня по полной
// цепи) от свойства ПРОИЗВОДИТЕЛЕЙ (существует ли полная цепь). Смешать их —
// значит объявить закрытым то, что не закрыто.
func seedChainThroughProducer(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	base := time.Now().UTC().Truncate(time.Microsecond)
	regs := []struct {
		objectType, objectID string
		chain                []string
		project, account     string
	}{
		// Сеть vpc: ровно то, что шлёт vpc — ParentChain(nil, project, "").
		// Тип называется словарём КАТАЛОГА: им назван `resource_mirror.object_type`,
		// и перевод в словарь модели делает сам производитель.
		{catalogFormOf(t, "vpc_network"), "net-1", ownerregister.ParentChain(nil, "prj-1", ""), "prj-1", ""},
		// Проект: своё собственное ребро на аккаунт — тоже досеяно (см. ниже).
		{catalogFormOf(t, "project"), "prj-1", ownerregister.ParentChain(nil, "", "acc-1"), "", "acc-1"},
		// ВНИМАНИЕ: ни этого звена, ни предыдущего дерево НЕ ПРОИЗВОДИТ. Рёбра
		// появляются единственным путём — регистрацией ресурса, — и зовут её
		// пять сервисов-соседей для СВОИХ ресурсов; объектов типа project и
		// account не регистрирует никто, включая сам iam. Оба звена досеяны
		// здесь НАМЕРЕННО, чтобы проба судила достижимость ПРИ ПОЛНОЙ ЦЕПИ:
		// свойство запроса отделено от свойства производителей.
		//
		// Что происходит на цепи БЕЗ этих звеньев — утверждает соседняя проба
		// TestScopeStopsAtTheProjectOnTheChainTheTreeActuallyProduces, и её
		// ответ сегодня «не находит».
		{catalogFormOf(t, "account"), "acc-1", []string{"cluster:cluster_kacho_root"}, "", ""},
	}
	for i, r := range regs {
		if _, err := resource_mirror.UpsertTx(ctx, tx, resource_mirror.Row{
			ObjectType:      r.objectType,
			ObjectID:        r.objectID,
			ParentProjectID: r.project,
			ParentAccountID: r.account,
			ParentChain:     r.chain,
			SourceVersion:   base.Add(time.Duration(i) * time.Millisecond),
		}); err != nil {
			t.Fatalf("регистрация %s:%s через производителя: %v", r.objectType, r.objectID, err)
		}
	}
}

// TestScopeReachesTheRootOnTheChainProducersActuallyWrite — Б1.
//
// Красная, пока область вердикта читается ОДНИМ обращением к таблице рёбер:
// у сети лежит одно ребро (проект), и ни аккаунт, ни кластер в область не
// попадают.
func TestScopeReachesTheRootOnTheChainProducersActuallyWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	withTx(t, func(ctx context.Context, tx pgx.Tx) {
		seedTenant(t, ctx, tx)
		seedRole(t, ctx, tx, "rol-acc", "vpc_network", "get", "anchor", "{}")
		seedChainThroughProducer(t, ctx, tx)

		// ПРЕДПОСЫЛКА, названная числом: у сети РОВНО ОДНО ребро. Проба стоит
		// именно на той форме, которую производит дерево, и это проверяется,
		// а не подразумевается.
		var links int
		if err := tx.QueryRow(ctx,
			`SELECT count(*)::int FROM kacho_iam.resource_parent_edge
			  WHERE object_type = 'vpc_network' AND object_id = 'net-1'`).Scan(&links); err != nil {
			t.Fatalf("перепись рёбер объекта: %v", err)
		}
		if links != 1 {
			t.Fatalf("у сети %d рёбер, ожидалось РОВНО ОДНО: фикстура положила форму, которой "+
				"производители не производят, и проба судила бы не то состояние", links)
		}
		t.Logf("предпосылка: у объекта арендатора %d ребро — цепь до корня собирается ОБХОДОМ", links)

		// (1) Выдача на АККАУНТ — два звена вверх.
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.access_bindings
			   (id, subject_type, subject_id, role_id, resource_type, resource_id, status)
			 VALUES ('acb-acc', 'user', 'usr-1', 'rol-acc', 'account', 'acc-1', 'ACTIVE')`)
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.access_binding_subjects (binding_id, subject_type, subject_id)
			 VALUES ('acb-acc', 'user', 'usr-1')`)

		got, _, err := relverdict.Ask(ctx, tx, relverdict.Query{
			Subject: "user:usr-1", ObjectType: "vpc_network", ObjectID: "net-1", Relation: "v_get",
		})
		if err != nil {
			t.Fatalf("вопрос о выдаче на аккаунт: %v", err)
		}
		if got != relverdict.Allow {
			t.Errorf("выдача на АККАУНТ не достала до объекта арендатора: %s. Область вердикта "+
				"схлопнулась до «объект + проект», и отказ неотличим от честного", got)
		}

		// (2) Прямой факт администратора облака на КЛАСТЕРЕ — три звена вверх.
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.users (id, external_id, email, account_id)
			 VALUES ('usr-admin', 'ext-admin', 'admin@kacho.local', 'acc-1')`)
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.relation_fact (object_type, object_id, relation, subject)
			 VALUES ('cluster', 'cluster_kacho_root', 'system_admin', 'user:usr-admin')`)

		admin, _, err := relverdict.Ask(ctx, tx, relverdict.Query{
			Subject: "user:usr-admin", ObjectType: "vpc_network", ObjectID: "net-1", Relation: "v_get",
		})
		if err != nil {
			t.Fatalf("вопрос администратора облака: %v", err)
		}
		if admin != relverdict.Allow {
			t.Errorf("администратор облака не достал до объекта арендатора: %s. Это аварийный "+
				"путь §«Три уровня супер-доступа»: он обязан работать независимо от состояния "+
				"конвейеров материализации", admin)
		}

		// ОТРИЦАНИЕ рядом с положительным: чужой аккаунт по-прежнему не достаёт.
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.accounts (id, name, owner_user_id) VALUES ('acc-9', 'foreign', 'usr-1')`)
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.users (id, external_id, email, account_id)
			 VALUES ('usr-out', 'ext-out', 'out@kacho.local', 'acc-1')`)
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.access_bindings
			   (id, subject_type, subject_id, role_id, resource_type, resource_id, status)
			 VALUES ('acb-foreign', 'user', 'usr-out', 'rol-acc', 'account', 'acc-9', 'ACTIVE')`)
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.access_binding_subjects (binding_id, subject_type, subject_id)
			 VALUES ('acb-foreign', 'user', 'usr-out')`)
		outsider, _, err := relverdict.Ask(ctx, tx, relverdict.Query{
			Subject: "user:usr-out", ObjectType: "vpc_network", ObjectID: "net-1", Relation: "v_get",
		})
		if err != nil {
			t.Fatalf("вопрос постороннего: %v", err)
		}
		if outsider != relverdict.Deny {
			t.Errorf("выдача на ЧУЖОЙ аккаунт достала до объекта: %s — положительные утверждения "+
				"выше зеленели бы на форме, которая разрешает всем", outsider)
		}
	})
}

// TestScopeStopsAtTheProjectOnTheChainTheTreeActuallyProduces — вторая половина
// Б1, закреплённая КАК ЕСТЬ.
//
// # Зачем закреплять то, что неверно
//
// Рёбра появляются единственным путём — регистрацией ресурса у владельца прав, —
// и зовут её пять сервисов-соседей ТОЛЬКО для своих ресурсов. Объектов типа
// project и account не регистрирует никто, включая сам iam. Значит рёбер
// project → account и account → cluster в базе нет, и обход, дойдя до проекта,
// останавливается: выдача на аккаунт и факт администратора облака на кластере
// этой формой НЕ НАХОДЯТСЯ.
//
// Пока это не закреплено, «не работает» неотличимо от «работает»: соседняя проба
// досеивает недостающие звенья и потому зелена, а корпус вопросов сравнителя
// таких вопросов не задаёт. Проба фиксирует НЫНЕШНИЙ ответ, чтобы:
//
//	· появление производителя было ЗАМЕЧЕНО — она покраснеет, и это сигнал
//	  «предмет задачи о недостающих рёбрах закрыт», а не поломка;
//	· переключение решения о доступе на эту форму нельзя было сделать молча.
//
// # Почему это не боевая регрессия
//
// Реляционный вердикт провязан единственным местом — теневым сравнителем;
// решение о доступе принимает другая форма, берущая аккаунт резолвом на границе
// чтения, а не ребром. Опасность отложенная, и потому предмет заведён задачей, а
// не чинится здесь: kacho#740, там же предикат снятия и три рассматриваемых
// решения.
func TestScopeStopsAtTheProjectOnTheChainTheTreeActuallyProduces(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	withTx(t, func(ctx context.Context, tx pgx.Tx) {
		seedTenant(t, ctx, tx)
		seedRole(t, ctx, tx, "rol-any", "vpc_network", "get", "anchor", "{}")

		// ТОЛЬКО то звено, которое дерево действительно производит: vpc шлёт
		// ParentChain(nil, projectID, "") — один проект и ничего больше.
		base := time.Now().UTC().Truncate(time.Microsecond)
		if _, err := resource_mirror.UpsertTx(ctx, tx, resource_mirror.Row{
			ObjectType:      catalogFormOf(t, "vpc_network"),
			ObjectID:        "net-1",
			ParentProjectID: "prj-1",
			ParentChain:     ownerregister.ParentChain(nil, "prj-1", ""),
			SourceVersion:   base,
		}); err != nil {
			t.Fatalf("регистрация сети через производителя: %v", err)
		}

		var edges int
		if err := tx.QueryRow(ctx,
			`SELECT count(*)::int FROM kacho_iam.resource_parent_edge`).Scan(&edges); err != nil {
			t.Fatalf("перепись рёбер: %v", err)
		}
		if edges != 1 {
			t.Fatalf("рёбер в базе %d, ожидалось РОВНО ОДНО: фикстура положила больше, чем "+
				"производит дерево, и проба судила бы не то состояние", edges)
		}
		t.Logf("на цепи, которую производит дерево, рёбер всего %d (сеть → проект)", edges)

		ask := func(scopeType, scopeID, bindingID string) relverdict.Verdict {
			t.Helper()
			exec(t, ctx, tx,
				`INSERT INTO kacho_iam.access_bindings
				   (id, subject_type, subject_id, role_id, resource_type, resource_id, status)
				 VALUES ($1, 'user', 'usr-1', 'rol-any', $2, $3, 'ACTIVE')`,
				bindingID, scopeType, scopeID)
			exec(t, ctx, tx,
				`INSERT INTO kacho_iam.access_binding_subjects (binding_id, subject_type, subject_id)
				 VALUES ($1, 'user', 'usr-1')`, bindingID)
			got, _, err := relverdict.Ask(ctx, tx, relverdict.Query{
				Subject: "user:usr-1", ObjectType: "vpc_network", ObjectID: "net-1", Relation: "v_get",
			})
			if err != nil {
				t.Fatalf("вопрос при выдаче на %s: %v", scopeType, err)
			}
			exec(t, ctx, tx, `DELETE FROM kacho_iam.access_bindings WHERE id = $1`, bindingID)
			return got
		}

		// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ПЕРВЫМ: до проекта обход доходит. Без него
		// отказы ниже были бы неотличимы от «форма не работает вовсе».
		if got := ask("project", "prj-1", "acb-prj"); got != relverdict.Allow {
			t.Fatalf("выдача на ПРОЕКТ не достала до объекта: %s. Контроль провален, и отказы "+
				"ниже ничего не говорят о высоте цепи", got)
		}
		t.Log("контроль: выдача на проект — allow, обход до непосредственного предка работает")

		// А выше — не достаёт, и это закрепляется КАК ЕСТЬ.
		if got := ask("account", "acc-1", "acb-acc"); got != relverdict.Deny {
			t.Errorf("выдача на АККАУНТ дала %s. Если это allow — значит недостающие рёбра "+
				"кто-то начал писать: предмет задачи о производителях закрыт, и эту пробу "+
				"надо снять вместе с ним, а не чинить", got)
		}
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.users (id, external_id, email, account_id)
			 VALUES ('usr-admin', 'ext-admin', 'admin@kacho.local', 'acc-1')`)
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.relation_fact (object_type, object_id, relation, subject)
			 VALUES ('cluster', 'cluster_kacho_root', 'system_admin', 'user:usr-admin')`)
		admin, _, err := relverdict.Ask(ctx, tx, relverdict.Query{
			Subject: "user:usr-admin", ObjectType: "vpc_network", ObjectID: "net-1", Relation: "v_get",
		})
		if err != nil {
			t.Fatalf("вопрос администратора облака: %v", err)
		}
		if admin != relverdict.Deny {
			t.Errorf("администратор облака дал %s — см. выше: это сигнал о закрытии предмета, "+
				"а не о поломке", admin)
		}
		t.Log("закреплено КАК ЕСТЬ: на цепи дерева выдача на аккаунт и факт на кластере " +
			"этой формой не находятся — рёбер выше проекта не пишет никто")
	})
}

// TestAllFourEntryPointsAgreeOnAGrantAboveTheImmediateParent — В-3.
//
// # Дыра в покрытии, а не живой дефект
//
// Гейт формы области сверяет ТЕКСТ рекурсивного шага, приведённый к виду без
// номеров параметров. После нормализации `$7`, `$5` и `$3` неразличимы, поэтому
// подмена привязки — предел обхода вместо размера страницы — гейту невидима, а в
// `list.go` эти два параметра стоят рядом. Текстовый гейт этого класса не ловит
// by construction: он сверяет форму, а привязка — свойство вызова.
//
// Ловит его поведение, и до сих пор его никто не спрашивал: проб, читающих
// перечисление, субъектов и основания на выдаче ВЫШЕ непосредственного предка, в
// дереве не было ни одной. Все четыре точки входа сегодня сходятся — эта проба
// закрепляет схождение, чтобы расхождение стало видимым.
//
// Цепь досеивается до аккаунта намеренно: предмет здесь — согласие четырёх
// форм между собой, а не высота, до которой доходит производитель (её
// закрепляет соседняя проба).
func TestAllFourEntryPointsAgreeOnAGrantAboveTheImmediateParent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	withTx(t, func(ctx context.Context, tx pgx.Tx) {
		seedTenant(t, ctx, tx)
		seedRole(t, ctx, tx, "rol-acc", "vpc_network", "get", "anchor", "{}")
		seedChainThroughProducer(t, ctx, tx)

		// Выдача на АККАУНТ — два звена вверх от объекта.
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.access_bindings
			   (id, subject_type, subject_id, role_id, resource_type, resource_id, status)
			 VALUES ('acb-acc', 'user', 'usr-1', 'rol-acc', 'account', 'acc-1', 'ACTIVE')`)
		exec(t, ctx, tx,
			`INSERT INTO kacho_iam.access_binding_subjects (binding_id, subject_type, subject_id)
			 VALUES ('acb-acc', 'user', 'usr-1')`)

		// (1) точечный вердикт
		verdict, _, err := relverdict.Ask(ctx, tx, relverdict.Query{
			Subject: "user:usr-1", ObjectType: "vpc_network", ObjectID: "net-1", Relation: "v_get",
		})
		if err != nil {
			t.Fatalf("точечный вердикт: %v", err)
		}

		// (2) перечисление объектов
		ids, _, err := relverdict.List(ctx, tx, relverdict.ListQuery{
			Subject: "user:usr-1", ObjectType: "vpc_network", Relation: "v_get", Limit: 50,
		})
		if err != nil {
			t.Fatalf("перечисление: %v", err)
		}
		listed := false
		for _, id := range ids {
			if id == "net-1" {
				listed = true
			}
		}

		// (3) перечисление субъектов
		subs, _, err := relverdict.Subjects(ctx, tx, relverdict.SubjectsQuery{
			ObjectType: "vpc_network", ObjectID: "net-1", Relation: "v_get", Limit: 50,
		})
		if err != nil {
			t.Fatalf("перечисление субъектов: %v", err)
		}
		named := false
		for _, s := range subs {
			if s == "user:usr-1" {
				named = true
			}
		}

		// (4) разбор оснований
		grounds, err := relverdict.Expand(ctx, tx, "vpc_network", "net-1", "v_get")
		if err != nil {
			t.Fatalf("разбор оснований: %v", err)
		}

		t.Logf("выдача на аккаунт (два звена вверх): вердикт %s · в перечислении %v · "+
			"среди субъектов %v · оснований %d", verdict, listed, named, len(grounds))

		// СОГЛАСИЕ ЧЕТЫРЁХ. Расхождение здесь означает, что одна из форм
		// поднимается по цепи не на ту высоту, — а по какой причине (иной
		// предикат, иная привязка параметра), скажет уже разбор.
		if verdict != relverdict.Allow {
			t.Errorf("точечный вердикт %s при выдаче на аккаунт и полной цепи", verdict)
		}
		if !listed {
			t.Errorf("перечисление не вернуло объект, который точечный вердикт разрешает: "+
				"формы отвечают на разной высоте цепи (вернулось %v)", ids)
		}
		if !named {
			t.Errorf("перечисление субъектов не назвало обладателя права: %v", subs)
		}
		if len(grounds) == 0 {
			t.Error("разбор не назвал ни одного основания там, где право есть: " +
				"обратный вопрос молчит про источник, по которому доступ реально выдан")
		}
	})
}
