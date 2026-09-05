// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retentionsweeper_injection_test.go — доказательство, что гейт уборщиков
// СПОСОБЕН УПАСТЬ и СПОСОБЕН СМОЛЧАТЬ (RET-SWP-10…14).
//
// Инъекция идёт СИНТЕТИЧЕСКИМ корпусом через тот же разбор и то же суждение,
// которыми судит гейт. Своей копии предиката здесь нет: копия разошлась бы с
// оригиналом молча и доказывала бы способность упасть у кода, который не
// исполняется. И дерева она не трогает — значит уронить может только тот гейт,
// который доказывает.
package repohygiene

import (
	"strings"
	"testing"
)

// injectedSweeperWithoutCaller — уборщик по сроку, которого никто не зовёт.
const injectedSweeperWithoutCaller = `package pg

import "context"

type OrphanRepo struct{ pool any }

func (r *OrphanRepo) Reap(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, ` + "`DELETE FROM t WHERE expires_at <= now()`" + `)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
`

// injectedSweeperMultiline — тот же уборщик, чей оператор объявлен В НЕСКОЛЬКО
// СТРОК: построчный предикат увидел бы `DELETE FROM t` и `WHERE expires_at <=
// now()` порознь и не признал бы уборкой ни одну строку (RET-SWP-11).
const injectedSweeperMultiline = `package pg

import "context"

type OrphanRepo struct{ pool any }

func (r *OrphanRepo) SweepStale(ctx context.Context) (int64, error) {
	const q = ` + "`" + `
DELETE FROM t
 WHERE ctid IN (
     SELECT ctid FROM t
      WHERE expires_at <= now() - make_interval(secs => $1)
      LIMIT $2
 )` + "`" + `
	tag, err := r.pool.Exec(ctx, q, 0, 100)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
`

// injectedLawfulTwin — ЗАКОННЫЙ БЛИЗНЕЦ: удаление БЕЗ сравнения со временем.
//
// Он обязан молчать. Без него гейт ловил бы «функцию со словом DELETE», а не
// уборку по сроку, и первое же законное удаление строки его отключило бы.
const injectedLawfulTwin = `package pg

import "context"

type OrdinaryRepo struct{ pool any }

func (r *OrdinaryRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, ` + "`DELETE FROM t WHERE id = $1`" + `, id)
	return err
}
`

// injectedTimeReadWithoutDelete — второй законный близнец: сравнение со
// временем ЕСТЬ, удаления нет. Обязан молчать.
const injectedTimeReadWithoutDelete = `package pg

import "context"

type OrdinaryRepo struct{ pool any }

func (r *OrdinaryRepo) ListStale(ctx context.Context) error {
	_, err := r.pool.Query(ctx, ` + "`SELECT id FROM t WHERE expires_at <= now()`" + `)
	return err
}
`

// injectedSweeperViaNamedConst — уборщик, чей оператор объявлен ПАКЕТНОЙ
// величиной и позван по ИМЕНИ, да ещё собран склейкой из второй такой же.
//
// Форма списана с дерева, а не выдумана: так записаны `dpopPurgeSQL` шлюза и
// `drainSQL` nlb. До разбора имён оба уборщика были гейту НЕВИДИМЫ — не
// находкой, а молчанием.
const injectedSweeperViaNamedConst = `package pg

import "context"

const expiredPredicate = "t.expires_at <= now()"

const purgeSQL = "\nDELETE FROM t\n WHERE ctid IN (\n     SELECT ctid FROM t\n      WHERE " +
	expiredPredicate + "\n      LIMIT $1\n )"

type OrphanRepo struct{ pool any }

func (r *OrphanRepo) Purge(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, purgeSQL, 100)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
`

// injectedNamedConstWithoutTime — ЗАКОННЫЙ БЛИЗНЕЦ: пакетная величина есть,
// удаление есть, сравнения со временем нет. Обязан молчать.
//
// Без него разбор имён ловил бы «функцию, называющую константу со словом
// DELETE», и первое же законное удаление по ключу его отключило бы.
const injectedNamedConstWithoutTime = `package pg

import "context"

const deleteSQL = "DELETE FROM t WHERE id = $1"

type OrdinaryRepo struct{ pool any }

func (r *OrdinaryRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, deleteSQL, id)
	return err
}
`

