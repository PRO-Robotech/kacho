// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogwriterlock_injection_test.go — доказательство, что Г1 СПОСОБЕН упасть,
// СПОСОБЕН смолчать и НЕ СПОСОБЕН зеленеть на пустом обходе (приёмка
// `plan-confirms-what-apply-withdraws.md` §7).
//
// # Инъекция роняет ТОЛЬКО проверяемое
//
// Каждый случай отличается от своего законного близнеца РОВНО одним названным
// фактом: снят метод замка · `_xact_` заменён сессионной формой · ключ подменён ·
// разбирающий вызов заменён исполняющим · у УЖЕ СУЩЕСТВУЮЩЕГО типа снято новое
// свойство. Форма «завести ещё один элемент» применяется ровно один раз и
// намеренно — там, где предмет инъекции и есть ВТОРОЙ писатель; во всех
// остальных случаях новый элемент нарушал бы всё, что требуется от писателей
// вообще, и красное приходило бы от соседа.
package repohygiene

import (
	"strings"
	"testing"
)

// ── Живая раскладка писателя, воспроизведённая синтетикой ───────────────────
//
// Один тип, оператор уезжает в исполнитель пакета (`changed`), а не прямо в
// драйвер: гейт, знающий только прямой `Exec`, молчал бы на живом писателе.

const catalogWriterLocked = `package pg

import "context"

const CatalogLockKey = "kaname.module_catalog"

type catalogWriter struct{ tx pgx.Tx }

func (w catalogWriter) LockCatalog(ctx context.Context) error {
	_, err := w.tx.Exec(ctx, ` + "`SELECT pg_advisory_xact_lock(hashtext($1))`" + `, CatalogLockKey)
	return err
}

func (w catalogWriter) UpsertModule(ctx context.Context, module string) (bool, error) {
	return w.changed(ctx, ` + "`" + `
		INSERT INTO kaname.catalog_module (module) VALUES ($1)
		ON CONFLICT (module) DO UPDATE SET live = true
		RETURNING 1` + "`" + `, module)
}

func (w catalogWriter) changed(ctx context.Context, sql string, args ...any) (bool, error) {
	var one int
	err := w.tx.QueryRow(ctx, sql, args...).Scan(&one)
	return err == nil, err
}
`

// catalogWriterNoLock — ДЕФЕКТ: у того же типа снят метод замка. Один факт.
const catalogWriterNoLock = `package pg

import "context"

const CatalogLockKey = "kaname.module_catalog"

type catalogWriter struct{ tx pgx.Tx }

func (w catalogWriter) UpsertModule(ctx context.Context, module string) (bool, error) {
	return w.changed(ctx, ` + "`" + `
		INSERT INTO kaname.catalog_module (module) VALUES ($1)
		ON CONFLICT (module) DO UPDATE SET live = true
		RETURNING 1` + "`" + `, module)
}

func (w catalogWriter) changed(ctx context.Context, sql string, args ...any) (bool, error) {
	var one int
	err := w.tx.QueryRow(ctx, sql, args...).Scan(&one)
	return err == nil, err
}
`

// catalogWriterSessionLock — тот же писатель, но замок СЕССИОННЫЙ. Один факт:
// из имени функции вынуто `_xact_`. Сессионный замок не снимается откатом, и
// оборванный применитель запирает каталог до возврата соединения в пул.
const catalogWriterSessionLock = `package pg

import "context"

const CatalogLockKey = "kaname.module_catalog"

type catalogWriter struct{ tx pgx.Tx }

func (w catalogWriter) LockCatalog(ctx context.Context) error {
	_, err := w.tx.Exec(ctx, ` + "`SELECT pg_advisory_lock(hashtext($1))`" + `, CatalogLockKey)
	return err
}

func (w catalogWriter) UpsertModule(ctx context.Context, module string) (bool, error) {
	return w.changed(ctx, ` + "`INSERT INTO kaname.catalog_module (module) VALUES ($1)`" + `, module)
}

func (w catalogWriter) changed(ctx context.Context, sql string, args ...any) (bool, error) {
	var one int
	err := w.tx.QueryRow(ctx, sql, args...).Scan(&one)
	return err == nil, err
}
`

