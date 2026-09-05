// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция гейта IAM-RV-1-12 «у проекции роли один АВТОР» — В ОБЕ СТОРОНЫ.
//
// Гейт обходит дерево, поэтому его признак воспроизводится здесь над СИНТЕТИЧЕСКИМ
// входом: доказывается, что он различает АВТОРСТВО, СНЯТИЕ и ЧТЕНИЕ, приписывает
// находку функции и молчит на законных формах. Инъекция правкой настоящего дерева
// не ставится намеренно — она рвала бы чужие прогоны в общей рабочей копии.
//
// Прогонов по каждой оси ТРИ, а не два: контроль · инъекция проверяемого ·
// законный близнец. Без третьего молчание проверки неотличимо от её смерти.
//
// ПЕРЕУСТРОЙСТВО ДОКАЗАНО ЗАНОВО. Единица гейта сменилась с «пишет» на «авторует»
// (kacho#1034), и перепись, сошедшаяся с прежней, о способности краснеть на НОВОЙ
// оси не говорит ничего. Поэтому набор ниже доказывает обе оси порознь, и каждая
// инъекция роняет ТОЛЬКО свою: инъекция авторства оставляет ось переселения
// нетронутой, и наоборот.

// opsOf — плоский перечень найденных операторов (для утверждений).
func opsOf(t *testing.T, src string) []string {
	t.Helper()
	return opsOfTable(t, src, roleVerbTable)
}

// opsOfTable — то же для ЛЮБОЙ из таблиц проекции.
func opsOfTable(t *testing.T, src, table string) []string {
	t.Helper()
	ops, _, err := projectionWritesIn("zz_injection.go", src, table)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		role := "снимает"
		if op.Authors {
			role = "АВТОРУЕТ"
		}
		if op.Relocates {
			role += "+переселяет"
		}
		out = append(out, op.Func+" → "+op.Verb+" ["+role+"]")
	}
	return out
}

// authorsOf — только вносящие строку: единица ОСИ 1.
func authorsOf(t *testing.T, src, table string) []string {
	t.Helper()
	ops, _, err := projectionWritesIn("zz_injection.go", src, table)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	var out []string
	for _, op := range ops {
		if op.Authors {
			out = append(out, op.Func)
		}
	}
	return out
}

// strandedOf — снимающие, чей оператор НЕ переселяет снятое: единица ОСИ 2.
func strandedOf(t *testing.T, src, table string) []string {
	t.Helper()
	ops, _, err := projectionWritesIn("zz_injection.go", src, table)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	var out []string
	for _, op := range ops {
		if !op.Authors && !op.Relocates {
			out = append(out, op.Func+" → "+op.Verb)
		}
	}
	return out
}

// ── ОБРАЗЦЫ, из которых собираются прогоны ──────────────────────────────────
//
// Каждый — форма, ДЕЙСТВИТЕЛЬНО стоящая в дереве, а не выдуманная: гейт судит их,
// и инъекция обязана подавать ему то же, что подаёт прод-код.

// srcAuthor — путь роли: снимает СВОЮ проекцию и вносит её заново.
const srcAuthor = `package pg

func (w *roleWriter) ReplaceRoleVerbs(ctx context.Context, roleID string) error {
	if _, err := w.tx.Exec(ctx, ` + "`DELETE FROM kaname.role_verb WHERE role_id = $1`" + `, roleID); err != nil {
		return err
	}
	_, err := w.tx.Exec(ctx, ` + "`INSERT INTO kaname.role_verb (role_id, object_type, verb) VALUES ($1,$2,$3)`" + `)
	return err
}`

// srcResettler — применитель каталога: снимает по чужому референту и ПЕРЕСЕЛЯЕТ
// снятое ТЕМ ЖЕ оператором. Законная форма — гейт обязан молчать на обеих осях.
const srcResettler = `package pg

func (w catalogWriter) ResettleTenantProjections(ctx context.Context) error {
	return w.tx.QueryRow(ctx, ` + "`" + `
		WITH doomed AS (
		  SELECT rv.role_id, rv.object_type, rv.verb FROM kaname.role_verb rv
		), moved AS (
		  INSERT INTO kaname.role_grant_orphan (role_id, object_type, verb, source, reason)
		  SELECT d.role_id, d.object_type, d.verb, 'role_verb', $1 FROM doomed d
		), dropped AS (
		  DELETE FROM kaname.role_verb rv USING doomed d
		   WHERE rv.role_id = d.role_id AND rv.object_type = d.object_type AND rv.verb = d.verb
		  RETURNING 1
		)
		SELECT (SELECT count(*) FROM doomed), (SELECT count(*) FROM dropped)` + "`" + `).Scan()
}`

// ── ОСЬ 1: АВТОР ОДИН ───────────────────────────────────────────────────────

