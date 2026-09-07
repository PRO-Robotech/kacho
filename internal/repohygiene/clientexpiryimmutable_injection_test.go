// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientexpiryimmutable_injection_test.go — доказательство того, что
// TestClientExpiryIsNeverUpdated СПОСОБЕН упасть, и падает он на существе.
//
// Инъекция гоняет ТЕ ЖЕ функции разбора (ScanSQLUpdates, SQLCreateTableBody),
// что и гейт.
//
// Вторая сторона пары обязательна и без неё гейт был бы вреден: правка ДРУГОГО
// столбца этих же таблиц законна и происходит на каждом предъявлении. Гейт,
// считающий всякую правку таблицы, запретил бы её вести.
package repohygiene

import (
	"strings"
	"testing"
)

// expiryInjectedUpdate — правка срока после создания.
//
// Форма подобрана так, чтобы обойти разбор по подстроке: столбец назван не
// первым, оператор записан в несколько строк, а рядом стоит комментарий,
// называющий тот же столбец, — предикат по тексту краснел бы на собственном
// объяснении.
const expiryInjectedUpdate = `package pg

import "context"

type UserOAuthClientRepo struct{ pool *pgxpool.Pool }

// Продлить срок клиента: expires_at сдвигается вперёд.
func (r *UserOAuthClientRepo) Extend(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		` + "`" + `UPDATE kaname.user_oauth_clients
		   SET last_used_at = now(), expires_at = $2
		 WHERE id = $1` + "`" + `, id, nil)
	return err
}
`

// expiryInjectedLawful — законные соседи, каждый со своим способом обмануть
// разбор:
//
//   - правка ДРУГОГО столбца тех же таблиц: она законна и идёт на каждом
//     предъявлении;
//   - создание, НАЗНАЧАЮЩЕЕ срок: неизменяемость оно не нарушает — срок здесь
//     появляется, а не двигается;
//   - чтение срока;
//   - правка срока у ЧУЖОЙ таблицы: предмет решения §2.10 — клиенты, способные
//     к утверждению, и чужая таблица к нему не относится;
//   - имя столбца в условии `WHERE`, а не в списке правки: сравнение правкой не
//     является;
//   - имя столбца в списке ВОЗВРАТА: `RETURNING` читает, а не пишет.
const expiryInjectedLawful = `package pg

import "context"

type UserOAuthClientRepo struct{ pool *pgxpool.Pool }

func (r *UserOAuthClientRepo) TouchLastUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		` + "`" + `UPDATE user_oauth_clients SET last_used_at = $2 WHERE id = $1` + "`" + `, id, nil)
	return err
}

func (r *UserOAuthClientRepo) Create(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		` + "`" + `INSERT INTO user_oauth_clients (id, expires_at) VALUES ($1, $2)` + "`" + `, id, nil)
	return err
}

func (r *UserOAuthClientRepo) Get(ctx context.Context, id string) error {
	return r.pool.QueryRow(ctx,
		` + "`" + `SELECT id, expires_at FROM user_oauth_clients WHERE id = $1` + "`" + `, id).Scan()
}

func (r *UserOAuthClientRepo) Sweep(ctx context.Context) error {
	_, err := r.pool.Exec(ctx,
		` + "`" + `UPDATE some_other_table SET expires_at = now() WHERE id = $1` + "`" + `)
	return err
}

func (r *UserOAuthClientRepo) Revoke(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		` + "`" + `UPDATE user_oauth_clients SET revoked_at = now()
		 WHERE id = $1 AND expires_at > now() RETURNING expires_at` + "`" + `, id)
	return err
}
`

// TestExpiryScannerFindsAnUpdateOfTheColumn — сторона (а): правка срока
// становится находкой, и находка несёт координату.
func TestExpiryScannerFindsAnUpdateOfTheColumn(t *testing.T) {
	updates, census, err := ScanSQLUpdates(
		"synthetic/pg/user_oauth_clients_repos.go", []byte(expiryInjectedUpdate), clientExpiryTables)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.SQLLiterals == 0 {
		t.Fatalf("осмотрено ноль литералов SQL — разбирается не то дерево")
	}
	if len(updates) != 1 {
		t.Fatalf("правок найдено %d, ожидалась 1: %+v", len(updates), updates)
	}
	u := updates[0]
	if u.File == "" || u.Line == 0 {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", u)
	}
	if u.Func != "UserOAuthClientRepo.Extend" {
		t.Errorf("находка не называет функцию: %+v", u)
	}
	if u.Table != "user_oauth_clients" {
		t.Errorf("находка не сняла имя схемы с таблицы: %+v", u)
	}
	// Столбец назван НЕ ПЕРВЫМ намеренно: разбор, читающий одно присваивание,
	// эту правку пропустил бы.
	found := false
	for _, c := range u.Columns {
		if c == clientExpiryColumn {
			found = true
		}
	}
	if !found {
		t.Fatalf("столбец %q не найден в списке правки %v — гейт на этом дефекте остался "+
			"бы зелёным", clientExpiryColumn, u.Columns)
	}
	if len(u.Columns) != 2 {
		t.Errorf("столбцов разобрано %d, ожидалось 2: %v", len(u.Columns), u.Columns)
	}
}