// catalogWriterWrongKey — тот же писатель и та же форма замка, но ключ ЧУЖОЙ.
// Один факт. Два писателя на разных ключах проходят одновременно, и «замок
// взят» становится утверждением о вызове, а не о свойстве.
const catalogWriterWrongKey = `package pg

import "context"

const CatalogLockKey = "kaname.module_catalog"
const someOtherKey = "kaname.roles"

type catalogWriter struct{ tx pgx.Tx }

func (w catalogWriter) LockCatalog(ctx context.Context) error {
	_, err := w.tx.Exec(ctx, ` + "`SELECT pg_advisory_xact_lock(hashtext($1))`" + `, someOtherKey)
	return err
}

func (w catalogWriter) UpsertModule(ctx context.Context, module string) (bool, error) {
	return w.changed(ctx, ` + "`INSERT INTO kaname.catalog_module (module) VALUES ($1)`" + `, module)
}

func (w catalogWriter) changed(ctx context.Context, sql string, args ...any) (bool, error) {
	var one int
	err := w.tx.QueryRow(ctx, sql, args...).Scan(&one)
	return err == nil, err
}
`

// catalogSecondWriterInSamePackage — ВТОРОЙ пишущий тип рядом с запирающим.
//
// Единственный случай, где инъекция заводит новый элемент, и это её предмет:
// именно так свойство и истечёт молча. Единица «пакет» здесь смолчала бы —
// замок в пакете есть, берёт его другой тип.
const catalogSecondWriterInSamePackage = `package pg

import "context"

type catalogPruner struct{ tx pgx.Tx }

func (p catalogPruner) DropStale(ctx context.Context, module string) error {
	_, err := p.tx.Exec(ctx, ` + "`" + `
		UPDATE kaname.catalog_verb SET live = false WHERE module = $1` + "`" + `, module)
	return err
}
`

// ── Законный близнец, и он НЕ ГИПОТЕТИЧЕСКИЙ ────────────────────────────────
//
// `services/iam/internal/check/catalog_seed_parity.go` несёт ровно эти операторы
// текстом миграции: он их РАЗБИРАЕТ и сверяет посев с манифестом. Замер живого
// дерева на день заведения гейта: текстом совпало 9 операторов в 2 файлах,
// исполняется 5 в 1 файле — то есть различение имеет предмет прямо сейчас.

const catalogSeedParityParsesTheSameText = `package check

const tierOnlyVerbSeedPrefix = "INSERT INTO kaname.catalog_verb (module, resource, verb, per_object) VALUES"

// Сверка посева миграции с манифестом: операторы ниже РАЗБИРАЮТСЯ, а не
// исполняются. INSERT INTO kaname.catalog_module здесь стоит и в прозе.
func auditCatalogSeed(body string) ([][]string, error) {
	mods, err := parseSeedBlock(body, "INSERT INTO kaname.catalog_module (module) VALUES")
	if err != nil {
		return nil, err
	}
	res, err := parseSeedBlock(body, "INSERT INTO kaname.catalog_resource (module, resource, dotted) VALUES")
	if err != nil {
		return nil, err
	}
	verbs, err := parseSeedBlock(body, tierOnlyVerbSeedPrefix)
	if err != nil {
		return nil, err
	}
	return append(append(mods, res...), verbs...), nil
}

func parseSeedBlock(body, insertPrefix string) ([][]string, error) {
	i := indexOf(body, insertPrefix)
	if i < 0 {
		return nil, nil
	}
	return splitTuples(body[i:]), nil
}
`

// catalogSeedParityTurnedIntoAnExecutor — тот же файл, где РОВНО ОДИН факт
// изменён: разбирающий вызов заменён исполняющим. Больше ничего — те же
// литералы, та же константа, тот же пакет.
const catalogSeedParityTurnedIntoAnExecutor = `package check

const tierOnlyVerbSeedPrefix = "INSERT INTO kaname.catalog_verb (module, resource, verb, per_object) VALUES"

func auditCatalogSeed(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, tierOnlyVerbSeedPrefix)
	return err
}

func parseSeedBlock(body, insertPrefix string) ([][]string, error) {
	i := indexOf(body, insertPrefix)
	if i < 0 {
		return nil, nil
	}
	return splitTuples(body[i:]), nil
}
`

// scanCatalogOne — один синтетический файл через тот же предикат, что зовёт
// живой гейт.
func scanCatalogOne(t *testing.T, what, path, src string) ([]string, CatalogWriteCensus) {
	t.Helper()
	f, census, err := ScanCatalogWriteLocking([]CatalogSource{{Path: path, Src: []byte(src)}})
	if err != nil {
		t.Fatalf("разбор %s: %v", what, err)
	}
	if census.Parsed != 1 {
		t.Fatalf("%s: разобрано %d файлов из одного — вход беспредметен", what, census.Parsed)
	}
	return catalogWriteFindings(f), census
}