// injectedQueryFromCallData — второй законный близнец: оператор приходит
// ПАРАМЕТРОМ, пакетной величины с таким именем нет.
//
// Это и есть узость расширения: узнаётся ровно то имя, которое объявлено ЗДЕСЬ
// ЖЕ. Широкое «всякий идентификатор, похожий на запрос» объявило бы уборщиком
// любую обёртку над `Exec`.
const injectedQueryFromCallData = `package pg

import "context"

type OrdinaryRepo struct{ pool any }

func (r *OrdinaryRepo) Exec(ctx context.Context, purgeSQL string) error {
	_, err := r.pool.Exec(ctx, purgeSQL)
	return err
}
`

// injectedFieldSharingTheConstName — третий законный близнец: пакетная величина
// объявлена, но функция читает ОДНОИМЁННОЕ ПОЛЕ, а не её.
//
// Правая часть селектора — поле чужого типа, и считать её именем величины
// значило бы объявить уборщиком функцию, оператора не называющую.
const injectedFieldSharingTheConstName = `package pg

import "context"

const purgeSQL = "DELETE FROM t WHERE expires_at <= now()"

type cfg struct{ purgeSQL string }

type OrdinaryRepo struct {
	pool any
	cfg  cfg
}

func (r *OrdinaryRepo) Run(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, r.cfg.purgeSQL)
	return err
}
`

// scanInjected — разбор одного синтетического файла тем же кодом, что и гейт.
func scanInjected(t *testing.T, name, dir, src string) []RetentionSweeper {
	t.Helper()
	out, census, err := ScanRetentionSweepers(name, dir, []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики %s: %v", name, err)
	}
	if census.Functions == 0 {
		t.Fatalf("в синтетике %s не осмотрено ни одной функции — доказывать нечем", name)
	}
	return out
}

// TestRetentionSweeperGate_Injection_RecognisesAndSpares — распознаватель:
// уборщик признан, законные близнецы — нет.
func TestRetentionSweeperGate_Injection_RecognisesAndSpares(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"уборщик одной строкой признан", injectedSweeperWithoutCaller, 1},
		{"уборщик в несколько строк признан", injectedSweeperMultiline, 1},
		{"удаление без сравнения со временем — не уборщик", injectedLawfulTwin, 0},
		{"сравнение со временем без удаления — не уборщик", injectedTimeReadWithoutDelete, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := scanInjected(t, "services/iam/internal/repo/kaname/pg/x.go",
				"services/iam/internal/repo/kaname/pg", tc.src)
			if len(got) != tc.want {
				t.Fatalf("распознано уборщиков %d, ожидалось %d: %+v", len(got), tc.want, got)
			}
		})
	}
}

// TestRetentionSweeperGate_Injection_FindsTheOrphanByName — суждение: уборщик
// без вызывающего есть находка, и находка НАЗЫВАЕТ ЕГО ИМЯ.
//
// Имя обязательно: находка без координаты неотличима от промаха разбора, и
// чинить по ней нечего.
func TestRetentionSweeperGate_Injection_FindsTheOrphanByName(t *testing.T) {
	sw := scanInjected(t, "services/iam/internal/repo/kaname/pg/x.go",
		"services/iam/internal/repo/kaname/pg", injectedSweeperWithoutCaller)

	findings, stale, wired := retentionSweeperVerdict(sw,
		map[string]map[string][]string{}, map[string]string{})

	if len(findings) != 1 {
		t.Fatalf("уборщик без вызывающего находкой НЕ стал: находок %d, провязанных %d", len(findings), wired)
	}
	if !strings.Contains(findings[0], "OrphanRepo.Reap") {
		t.Errorf("находка не называет уборщика по имени: %s", findings[0])
	}
	if !strings.Contains(findings[0], "x.go:") {
		t.Errorf("находка не называет координату: %s", findings[0])
	}
	if len(stale) != 0 {
		t.Errorf("ведомость пуста, а просроченных записей насчитано %d", len(stale))
	}
}

