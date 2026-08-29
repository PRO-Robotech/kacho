// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// basic_credential_liveness_integration_test.go — ЖИВОСТЬ, СПРОШЕННАЯ ПО
// ИДЕНТИФИКАТОРУ, СОВПАДАЕТ С ЖИВОСТЬЮ, СПРОШЕННОЙ ПО СЕКРЕТУ (задача #1450).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРОБА СРАВНИВАЕТ ДВЕ ПОЛОСЫ, А НЕ ПРОВЕРЯЕТ КАЖДУЮ ОТДЕЛЬНО
//
// Полос теперь две: предъявитель спрашивает секретом, открытое соединение —
// идентификатором. Проба каждой по отдельности требует знать, каким ответ
// ДОЛЖЕН быть, — а это и есть спорный вопрос. Сравнение полос спрашивает
// другое: решал ли кто-нибудь, что они различаются. На это ответ есть всегда.
//
// Расхождение здесь стоит дорого и в обе стороны: полоса идентификатора,
// оказавшись СТРОЖЕ, закрывала бы живые соединения; оказавшись МЯГЧЕ — не
// закрывала бы отозванные, то есть возвращала бы ровно тот дефект, ради
// которого вопрос и заведён.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕПИСЬ ПЕЧАТАЕТСЯ
//
// «Полосы согласны» из нуля осмотренных состояний неотличимо от «полосы
// согласны» из шести. Число состояний печатается, и пустой перечень — отказ.

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/credsecret"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg"
)

