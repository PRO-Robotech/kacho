// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// membership_read_integration_test.go — чтение членства на аккаунт-скоупных
// путях (IAM-ID-2, стадия S1).
//
// Предмет проб — СУЖЕНИЕ ОТБОРА, а не форма ответа: аккаунт обязан стоять в
// условии запроса, а не в проверке после чтения. Поэтому у каждого отрицания
// здесь стоит положительный контроль, и оба утверждаются В ОДНОМ ПРОГОНЕ И НА
// ОДНИХ ДАННЫХ — человек состоит и в `A`, и в `B` одновременно. Иначе «строк
// `B` не выбрал» доказывалось бы отсутствием `B`, а не сужением отбора, и
// зеленело бы у чтения, которое не выбирает ничего.

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	iamerr "github.com/PRO-Robotech/kacho/services/iam/internal/errors"
	repomembership "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/membership"
	kachopg "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg"
)

// seedMembership кладёт строку членства напрямую — тем же идентификатором,
// который выводит неизменяемая функция схемы. Прямая вставка выбрана намеренно:
// предмет этих проб — ЧТЕНИЕ, и оно обязано отвечать про строку, как бы та ни
// появилась (писателей у неё больше одного).
func seedMembershipRow(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	userID domain.UserID, accountID domain.AccountID,
	state domain.MembershipState, invitedBy domain.UserID, createdAt time.Time,
) domain.MembershipID {
	t.Helper()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO memberships (id, user_id, account_id, state, invited_by, created_at, updated_at)
		VALUES (kacho_iam.membership_mirror_id($1, $2), $1, $2, $3, NULLIF($4, ''), $5, $5)
		ON CONFLICT (user_id, account_id) DO UPDATE
		   SET state = EXCLUDED.state, invited_by = EXCLUDED.invited_by,
		       created_at = EXCLUDED.created_at, updated_at = EXCLUDED.updated_at
		RETURNING id`,
		string(userID), string(accountID), string(state), string(invitedBy), createdAt,
	).Scan(&id)
	require.NoError(t, err, "seed membership")
	return domain.MembershipID(id)
}

func membershipReaderOn(t *testing.T, ctx context.Context, repo *kachopg.Repository) (repomembership.ReaderIface, func()) {
	t.Helper()
	s, err := repo.MembershipReader(ctx)
	require.NoError(t, err)
	return s.Memberships(), func() { s.Close(ctx) }
}

// TestMembership_IAMID2_14_AccountScopeIsInTheQueryNotAfterTheRead — обе
// половины IAM-ID-2-14 на одних данных: человек состоит И в `A`, И в `B`.
func TestMembership_IAMID2_14_AccountScopeIsInTheQueryNotAfterTheRead(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.New(pool, nil)

	owner := mustSeedUser(t, ctx, pool, "mbr14own")
	person := mustSeedUser(t, ctx, pool, "mbr14per")
	accA := seedAccount(t, ctx, repo, "mbr14-a", owner)
	accB := seedAccount(t, ctx, repo, "mbr14-b", owner)

	base := time.Now().UTC().Truncate(time.Second)
	idA := seedMembershipRow(t, ctx, pool, person, accA.ID, domain.MembershipStateActive, owner, base)
	idB := seedMembershipRow(t, ctx, pool, person, accB.ID, domain.MembershipStateActive, owner, base.Add(time.Second))

	rd, done := membershipReaderOn(t, ctx, repo)
	defer done()

	rows, next, err := rd.List(ctx, repomembership.ListFilter{AccountID: accA.ID})
	require.NoError(t, err)
	require.Empty(t, next, "одна строка в аккаунте — токена продолжения быть не должно")

	// ПОЛОЖИТЕЛЬНАЯ половина: отбор выбрал РОВНО строку `A`, с её полями.
	require.Len(t, rows, 1, "чтение по пути аккаунта A обязано выбрать строку A — "+
		"без этой половины «строк B не выбрал» истинно и у отбора, который всегда пуст")
	require.Equal(t, idA, rows[0].ID)
	require.Equal(t, accA.ID, rows[0].AccountID)
	require.Equal(t, person, rows[0].UserID)
	require.Equal(t, domain.MembershipStateActive, rows[0].State)
	require.Equal(t, owner, rows[0].InvitedBy)
	require.Equal(t, domain.AccountName("mbr14-a"), rows[0].AccountName,
		"имя аккаунта — зеркало, читается соединением в той же БД")

	// ОТРИЦАТЕЛЬНАЯ половина: ни одна строка `B` не выбрана, при том что она
	// существует и принадлежит ТОМУ ЖЕ человеку.
	for _, m := range rows {
		require.NotEqual(t, idB, m.ID, "чтение по пути аккаунта A выбрало строку аккаунта B")
		require.NotEqual(t, accB.ID, m.AccountID)
	}

	// Зеркальная сторона: тот же вызов по пути `B` выбирает ровно строку `B`.
	rowsB, _, err := rd.List(ctx, repomembership.ListFilter{AccountID: accB.ID})
	require.NoError(t, err)
	require.Len(t, rowsB, 1)
	require.Equal(t, idB, rowsB[0].ID)
}

// TestMembership_IAMID2_13_ForeignMembershipIsIndistinguishableFromAbsent —
// одиночное чтение: членство ЧУЖОГО аккаунта и несуществующее дают один сентинел.
func TestMembership_IAMID2_13_ForeignMembershipIsIndistinguishableFromAbsent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.New(pool, nil)

	owner := mustSeedUser(t, ctx, pool, "mbr13own")
	person := mustSeedUser(t, ctx, pool, "mbr13per")
	accA := seedAccount(t, ctx, repo, "mbr13-a", owner)
	accB := seedAccount(t, ctx, repo, "mbr13-b", owner)

	base := time.Now().UTC().Truncate(time.Second)
	idA := seedMembershipRow(t, ctx, pool, person, accA.ID, domain.MembershipStateActive, "", base)
	idB := seedMembershipRow(t, ctx, pool, person, accB.ID, domain.MembershipStateActive, "", base)

	rd, done := membershipReaderOn(t, ctx, repo)
	defer done()

	// Чужое членство по пути `A` — отсутствие.
	_, errForeign := rd.Get(ctx, accA.ID, idB)
	require.True(t, stderrors.Is(errForeign, iamerr.ErrNotFound),
		"членство аккаунта B, прочитанное по пути аккаунта A, обязано быть отсутствием: %v", errForeign)

	// Несуществующее — то же отсутствие. Идентификатор well-formed и не
	// резолвится ни во что.
	const absent = "mbr-00000000000000000"
	_, errAbsent := rd.Get(ctx, accA.ID, domain.MembershipID(absent))
	require.True(t, stderrors.Is(errAbsent, iamerr.ErrNotFound),
		"несуществующее членство обязано быть отсутствием: %v", errAbsent)

	// ПОЛОЖИТЕЛЬНЫЙ контроль: своё членство читается. Без него «оба отсутствуют»
	// истинно и у чтения, которое не находит ничего никогда.
	got, err := rd.Get(ctx, accA.ID, idA)
	require.NoError(t, err)
	require.Equal(t, idA, got.ID)
	require.Equal(t, accA.ID, got.AccountID)

	// ВТОРОЙ положительный контроль: та же строка `B` по СВОЕМУ пути читается —
	// значит она существует, и её недоступность выше была сужением отбора, а не
	// отсутствием данных.
	gotB, err := rd.Get(ctx, accB.ID, idB)
	require.NoError(t, err)
	require.Equal(t, idB, gotB.ID)
}

// TestMembership_IAMID2_05_CursorNeitherSkipsNorDuplicates — курсор обходит
// страницу за страницей, ни одной строки не теряя и ни одной не повторяя.
func TestMembership_IAMID2_05_CursorNeitherSkipsNorDuplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.New(pool, nil)

	owner := mustSeedUser(t, ctx, pool, "mbr05own")
	accA := seedAccount(t, ctx, repo, "mbr05-a", owner)
	accB := seedAccount(t, ctx, repo, "mbr05-b", owner)

	base := time.Now().UTC().Truncate(time.Second)
	want := map[domain.MembershipID]bool{}
	const total = 7
	for i := 0; i < total; i++ {
		p := mustSeedUser(t, ctx, pool, fmt.Sprintf("mbr05p%d", i))
		want[seedMembershipRow(t, ctx, pool, p, accA.ID, domain.MembershipStateActive, "", base.Add(time.Duration(i)*time.Second))] = true
		// Шум в СОСЕДНЕМ аккаунте: обход обязан его не заметить.
		seedMembershipRow(t, ctx, pool, p, accB.ID, domain.MembershipStateActive, "", base.Add(time.Duration(i)*time.Second))
	}

	rd, done := membershipReaderOn(t, ctx, repo)
	defer done()

	seen := map[domain.MembershipID]int{}
	token := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 20, "обход не сходится — токен не продвигается")
		rows, next, err := rd.List(ctx, repomembership.ListFilter{
			AccountID: accA.ID, PageSize: 3, PageToken: token,
		})
		require.NoError(t, err)
		for _, m := range rows {
			seen[m.ID]++
			require.Equal(t, accA.ID, m.AccountID, "в страницу попала строка чужого аккаунта")
		}
		if next == "" {
			break
		}
		token = next
	}
	require.Len(t, seen, total, "обход потерял либо добрал строки")
	for id := range want {
		require.Equal(t, 1, seen[id], "строка %s встретилась не ровно один раз", id)
	}
}

// TestMembership_IAMID2_04_FilterTermAndOperator — белый список терма И
// объявленный оператор. Оба отрицания идут с положительным контролем.
func TestMembership_IAMID2_04_FilterTermAndOperator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.New(pool, nil)

	owner := mustSeedUser(t, ctx, pool, "mbr04own")
	person := mustSeedUser(t, ctx, pool, "mbr04per")
	other := mustSeedUser(t, ctx, pool, "mbr04oth")
	accA := seedAccount(t, ctx, repo, "mbr04-a", owner)
	base := time.Now().UTC().Truncate(time.Second)
	idPerson := seedMembershipRow(t, ctx, pool, person, accA.ID, domain.MembershipStateActive, "", base)
	seedMembershipRow(t, ctx, pool, other, accA.ID, domain.MembershipStateActive, "", base.Add(time.Second))

	rd, done := membershipReaderOn(t, ctx, repo)
	defer done()

	// ПОЛОЖИТЕЛЬНЫЙ контроль: объявленный терм сужает, а не игнорируется.
	rows, _, err := rd.List(ctx, repomembership.ListFilter{
		AccountID: accA.ID, Filter: `userId="` + string(person) + `"`,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "терм userId обязан сужать перечень, а не приниматься молча")
	require.Equal(t, idPerson, rows[0].ID)

	// Терм ВНЕ белого списка — отказ с именем поля, а не полная страница.
	_, _, err = rd.List(ctx, repomembership.ListFilter{
		AccountID: accA.ID, Filter: `email="p@example.test"`,
	})
	require.True(t, stderrors.Is(err, iamerr.ErrInvalidArg), "терм вне белого списка: %v", err)
	require.Contains(t, err.Error(), "email", "отказ обязан называть поле")

	// Оператор ВНЕ объявленного — отказ. Грамматика подстрочный поиск разбирает,
	// эта поверхность его не заводила: свести его молча к равенству значило бы
	// ответить уверенно и неверно.
	_, _, err = rd.List(ctx, repomembership.ListFilter{
		AccountID: accA.ID, Filter: `userId CONTAINS "usr"`,
	})
	require.True(t, stderrors.Is(err, iamerr.ErrInvalidArg),
		"подстрочный поиск по userId обязан отвергаться явно, а не сводиться к равенству: %v", err)
}

// TestMembership_IAMID2_02_StateIsCarriedNotConstant — состояние читается из
// строки и РАЗЛИЧАЕТ, а не отдаёт константу.
func TestMembership_IAMID2_02_StateIsCarriedNotConstant(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.New(pool, nil)

	owner := mustSeedUser(t, ctx, pool, "mbr02own")
	invited := mustSeedUser(t, ctx, pool, "mbr02inv")
	active := mustSeedUser(t, ctx, pool, "mbr02act")
	accA := seedAccount(t, ctx, repo, "mbr02-a", owner)
	base := time.Now().UTC().Truncate(time.Second)
	idPending := seedMembershipRow(t, ctx, pool, invited, accA.ID, domain.MembershipStatePending, owner, base)
	idActive := seedMembershipRow(t, ctx, pool, active, accA.ID, domain.MembershipStateActive, "", base.Add(time.Second))

	rd, done := membershipReaderOn(t, ctx, repo)
	defer done()

	byID := map[domain.MembershipID]domain.Membership{}
	rows, _, err := rd.List(ctx, repomembership.ListFilter{AccountID: accA.ID})
	require.NoError(t, err)
	for _, m := range rows {
		byID[m.ID] = m
	}
	require.Equal(t, domain.MembershipStatePending, byID[idPending].State)
	require.Equal(t, owner, byID[idPending].InvitedBy, "след приглашения обязан читаться")
	// ПОЛОЖИТЕЛЬНЫЙ контроль различения: у соседа состояние ДРУГОЕ, и
	// пригласивший пуст — значит поле различает, а не рисуется всегда.
	require.Equal(t, domain.MembershipStateActive, byID[idActive].State)
	require.Empty(t, byID[idActive].InvitedBy)
}

// TestMembership_PageSizeIsRejectedNotClamped — размер вне [0..1000]
// ОТВЕРГАЕТСЯ репозиторием, который остаётся авторитетным.
func TestMembership_PageSizeIsRejectedNotClamped(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires Docker")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.New(pool, nil)

	owner := mustSeedUser(t, ctx, pool, "mbrpsown")
	accA := seedAccount(t, ctx, repo, "mbrps-a", owner)
	seedMembershipRow(t, ctx, pool, owner, accA.ID, domain.MembershipStateActive, "",
		time.Now().UTC().Truncate(time.Second))

	rd, done := membershipReaderOn(t, ctx, repo)
	defer done()

	_, _, err = rd.List(ctx, repomembership.ListFilter{AccountID: accA.ID, PageSize: 5000})
	require.True(t, stderrors.Is(err, iamerr.ErrInvalidArg), "page_size 5000: %v", err)

	_, _, err = rd.List(ctx, repomembership.ListFilter{AccountID: accA.ID, PageToken: "не-курсор"})
	require.True(t, stderrors.Is(err, iamerr.ErrInvalidArg), "негодный курсор: %v", err)

	// ПОЛОЖИТЕЛЬНЫЙ контроль: законная пагинация проходит по ТОМУ ЖЕ пути.
	rows, _, err := rd.List(ctx, repomembership.ListFilter{AccountID: accA.ID, PageSize: 10})
	require.NoError(t, err)
	require.NotEmpty(t, rows, "законная страница обязана проходить — иначе отрицания "+
		"зеленеют на чтении, отвергающем всякий вход")
}