// TestRetentionSweeperGate_Injection_SilentWhenWired — обратная сторона: тот же
// уборщик С вызывающим — молчание.
//
// Без этой половины гейт ловил бы форму, а не существо: он краснел бы на всякой
// уборке, включая живую, и первый же ложный срабат его отключил бы.
func TestRetentionSweeperGate_Injection_SilentWhenWired(t *testing.T) {
	sw := scanInjected(t, "services/iam/internal/repo/kaname/pg/x.go",
		"services/iam/internal/repo/kaname/pg", injectedSweeperWithoutCaller)

	callers := map[string]map[string][]string{
		"Reap": {"services/iam": {"Sweeper.Pass"}},
	}
	findings, _, wired := retentionSweeperVerdict(sw, callers, map[string]string{})
	if len(findings) != 0 {
		t.Fatalf("уборщик С вызывающим объявлен находкой: %v", findings)
	}
	if wired != 1 {
		t.Fatalf("провязанных насчитано %d, ожидалась 1", wired)
	}
}

// TestRetentionSweeperGate_Injection_CallerInAnotherServiceDoesNotCount —
// однофамилец из ЧУЖОЙ службы вызывающим не является.
//
// Ради этого граница и заводилась: имя `Reap` в дереве носят два разных типа, и
// без границы вызывающий шлюза покрывал бы уборщика iam — то есть гейт молчал
// бы ровно на том состоянии, ради которого написан.
func TestRetentionSweeperGate_Injection_CallerInAnotherServiceDoesNotCount(t *testing.T) {
	sw := scanInjected(t, "services/iam/internal/repo/kaname/pg/x.go",
		"services/iam/internal/repo/kaname/pg", injectedSweeperWithoutCaller)

	callers := map[string]map[string][]string{
		"Reap": {"gateway": {"Store.reapLoop"}},
	}
	findings, _, _ := retentionSweeperVerdict(sw, callers, map[string]string{})
	if len(findings) != 1 {
		t.Fatalf("вызывающий из ЧУЖОЙ службы засчитан за своего: находок %d", len(findings))
	}
}

// TestRetentionSweeperGate_Injection_SelfCallIsNotACaller — уборщик, зовущий сам
// себя, вызывающим себе не является.
func TestRetentionSweeperGate_Injection_SelfCallIsNotACaller(t *testing.T) {
	sw := scanInjected(t, "services/iam/internal/repo/kaname/pg/x.go",
		"services/iam/internal/repo/kaname/pg", injectedSweeperWithoutCaller)

	callers := map[string]map[string][]string{
		"Reap": {"services/iam": {"OrphanRepo.Reap"}},
	}
	findings, _, _ := retentionSweeperVerdict(sw, callers, map[string]string{})
	if len(findings) != 1 {
		t.Fatalf("рекурсия объявила уборщика провязанным: находок %d", len(findings))
	}
}