// TestIAMRV112_AxisAuthor_ControlSeesExactlyOneAuthor — КОНТРОЛЬ.
//
// Дерево цело: автор один, применитель автором НЕ является. Обе формы вместе
// обязаны дать ровно одного — иначе всё, что ниже, судит сломанный признак.
func TestIAMRV112_AxisAuthor_ControlSeesExactlyOneAuthor(t *testing.T) {
	if got := authorsOf(t, srcAuthor, roleVerbTable); len(got) != 1 {
		t.Fatalf("на пути роли авторов %d, а он ровно один: %v", len(got), got)
	}
	if got := authorsOf(t, srcResettler, roleVerbTable); len(got) != 0 {
		t.Fatalf("применитель признан АВТОРОМ: %v — он строку не вносит, а снимает, и "+
			"считать его автором значило бы вернуть прежнюю единицу счёта", got)
	}
}

// TestIAMRV112_AxisAuthor_RedOnASecondAuthor — ИНЪЕКЦИЯ ПРОВЕРЯЕМОГО.
//
// Второй вносящий со своим сырым SQL в слое досева — ровно тот писатель, что жил
// в дереве до #1028. Ось обязана покраснеть и НАЗВАТЬ функцию.
func TestIAMRV112_AxisAuthor_RedOnASecondAuthor(t *testing.T) {
	src := `package seed

func replaceRoleVerbsTx(ctx context.Context, tx pgxExecer, roleID string) error {
	if _, err := tx.Exec(ctx, ` + "`DELETE FROM kaname.role_verb WHERE role_id = $1`" + `, roleID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, ` + "`INSERT INTO kaname.role_verb (role_id, object_type, verb) VALUES ($1,$2,$3)`" + `)
	return err
}`
	got := authorsOf(t, src, roleVerbTable)
	if len(got) == 0 {
		t.Fatal("ось авторства МОЛЧИТ на втором вносящем — гейт не способен покраснеть, " +
			"и его зелёный на дереве ничего не значит")
	}
	if got[0] != "replaceRoleVerbsTx" {
		t.Errorf("находка не приписана функции: %q — читатель пойдёт искать координату "+
			"и не найдёт её", got[0])
	}
	// Инъекция роняет ТОЛЬКО проверяемое: снятие здесь идёт по своей роли и оси
	// переселения не касается — она обязана остаться при своём вердикте.
	if stranded := strandedOf(t, src, roleVerbTable); len(stranded) != 1 {
		t.Errorf("инъекция авторства задела ось переселения: %v", stranded)
	}
}

// TestIAMRV112_AxisAuthor_SilentOnAPureRemover — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Снимающий-переселяющий не вносит строку и потому автором не считается. Ось,
// краснеющая здесь, отвергала бы применитель каталога — то есть саму возможность
// снять модуль.
func TestIAMRV112_AxisAuthor_SilentOnAPureRemover(t *testing.T) {
	if got := authorsOf(t, srcResettler, roleVerbTable); len(got) != 0 {
		t.Errorf("ось авторства краснеет на СНИМАЮЩЕМ: %v — под таким предикатом снятие "+
			"модуля не прошло бы ни разу", got)
	}
}

// ── ОСЬ 2: СНИМАЮЩИЙ НЕ-АВТОР ПЕРЕСЕЛЯЕТ ────────────────────────────────────

// TestIAMRV112_AxisRelocation_ControlSeesTheResettlerAsRelocating — КОНТРОЛЬ.
func TestIAMRV112_AxisRelocation_ControlSeesTheResettlerAsRelocating(t *testing.T) {
	if got := strandedOf(t, srcResettler, roleVerbTable); len(got) != 0 {
		t.Fatalf("настоящая форма применителя признана НЕ переселяющей: %v — признак "+
			"переселения не видит `INSERT INTO %s` в том же операторе, и всё, что ниже, "+
			"судит сломанный предикат", got, roleGrantOrphanTable)
	}
}