const catalogWriterRel = "services/iam/internal/repo/kaname/pg/catalog_writer.go"

// TestG1_ControlTheLockingWriterIsSilent — прогон 1: годный вход проходит.
func TestG1_ControlTheLockingWriterIsSilent(t *testing.T) {
	f, census := scanCatalogOne(t, "контроль", catalogWriterRel, catalogWriterLocked)

	if census.Executed == 0 {
		t.Fatalf("контроль беспредметен: исполняемых операторов записи прочитано ноль " +
			"— молчание сказано ни о чём")
	}
	if census.WriteUnits != 1 || census.LockedUnits != 1 {
		t.Fatalf("контроль: пишущих единиц %d, запирающих %d — ожидалась одна и та же",
			census.WriteUnits, census.LockedUnits)
	}
	if census.Executors == 0 {
		t.Fatalf("контроль: исполнителей пакета прочитано ноль — оператор, уехавший " +
			"в `changed`, разбором не прослежен, и живой писатель был бы невидим")
	}
	if len(f) != 0 {
		t.Fatalf("КОНТРОЛЬ: запирающий писатель объявлен находкой: %v", f)
	}
}

// TestG1_InjectionRedsTheWriterThatDoesNotLock — прогон 2: одно-фактный дефект.
func TestG1_InjectionRedsTheWriterThatDoesNotLock(t *testing.T) {
	f, census := scanCatalogOne(t, "инъекция «замок снят»", catalogWriterRel, catalogWriterNoLock)

	if census.Executed == 0 {
		t.Fatalf("инъекция беспредметна: исполняемых операторов ноль")
	}
	if census.LockSites != 0 {
		t.Fatalf("инъекция не сняла предмет: мест взятия замка %d", census.LockSites)
	}
	if len(f) != 1 {
		t.Fatalf("писатель БЕЗ замка каталога не стал находкой: находок %d\n"+
			"Пока замка нет, подтверждение применения сравнивает состояние, которое сосед "+
			"меняет прямо сейчас, — и «совпало» означает «совпало на момент чтения»", len(f))
	}
	// Находка обязана назвать КООРДИНАТУ, единицу и причину: «замка нет»,
	// «замок сессионный» и «ключ чужой» чинятся по-разному.
	if !strings.Contains(f[0], catalogWriterRel+":") {
		t.Errorf("находка не называет координату файла: %q", f[0])
	}
	if !strings.Contains(f[0], "catalogWriter") {
		t.Errorf("находка не называет единицу суждения: %q", f[0])
	}
	if !strings.Contains(f[0], "INSERT INTO kaname.catalog_module") {
		t.Errorf("находка не называет оператор: %q", f[0])
	}
	if !strings.Contains(f[0], "не берёт вовсе") {
		t.Errorf("находка не различает причину: %q", f[0])
	}
	// Инъекция проверяет не только «покраснел», но и ЧТО НАПЕЧАТАЛ: находка,
	// называющая симптом вместо причины, посылает читателя искать не там, и на
	// неё тратят прогон, а потом снимают гейт как непонятный.
	t.Logf("текст находки: %s", f[0])
}