// TestRetentionSweeperGate_Injection_LedgerExpiresByItself — RET-SWP-13.
//
// Запись ведомости, у которой уборщик ПОЛУЧИЛ вызывающего либо ИСЧЕЗ, — находка.
// Послабление, которое не истекает само, унаследует следующая слепая зона.
func TestRetentionSweeperGate_Injection_LedgerExpiresByItself(t *testing.T) {
	sw := scanInjected(t, "services/iam/internal/repo/kaname/pg/x.go",
		"services/iam/internal/repo/kaname/pg", injectedSweeperWithoutCaller)
	ledger := map[string]string{"OrphanRepo.Reap": "#0000"}

	t.Run("запись с ЖИВЫМ предметом молчит", func(t *testing.T) {
		findings, stale, _ := retentionSweeperVerdict(sw, map[string]map[string][]string{}, ledger)
		if len(findings) != 0 {
			t.Errorf("уборщик в ведомости объявлен находкой: %v", findings)
		}
		if len(stale) != 0 {
			t.Errorf("запись с живым предметом объявлена просроченной: %v", stale)
		}
	})

	t.Run("уборщик получил вызывающего — запись просрочена", func(t *testing.T) {
		callers := map[string]map[string][]string{"Reap": {"services/iam": {"Sweeper.Pass"}}}
		_, stale, _ := retentionSweeperVerdict(sw, callers, ledger)
		if len(stale) != 1 {
			t.Fatalf("запись, потерявшая предмет, находкой НЕ стала: %v", stale)
		}
		if !strings.Contains(stale[0], "#0000") {
			t.Errorf("находка не называет номер задачи: %s", stale[0])
		}
	})

	t.Run("уборщик исчез из дерева — запись просрочена", func(t *testing.T) {
		_, stale, _ := retentionSweeperVerdict(nil, map[string]map[string][]string{}, ledger)
		if len(stale) != 1 {
			t.Fatalf("запись об исчезнувшем уборщике находкой НЕ стала: %v", stale)
		}
	})
}

// TestRetentionSweeperGate_Injection_EmptyLedgerPasses — RET-SWP-14.
//
// Пустая ведомость есть ЦЕЛЬ, а не поломка: гейт, падающий на достижении своей
// цели, подталкивает держать запись ради зелёного. Способность гейта падать
// доказывают пробы ВЫШЕ — на синтетике, а не на живой записи ведомости: иначе
// доказательство исчезло бы вместе с целью.
func TestRetentionSweeperGate_Injection_EmptyLedgerPasses(t *testing.T) {
	sw := scanInjected(t, "services/iam/internal/repo/kaname/pg/x.go",
		"services/iam/internal/repo/kaname/pg", injectedSweeperMultiline)
	callers := map[string]map[string][]string{"SweepStale": {"services/iam": {"Sweeper.Pass"}}}

	findings, stale, wired := retentionSweeperVerdict(sw, callers, map[string]string{})
	if len(findings) != 0 || len(stale) != 0 {
		t.Fatalf("на пустой ведомости гейт нашёл: находок %v, просроченных %v", findings, stale)
	}
	if wired != 1 {
		t.Fatalf("провязанных насчитано %d, ожидалась 1", wired)
	}
}

// TestRetentionSweeperGate_Injection_NamedValueBand — вторая полоса разбора:
// оператор, объявленный ПАКЕТНОЙ величиной и позванный по имени.
//
// Полоса заведена не про запас: до неё два уборщика дерева
// (`Store.PurgeExpiredDPoPProofs` шлюза, `TargetDrainRunner.drainOnce` nlb) были
// вне наблюдения — ни красного, ни зелёного. Цена расширения измерена:
// осмотренных уборщиков 7 → 9, находок 0 → 0, полоса прежней формы (счётчик
// `Deletes`) не изменилась — то есть прибавка была слепой зоной, а не
// регрессией дерева.
//
// Утверждение идёт ЧЕТВЕРНЁЙ: дефект и три законных близнеца. Одного мало —
// каждый закрывает свою ось, и все три оси реальны.
func TestRetentionSweeperGate_Injection_NamedValueBand(t *testing.T) {
	for _, tc := range []struct {
		name        string
		src         string
		wantSweeper int
		wantNamed   int
	}{
		{"оператор пакетной величиной, собранный склейкой, — уборщик", injectedSweeperViaNamedConst, 1, 1},
		{"пакетная величина без сравнения со временем — не уборщик", injectedNamedConstWithoutTime, 0, 0},
		{"оператор из ДАННЫХ ВЫЗОВА — не уборщик", injectedQueryFromCallData, 0, 0},
		{"одноимённое ПОЛЕ вместо величины — не уборщик", injectedFieldSharingTheConstName, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, census, err := ScanRetentionSweepers("gateway/internal/x/x.go", "gateway/internal/x", []byte(tc.src))
			if err != nil {
				t.Fatalf("разбор синтетики: %v", err)
			}
			// Объём ВТОРОЙ полосы — отдельное утверждение: «уборщиков ноль»
			// обязано быть отличимо от «пакетных величин не прочитано ни одной».
			t.Logf("перепись: функций %d, литералов %d, пакетных строковых значений %d, из них назвали уборщика %d",
				census.Functions, census.Literals, census.NamedValues, census.Named)
			if len(got) != tc.wantSweeper {
				t.Fatalf("распознано уборщиков %d, ожидалось %d: %+v", len(got), tc.wantSweeper, got)
			}
			if census.Named != tc.wantNamed {
				t.Fatalf("признано ПО ИМЕНИ %d, ожидалось %d", census.Named, tc.wantNamed)
			}
		})
	}
}