// TestIAMRV112_AxisRelocation_RedWhenRemovalStrandsTheRow — ИНЪЕКЦИЯ ПРОВЕРЯЕМОГО.
//
// Тот же применитель, у которого снята ТОЛЬКО половина переселения. Форма
// осталась законной по всем прочим осям: он не вносит строку, лежит в слое
// репозитория, автора не удваивает, — и именно поэтому ось нужна отдельная.
func TestIAMRV112_AxisRelocation_RedWhenRemovalStrandsTheRow(t *testing.T) {
	src := `package pg

func (w catalogWriter) ResettleTenantProjections(ctx context.Context) error {
	return w.tx.QueryRow(ctx, ` + "`" + `
		WITH doomed AS (
		  SELECT rv.role_id, rv.object_type, rv.verb FROM kaname.role_verb rv
		), dropped AS (
		  DELETE FROM kaname.role_verb rv USING doomed d
		   WHERE rv.role_id = d.role_id AND rv.object_type = d.object_type AND rv.verb = d.verb
		  RETURNING 1
		)
		SELECT count(*) FROM dropped` + "`" + `).Scan()
}`
	got := strandedOf(t, src, roleVerbTable)
	if len(got) == 0 {
		t.Fatal("ось переселения МОЛЧИТ на снятии БЕЗ переселения — то есть на том самом " +
			"исходе, ради которого таблица сирот и заведена: право отобрано, записи нет")
	}
	if !strings.Contains(got[0], "catalogWriter.ResettleTenantProjections") {
		t.Errorf("находка не приписана функции: %q", got[0])
	}
	// Инъекция роняет ТОЛЬКО проверяемое: авторов она не добавила.
	if authors := authorsOf(t, src, roleVerbTable); len(authors) != 0 {
		t.Errorf("инъекция переселения задела ось авторства: %v", authors)
	}
}

// TestIAMRV112_AxisRelocation_SilentOnTheAuthorsOwnDelete — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Автор снимает СВОЮ проекцию и заменяет её тем же вызовом — переселять ему
// нечего. Гейт обязан отличать это от снятия по чужому поводу, иначе путь роли
// краснел бы на каждой правке роли.
func TestIAMRV112_AxisRelocation_SilentOnTheAuthorsOwnDelete(t *testing.T) {
	stranded := strandedOf(t, srcAuthor, roleVerbTable)
	if len(stranded) != 1 {
		t.Fatalf("признак не увидел снятия у автора: %v", stranded)
	}
	// Сам признак снятие видит — освобождает автора ГЕЙТ, по совпадению ключа.
	// Проверяется это здесь, а не на глаз: иначе освобождение было бы объявлено,
	// а не исполнено.
	if authors := authorsOf(t, srcAuthor, roleVerbTable); len(authors) != 1 ||
		!strings.Contains(stranded[0], authors[0]) {
		t.Errorf("снятие и авторство приписаны РАЗНЫМ функциям (%v против %v) — тогда "+
			"освобождение автора по ключу не сработает, и путь роли покраснеет на каждой "+
			"правке роли", stranded, authors)
	}
}

// ── ЗАКОННЫЕ БЛИЗНЕЦЫ, ОБЩИЕ ДЛЯ ОБЕИХ ОСЕЙ ─────────────────────────────────

// TestIAMRV112_InjectionSilentOnBothReaders — читателей проекции в дереве ШЕСТЬ,
// и обе формы чтения обязаны молчать: одни считают строки, другие соединяют.
// Близнец, названный одним экземпляром, оставил бы вторую форму непроверенной, и
// гейт, ловящий `SELECT` за запись, покраснел бы на ней при первом же прогоне.
func TestIAMRV112_InjectionSilentOnBothReaders(t *testing.T) {
	whole := `package scalegrid

func (c *census) read() error {
	return scalar(&c.RoleVerbs, ` + "`SELECT count(*)::bigint FROM kaname.role_verb`" + `)
}`
	perRole := `package scalegrid

func strengthOf(ctx context.Context, roleID string) error {
	return q.QueryRow(ctx,
		` + "`SELECT count(*)::bigint FROM kaname.role_verb WHERE role_id = $1`" + `, roleID).Scan(&n)
}`
	joined := `package relverdict

func expand(ctx context.Context) error {
	return q.Query(ctx, ` + "`SELECT 1 FROM roles r JOIN kaname.role_verb rv ON rv.role_id = r.id`" + `)
}`
	for name, src := range map[string]string{
		"чтение таблицы целиком": whole,
		"чтение по одной роли":   perRole,
		"соединение с ролями":    joined,
	} {
		if got := opsOf(t, src); len(got) != 0 {
			t.Errorf("признак краснеет на ЧТЕНИИ проекции (%s): %v — то есть на том, ради чего "+
				"таблица и заведена", name, got)
		}
	}
}

// TestIAMRV112_InjectionSilentOnItsOwnExplanation — ЗАКОННЫЙ БЛИЗНЕЦ второго рода:
// имя таблицы и слово INSERT в КОММЕНТАРИИ, объясняющем эту самую проверку.
//
// Гейт по подстроке над текстом файла краснел бы здесь — то есть на собственном
// объяснении. Признак судит узел-литерал разобранного дерева и обязан молчать.
func TestIAMRV112_InjectionSilentOnItsOwnExplanation(t *testing.T) {
	src := `package repohygiene

// Предмет: INSERT INTO kaname.role_verb из второго места — находка.
// Проверяется оператор DELETE FROM kaname.role_verb тоже, и переселение
// INSERT INTO kaname.role_grant_orphan — тоже.
func explain() {}
`
	if got := opsOf(t, src); len(got) != 0 {
		t.Errorf("признак краснеет на КОММЕНТАРИИ, объясняющем проверку: %v — гейт, красный "+
			"на собственном объяснении, снимут первым", got)
	}
}

