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
// дерева, каждым звеном по отдельности: ровно так её и набирают регистрации.
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
		// Проект: своё собственное ребро на аккаунт.
		{catalogFormOf(t, "project"), "prj-1", ownerregister.ParentChain(nil, "", "acc-1"), "", "acc-1"},
		// Аккаунт: своё собственное ребро на кластер. Его не шлёт сегодня никто,
		// и это отдельная находка; здесь оно кладётся, чтобы проба судила ДОСТИЖИМОСТЬ,
		// а не отсутствие звена.
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