// TestG1_InjectionProvesTheEmptyWalkCannotBeGreen — прогон 3, и его пропускают
// чаще всех: молчание проверки обязано быть отличимо от молчания МЁРТВОЙ
// проверки.
//
// Живой гейт превращает нулевую перепись в отказ (`census.Executed == 0` →
// `t.Fatalf`). Здесь доказано, что на пустом и на не-пишущем составе величины
// действительно нулевые, то есть у той ветки отказа есть производитель.
func TestG1_InjectionProvesTheEmptyWalkCannotBeGreen(t *testing.T) {
	// (а) состав пуст: находок ноль И переписи ноль. Первое без второго и есть
	// молчание мёртвой проверки.
	f, census, err := ScanCatalogWriteLocking(nil)
	if err != nil {
		t.Fatalf("разбор пустого состава: %v", err)
	}
	if len(f) != 0 {
		t.Fatalf("на пустом составе появились находки: %v", catalogWriteFindings(f))
	}
	if census.Parsed != 0 || census.Executed != 0 || census.TextMatches != 0 {
		t.Fatalf("пустой состав дал непустую перепись: разобрано %d, исполняется %d, "+
			"текстом совпало %d", census.Parsed, census.Executed, census.TextMatches)
	}

	// (б) состав НЕ ПУСТ, но предмета в нём нет: перепись растёт, исполняемых
	// операторов ноль. Именно это состояние живой гейт объявляет отказом — иначе
	// «писатель переехал» было бы неотличимо от «писатель в порядке».
	const unrelated = `package pg

import "context"

type roleWriter struct{ tx pgx.Tx }

func (w roleWriter) Upsert(ctx context.Context, id string) error {
	_, err := w.tx.Exec(ctx, ` + "`INSERT INTO kaname.roles (id) VALUES ($1)`" + `, id)
	return err
}
`
	f2, c2 := scanCatalogOne(t, "состав без предмета", "services/iam/internal/repo/kaname/pg/role_repo.go", unrelated)
	if c2.Parsed == 0 || c2.StringLiterals == 0 {
		t.Fatalf("состав без предмета не прочитан: разобрано %d, литералов %d",
			c2.Parsed, c2.StringLiterals)
	}
	if c2.Executed != 0 || c2.TextMatches != 0 {
		t.Fatalf("оператор над ЧУЖОЙ таблицей засчитан писателем каталога: "+
			"исполняется %d, текстом совпало %d", c2.Executed, c2.TextMatches)
	}
	if len(f2) != 0 {
		t.Fatalf("писатель чужой таблицы объявлен находкой: %v", f2)
	}
	// Ровно на этой величине живой гейт и отказывает: `Executed == 0`.
	t.Logf("предпосылка живого гейта имеет производителя: на составе без предмета "+
		"исполняемых операторов %d при %d прочитанных файлах", c2.Executed, c2.Parsed)
}

// TestG1_StaysSilentOnTheFileThatPARSESTheSameStatements — законный близнец, и
// он несущий: без него гейт был бы неотличим от предиката по подстроке.
func TestG1_StaysSilentOnTheFileThatPARSESTheSameStatements(t *testing.T) {
	const rel = "services/iam/internal/check/catalog_seed_parity.go"

	f, census := scanCatalogOne(t, "близнец «разбор текста миграции»", rel,
		catalogSeedParityParsesTheSameText)

	// Близнец обязан быть ПРЕДМЕТНЫМ: если текстовых совпадений ноль, молчание
	// доказывает не различение, а то, что образец вообще ни на что не похож.
	if census.TextMatches < 3 {
		t.Fatalf("близнец беспредметен: текстом совпало %d операторов — гейт по "+
			"подстроке здесь и не покраснел бы, а значит молчание ничего не доказывает",
			census.TextMatches)
	}
	if census.Executed != 0 {
		t.Fatalf("разбирающий вызов засчитан исполняющим: исполняется %d из %d "+
			"текстовых совпадений", census.Executed, census.TextMatches)
	}
	if len(f) != 0 {
		t.Fatalf("гейт судит ПОДСТРОКУ, а не узел разбора: файл, который операторы "+
			"РАЗБИРАЕТ, объявлен писателем — %v\n"+
			"`catalog_seed_parity.go` сверяет посев миграции с манифестом; он их не "+
			"исполняет, и гейт, краснеющий на нём, был бы снят первым читателем "+
			"(приёмка §7)", f)
	}

	// Вторая сторона того же близнеца: ОДИН изменённый факт — разбирающий вызов
	// заменён исполняющим — обязан дать находку. Иначе молчание выше означало бы
	// не различение, а слепоту.
	hit, c2 := scanCatalogOne(t, "инъекция «разбор стал исполнением»", rel,
		catalogSeedParityTurnedIntoAnExecutor)
	if c2.Executed == 0 {
		t.Fatalf("инъекция беспредметна: исполняемых операторов ноль")
	}
	if len(hit) != 1 {
		t.Fatalf("тот же файл, начавший ИСПОЛНЯТЬ оператор, не стал находкой "+
			"(находок %d) — значит молчание на близнеце есть слепота, а не различение", len(hit))
	}
	if !strings.Contains(hit[0], "оператор по имени `tierOnlyVerbSeedPrefix`") {
		t.Errorf("находка не называет оператор, приехавший ИМЕНЕМ константы: %q\n"+
			"Оператор, вынесенный в константу и переданный по имени, исполняется ровно "+
			"так же; разбор, знающий только литерал в аргументе, молчал бы на нём", hit[0])
	}
}