// TestIAMRV112_InjectionSilentOnAnotherTable — запись в чужую таблицу не находка.
func TestIAMRV112_InjectionSilentOnAnotherTable(t *testing.T) {
	src := `package pg

func put(ctx context.Context) error {
	_, err := tx.Exec(ctx, ` + "`INSERT INTO kaname.role_rule_selectors (role_id) VALUES ($1)`" + `)
	return err
}`
	if got := opsOf(t, src); len(got) != 0 {
		t.Errorf("признак краснеет на записи в ЧУЖУЮ таблицу: %v", got)
	}
}

// TestIAMRV112_LayerPredicateSeparatesRepoFromSeed — третья ось гейта: слой.
//
// Она проверяется отдельно от первых двух, потому что инъекция обязана ронять
// ТОЛЬКО проверяемое: автор может быть ОДИН и при этом лежать не там.
func TestIAMRV112_LayerPredicateSeparatesRepoFromSeed(t *testing.T) {
	inRepo := "services/iam/internal/repo/kacho/pg/role_repo.go"
	inSeed := "services/iam/internal/apps/kacho/seed/migrate_backfill.go"

	if !strings.Contains("/"+inRepo, roleVerbWriterLayer) {
		t.Errorf("предикат слоя не признаёт законного места писателя (%s) — гейт краснел бы "+
			"на единственно верной раскладке", inRepo)
	}
	if strings.Contains("/"+inSeed, roleVerbWriterLayer) {
		t.Errorf("предикат слоя признаёт своим SQL в слое use-case (%s) — третья ось гейта "+
			"вакуумна", inSeed)
	}
}

// ── ВТОРАЯ таблица проекции (kacho#1030, требование Т5) ──────────────────────
//
// Расширение охвата обязано быть доказано ЗАНОВО: перепись, сошедшаяся с
// прежней, о способности краснеть на НОВОЙ таблице не говорит ничего. Пробы
// ниже подают тот же синтетический вход, но по второй таблице, и требуют от
// признака того же — красноты на втором авторе, на снятии без переселения, и
// молчания на чтении.

func TestIAMCT105_InjectionRedOnASecondRuleRefAuthor(t *testing.T) {
	src := `package seed

func reseedRuleRefsTx(ctx context.Context, tx pgxExecer, roleID string) error {
	if _, err := tx.Exec(ctx, ` + "`DELETE FROM kaname.role_rule_ref WHERE role_id = $1`" + `, roleID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, ` + "`INSERT INTO kaname.role_rule_ref (role_id, module, resource) VALUES ($1,$2,$3)`" + `)
	return err
}`
	got := authorsOf(t, src, roleRuleRefTable)
	if len(got) != 1 || got[0] != "reseedRuleRefsTx" {
		t.Fatalf("второй АВТОР ВТОРОЙ проекции обязан находиться и называться функцией; "+
			"найдено: %v", got)
	}
}

func TestIAMCT105_InjectionRedWhenRuleRefRemovalStrandsTheRow(t *testing.T) {
	src := `package pg

func (w catalogWriter) resettle(ctx context.Context) error {
	return w.tx.QueryRow(ctx, ` + "`DELETE FROM kaname.role_rule_ref rr USING doomed d WHERE rr.role_id = d.role_id`" + `).Scan()
}`
	if got := strandedOf(t, src, roleRuleRefTable); len(got) != 1 {
		t.Fatalf("снятие сегментов БЕЗ переселения обязано находиться: %v", got)
	}
}

func TestIAMCT105_InjectionSilentOnARuleRefReader(t *testing.T) {
	src := `package relverdict

func rulesRefsOfRole(ctx context.Context, q pgxQuerier, roleID string) (int, error) {
	var n int
	err := q.QueryRow(ctx, ` + "`SELECT count(*) FROM kaname.role_rule_ref WHERE role_id = $1`" + `, roleID).Scan(&n)
	return n, err
}`
	if got := opsOfTable(t, src, roleRuleRefTable); len(got) != 0 {
		t.Fatalf("ЧТЕНИЕ второй проекции законно и обязано молчать — читателей у неё будет "+
			"несколько; найдено: %v", got)
	}
}

func TestIAMCT105_InjectionSilentOnTheOtherProjection(t *testing.T) {
	if got := opsOfTable(t, srcAuthor, roleRuleRefTable); len(got) != 0 {
		t.Fatalf("признак второй таблицы обязан судить ТОЛЬКО её: иначе расширение охвата "+
			"смешало бы две популяции и находка называла бы не тот предмет; найдено: %v", got)
	}
}