// TestBCL1450_LivenessByIdAgreesWithLivenessBySecretOnEveryState — обе полосы
// об одном предмете, сверенные между собой на КАЖДОМ состоянии строки.
func TestBCL1450_LivenessByIdAgreesWithLivenessBySecretOnEveryState(t *testing.T) {
	pool := basicCredPool(t)
	seedBasicOwners(t, pool)
	repo := pg.NewBasicCredentialRepo(pool)
	ctx := context.Background()

	const credID = "uoc_0000000000000cx01"
	secret := mintUserCredential(t, pool, credID, "usr0000000000000bat1")
	// Дату создания сдвигаем в прошлое: иначе истёкший срок — незаконный вход
	// ограничения `expires_at > created_at`, и состояние «истекло» неисполнимо.
	_, err := pool.Exec(ctx,
		`UPDATE user_oauth_clients SET created_at = now() - interval '2 days' WHERE id = $1`, credID)
	require.NoError(t, err)

	// Смена вида требует и смены формы: ограничение схемы держит их вместе
	// (`user_oauth_clients_credential_shape_ck`), поэтому хеш сохраняется и
	// возвращается вместе с видом. Иначе состояние «вид не SECRET» неисполнимо,
	// а неисполнимое Given — это проба, которая не проверяет ничего.
	var hashHex string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT encode(secret_hash, 'hex') FROM user_oauth_clients WHERE id = $1`, credID).Scan(&hashHex))
	require.NotEmpty(t, hashHex, "хеш пуст — Given состояния «вид снова SECRET» неисполним")

	// Каждое состояние: как его создать и живо ли удостоверение после этого.
	states := []struct {
		name     string
		arrange  string
		wantLive bool
	}{
		{"живое", `SELECT 1`, true},
		{"истёкшее", `UPDATE user_oauth_clients SET expires_at = now() - interval '1 second' WHERE id = '` + credID + `'`, false},
		{"снова живое", `UPDATE user_oauth_clients SET expires_at = now() + interval '30 days' WHERE id = '` + credID + `'`, true},
		{"владелец неактивен", `UPDATE users SET invite_status = 'BLOCKED' WHERE id = 'usr0000000000000bat1'`, false},
		{"владелец снова активен", `UPDATE users SET invite_status = 'ACTIVE' WHERE id = 'usr0000000000000bat1'`, true},
		{"вид не SECRET", `UPDATE user_oauth_clients SET credential_kind = 'KEYPAIR', secret_hash = ''::bytea WHERE id = '` + credID + `'`, false},
		{"вид снова SECRET", `UPDATE user_oauth_clients SET credential_kind = 'SECRET', secret_hash = decode('` + hashHex + `', 'hex') WHERE id = '` + credID + `'`, true},
		{"строка снята", `DELETE FROM user_oauth_clients WHERE id = '` + credID + `'`, false},
	}

	var agreed, positives int
	for _, st := range states {
		_, aerr := pool.Exec(ctx, st.arrange)
		require.NoError(t, aerr, "состояние %q не создать — Given пробы неисполним", st.name)

		_, bySecret := repo.ResolveBasic(ctx, secret)
		byID := repo.CheckBasicLive(ctx, credID)

		secretLive := bySecret == nil
		idLive := byID == nil
		require.Equal(t, secretLive, idLive,
			"состояние %q: полоса секрета говорит live=%v, полоса идентификатора — live=%v; "+
				"расхождение никем не решалось", st.name, secretLive, idLive)
		require.Equal(t, st.wantLive, idLive,
			"состояние %q: живость по идентификатору не та, какой её объявляет предикат", st.name)
		if !idLive {
			require.ErrorIs(t, byID, domain.ErrBasicCredentialRefused,
				"состояние %q: неживое удостоверение отвечает не единым отказом", st.name)
		}
		agreed++
		if st.wantLive {
			positives++
		}
	}

	t.Logf("осмотрено: состояний строки %d, из них живых %d", agreed, positives)
	require.NotZero(t, agreed, "состояний ноль — «полосы согласны» здесь означало бы «не сверено ни одно»")
	require.NotZero(t, positives,
		"живых состояний ноль — согласие было бы верно и о полосе, отвергающей всё")
}

// TestBCL1450_LivenessIsAskedWithoutTheSecret — вопрос отвечается по одному лишь
// идентификатору. Положительный контроль обязателен: «секрет не нужен» верно и о
// полосе, не находящей ничего.
func TestBCL1450_LivenessIsAskedWithoutTheSecret(t *testing.T) {
	pool := basicCredPool(t)
	seedBasicOwners(t, pool)
	repo := pg.NewBasicCredentialRepo(pool)
	ctx := context.Background()

	const human = "uoc_0000000000000cx02"
	const machine = "soc_0000000000000cx03"
	mintUserCredential(t, pool, human, "usr0000000000000bat1")
	mintSACredential(t, pool, machine, "sva0000000000000bat1")

	require.NoError(t, repo.CheckBasicLive(ctx, human),
		"живое удостоверение личности не признано живым по идентификатору")
	require.NoError(t, repo.CheckBasicLive(ctx, machine),
		"живое удостоверение служебной учётки не признано живым по идентификатору")

	// Отрицание в паре с положительным: неизвестный идентификатор годной формы.
	require.ErrorIs(t, repo.CheckBasicLive(ctx, "uoc_0000000000000cx04"),
		domain.ErrBasicCredentialRefused, "неизвестный идентификатор признан живым")
}

// TestBCL1450_LivenessRefusalIsSingleAndIsNoOracle — по различию отказов нельзя
// узнать, существует ли удостоверение. Полоса идентификатора опаснее полосы
// секрета: спрашивать её можно, ничего не предъявляя.
func TestBCL1450_LivenessRefusalIsSingleAndIsNoOracle(t *testing.T) {
	pool := basicCredPool(t)
	seedBasicOwners(t, pool)
	repo := pg.NewBasicCredentialRepo(pool)
	ctx := context.Background()

	const revoked = "uoc_0000000000000cx05"
	mintUserCredential(t, pool, revoked, "usr0000000000000bat1")
	_, err := pool.Exec(ctx, `DELETE FROM user_oauth_clients WHERE id = $1`, revoked)
	require.NoError(t, err)

	// Пять входов, ни один не живой, и различить их по отказу нельзя.
	inputs := []struct{ name, id string }{
		{"отозванное", revoked},
		{"не существовало никогда", "uoc_0000000000000cx06"},
		{"чужой префикс", "sva0000000000000bat1"},
		{"мусор", "не-идентификатор"},
		{"пусто", ""},
	}
	var msgs []string
	for _, in := range inputs {
		rerr := repo.CheckBasicLive(ctx, in.id)
		require.Error(t, rerr, "вход %q признан живым", in.name)
		require.ErrorIs(t, rerr, domain.ErrBasicCredentialRefused, "вход %q отвечает не единым отказом", in.name)
		msgs = append(msgs, rerr.Error())
	}
	for i := 1; i < len(msgs); i++ {
		require.Equal(t, msgs[0], msgs[i],
			"отказы различимы — по различию узнают, существует ли удостоверение")
	}
	t.Logf("осмотрено: неживых входов %d, все с одним отказом", len(msgs))

	// Положительный контроль в том же прогоне.
	const live = "uoc_0000000000000cx07"
	mintUserCredential(t, pool, live, "usr0000000000000bat1")
	require.NoError(t, repo.CheckBasicLive(ctx, live),
		"живое удостоверение отвергнуто — отрицания выше вакуумны")
}

// TestBCL1450_PresentedStringIsNotAcceptedAsAnIdentifier — предъявленная строка
// целиком в поле идентификатора отвергается, и это НЕ придирка к форме.
//
// Поле идентификатора не помечено носителем секрета намеренно: секрета в нём не
// бывает. Приняв полную строку молча, полоса сделала бы это утверждение ложным —
// секрет поехал бы по проводу в поле, которое никто не обязан беречь.
func TestBCL1450_PresentedStringIsNotAcceptedAsAnIdentifier(t *testing.T) {
	pool := basicCredPool(t)
	seedBasicOwners(t, pool)
	repo := pg.NewBasicCredentialRepo(pool)
	ctx := context.Background()

	const credID = "uoc_0000000000000cx08"
	secret := mintUserCredential(t, pool, credID, "usr0000000000000bat1")
	p, err := credsecret.Parse(secret)
	require.NoError(t, err)
	require.Equal(t, credID, p.CredentialID, "разбор вернул не тот идентификатор — проба ниже вакуумна")

	require.ErrorIs(t, repo.CheckBasicLive(ctx, secret), domain.ErrBasicCredentialRefused,
		"предъявленная строка принята как идентификатор — секрет поехал в поле, не помеченное носителем секрета")

	// Положительный контроль: голый идентификатор той же строки проходит.
	require.NoError(t, repo.CheckBasicLive(ctx, credID),
		"голый идентификатор отвергнут — отрицание выше верно и о полосе, отвергающей всё")
}