// TestG1_RedsTheSessionLockAndTheWrongKeySeparately — две оси причины, каждая
// одним фактом против того же контроля.
func TestG1_RedsTheSessionLockAndTheWrongKeySeparately(t *testing.T) {
	// ── Сессионный замок вместо транзакционного ──────────────────────────────
	f, census := scanCatalogOne(t, "инъекция «замок сессионный»", catalogWriterRel,
		catalogWriterSessionLock)
	if census.LockSites != 1 {
		t.Fatalf("сессионный замок не опознан как замок вовсе: мест %d — тогда "+
			"находка ниже не различала бы «замка нет» и «замок не тот»", census.LockSites)
	}
	if len(f) != 1 {
		t.Fatalf("СЕССИОННЫЙ замок засчитан за транзакционный (находок %d): он не "+
			"снимается откатом, и оборванный применитель запирает каталог до возврата "+
			"соединения в пул", len(f))
	}
	if !strings.Contains(f[0], "СЕССИОННЫЙ") {
		t.Errorf("находка не называет причину: %q", f[0])
	}
	t.Logf("текст находки (сессионный замок): %s", f[0])

	// ── Тот же замок на ЧУЖОМ ключе ──────────────────────────────────────────
	f, census = scanCatalogOne(t, "инъекция «ключ чужой»", catalogWriterRel, catalogWriterWrongKey)
	if census.LockSites != 1 {
		t.Fatalf("замок на чужом ключе не опознан как замок: мест %d", census.LockSites)
	}
	if len(f) != 1 {
		t.Fatalf("замок на ЧУЖОМ ключе засчитан за замок каталога (находок %d): два "+
			"писателя на разных ключах проходят одновременно", len(f))
	}
	if !strings.Contains(f[0], "ЧУЖОМ ключе") {
		t.Errorf("находка не называет причину: %q", f[0])
	}
	t.Logf("текст находки (чужой ключ): %s", f[0])

	// ── Контроль: ось не выхолощена ──────────────────────────────────────────
	if f, _ := scanCatalogOne(t, "контроль", catalogWriterRel, catalogWriterLocked); len(f) != 0 {
		t.Fatalf("КОНТРОЛЬ: верный замок объявлен находкой вместе с двумя неверными: %v", f)
	}
}

// TestG1_RedsASecondWriterWhileTheFirstOneStillLocks — то, ради чего гейт и
// заводится: свойство истекает МОЛЧА при появлении второго писателя.
//
// Единица суждения — ТИП. Единица «пакет» смолчала бы: замок в пакете есть,
// берёт его первый тип, а пишет второй.
func TestG1_RedsASecondWriterWhileTheFirstOneStillLocks(t *testing.T) {
	const dir = "services/iam/internal/repo/kaname/pg/"

	f, census, err := ScanCatalogWriteLocking([]CatalogSource{
		{Path: dir + "catalog_writer.go", Src: []byte(catalogWriterLocked)},
		{Path: dir + "catalog_pruner.go", Src: []byte(catalogSecondWriterInSamePackage)},
	})
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if census.Parsed != 2 {
		t.Fatalf("разобрано %d файлов из двух", census.Parsed)
	}
	if census.WriteUnits != 2 {
		t.Fatalf("пишущих единиц %d из двух — второй писатель не опознан как отдельная "+
			"единица, и гейт судит пакет, а не тип", census.WriteUnits)
	}
	if census.LockedUnits != 1 {
		t.Fatalf("запирающих единиц %d из одной", census.LockedUnits)
	}

	findings := catalogWriteFindings(f)
	if len(findings) != 1 {
		t.Fatalf("второй писатель в ТОМ ЖЕ пакете не стал находкой (находок %d): "+
			"замок в пакете есть, но берёт его другой тип — именно так свойство "+
			"истекает молча", len(findings))
	}
	if !strings.Contains(findings[0], "catalogPruner") {
		t.Errorf("находка не называет ВИНОВНИКА: %q", findings[0])
	}
	if strings.Contains(findings[0], "catalogWriter]") {
		t.Errorf("находка обвиняет запирающий тип вместо пишущего: %q", findings[0])
	}
	if !strings.Contains(findings[0], dir+"catalog_pruner.go:") {
		t.Errorf("находка не называет координату второго писателя: %q", findings[0])
	}
	if !strings.Contains(findings[0], "UPDATE kaname.catalog_verb") {
		t.Errorf("находка не называет оператор второго писателя: %q", findings[0])
	}
	t.Logf("текст находки: %s", findings[0])
}