// TestExpiryScannerIsSilentOnLawfulStatements — сторона (б).
func TestExpiryScannerIsSilentOnLawfulStatements(t *testing.T) {
	updates, census, err := ScanSQLUpdates(
		"synthetic/pg/user_oauth_clients_repos.go", []byte(expiryInjectedLawful), clientExpiryTables)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.SQLLiterals < 5 {
		t.Fatalf("литералов SQL осмотрено %d — разбирается не то дерево", census.SQLLiterals)
	}
	// Правок стережённых таблиц ДВЕ: правка другого столбца и правка с именем
	// столбца в условии. Правка чужой таблицы в счёт не идёт.
	if len(updates) != 2 {
		t.Fatalf("правок стережённых таблиц найдено %d, ожидалось 2: %+v.\n\nЛибо создание "+
			"или чтение принято за правку — тогда гейт запрещает срок заводить и читать; "+
			"либо чужая таблица принята за нашу.", len(updates), updates)
	}
	for _, u := range updates {
		for _, c := range u.Columns {
			if c != clientExpiryColumn {
				continue
			}
			t.Fatalf("законный оператор объявлен правкой срока (%+v).\n\nЛибо имя столбца в "+
				"условии WHERE прочитано как присваивание, либо список ВОЗВРАТА принят за "+
				"список правки: и то и другое читает, а не пишет.", u)
		}
	}
	// Молчание обязано быть взвешиваемым: разбор увидел операторы и признал их
	// законными, а не «не заметил».
	if census.Updates != 2 {
		t.Fatalf("перепись правок насчитала %d при двух найденных — счётчик и находки "+
			"разошлись, и молчание нечем взвесить", census.Updates)
	}
}

// TestExpiryPremiseReadsTheColumnOfTheTable — предпосылка гейта: столбец у
// таблицы действительно объявлен.
//
// Без этой пробы гейт мог бы стеречь неизменяемость столбца, которого нет, и
// молчать по построению.
func TestExpiryPremiseReadsTheColumnOfTheTable(t *testing.T) {
	const withColumn = `-- +goose Up
CREATE TABLE kaname.user_oauth_clients (
    id text NOT NULL,
    expires_at timestamp with time zone,
    CONSTRAINT user_oauth_clients_pkey PRIMARY KEY (id)
);
-- +goose Down
DROP TABLE kaname.user_oauth_clients;
`
	body := SQLCreateTableBody(withColumn, "user_oauth_clients")
	if body == "" {
		t.Fatal("объявление таблицы не найдено — предпосылка гейта не читается вовсе")
	}
	if !strings.Contains(body, clientExpiryColumn) {
		t.Fatalf("столбец %q не найден в теле объявления: %q", clientExpiryColumn, body)
	}

	const withoutColumn = `-- +goose Up
CREATE TABLE kaname.user_oauth_clients (
    id text NOT NULL,
    CONSTRAINT user_oauth_clients_pkey PRIMARY KEY (id)
);
-- +goose Down
CREATE TABLE kaname.user_oauth_clients (id text, expires_at timestamp with time zone);
`
	body2 := SQLCreateTableBody(withoutColumn, "user_oauth_clients")
	if body2 == "" {
		t.Fatal("объявление таблицы без столбца не найдено — предпосылка не различает " +
			"«таблицы нет» и «столбца нет»")
	}
	if strings.Contains(body2, clientExpiryColumn) {
		t.Fatalf("столбец найден там, где его нет: прочитана секция ОТКАТА, которая накатом "+
			"не применяется — тогда предпосылка судит схему по тому, чего в ней нет: %q", body2)
	}

	// Чужая таблица телом нашей не становится.
	if got := SQLCreateTableBody(withColumn, "service_account_oauth_clients"); got != "" {
		t.Fatalf("тело чужой таблицы выдано за наше: %q", got)
	}
}