// TestRetentionSweeperGate_Injection_NamedValueSweeperIsFoundByName — суждение
// над второй полосой: уборщик, объявленный пакетной величиной и НЕ позванный
// никем, есть находка, и находка называет имя, координату И сам оператор.
//
// Оператор в отказе обязателен именно здесь: у формы «по имени» текст лежит НЕ
// в теле функции, и находка без него была бы неотличима от промаха разбора —
// читателю пришлось бы искать, что именно гейт счёл уборкой.
func TestRetentionSweeperGate_Injection_NamedValueSweeperIsFoundByName(t *testing.T) {
	sw := scanInjected(t, "gateway/internal/x/x.go", "gateway/internal/x", injectedSweeperViaNamedConst)

	findings, stale, wired := retentionSweeperVerdict(sw,
		map[string]map[string][]string{}, map[string]string{})

	if len(findings) != 1 {
		t.Fatalf("уборщик пакетной величиной без вызывающего находкой НЕ стал: находок %d, провязанных %d",
			len(findings), wired)
	}
	if !strings.Contains(findings[0], "OrphanRepo.Purge") {
		t.Errorf("находка не называет уборщика по имени: %s", findings[0])
	}
	if !strings.Contains(findings[0], "x.go:") {
		t.Errorf("находка не называет координату: %s", findings[0])
	}
	if !strings.Contains(findings[0], "DELETE FROM t") || !strings.Contains(findings[0], "expires_at <= now()") {
		t.Errorf("находка не называет оператор, СОБРАННЫЙ из склейки: %s", findings[0])
	}
	if len(stale) != 0 {
		t.Errorf("ведомость пуста, а просроченных записей насчитано %d", len(stale))
	}
}

// TestRetentionSweeperGate_Injection_SelectorReceiverLiteralsStillCounted —
// контроль обхода: литерал, лежащий под СЕЛЕКТОРОМ, по-прежнему читается.
//
// Первая редакция разбора имён обрывала обход на селекторе, чтобы не считать
// поле именем величины, — и вместе с полем теряла литералы в его левой части.
// Сказала об этом перепись, а не проба: литералов 28210 → 27971, удалений
// 84 → 75. Утверждение стоит здесь, чтобы обрыв не вернулся молча.
func TestRetentionSweeperGate_Injection_SelectorReceiverLiteralsStillCounted(t *testing.T) {
	const src = `package pg

import "context"

type OrphanRepo struct{ pool any }

func (r *OrphanRepo) Reap(ctx context.Context) error {
	return r.pool.Exec(ctx, "DELETE FROM t WHERE expires_at <= now()").Scan()
}
`
	got, census, err := ScanRetentionSweepers("gateway/internal/x/x.go", "gateway/internal/x", []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Literals == 0 {
		t.Fatal("литерал под селектором не прочитан — обход оборвался на селекторе")
	}
	if census.Deletes != 1 {
		t.Fatalf("удалений насчитано %d, ожидалось 1", census.Deletes)
	}
	if len(got) != 1 || census.Named != 0 {
		t.Fatalf("уборщик обязан быть признан ПО ЛИТЕРАЛУ: уборщиков %d, по имени %d", len(got), census.Named)
	}
}
