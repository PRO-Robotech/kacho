// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

// Модульная учётная запись, посеянная iam, обязана называть компонент, который
// в дереве ЕСТЬ (задача продукта #1829).
//
// # ПРЕДМЕТ
//
// Служебная учётка модуля — это право ГОВОРИТЬ: предъявитель её ключа проходит
// как названный ею компонент. `security.md` §«Кто вправе ГОВОРИТЬ ЗА
// пользователя» требует, чтобы круг сужался ФАКТИЧЕСКИМИ отправителями,
// найденными по графу рёбер, а не бронью под будущее. Учётка, чьего компонента
// в дереве нет, — именно такая бронь: сертификата ему никто не выпускает, а
// строка и выданные ей права живут.
//
// # ПОЧЕМУ ГЕЙТ, А НЕ МИГРАЦИЯ
//
// Однократная миграция предмета не закрывает, и это сказано в самой задаче:
// следующий снятый компонент оставит тот же хвост. Миграцию нельзя ни повторить,
// ни заставить проверять будущее — а хвост заводится ровно тогда, когда
// компонент снимают, то есть в чужом изменении, где про эту строку никто не
// вспомнит.
//
// # ПОПУЛЯЦИЯ ОБЪЯВЛЕНА ДАННЫМИ, А НЕ ПЕРЕЧНЕМ ПРОЩЁННЫХ
//
// Модульной считается та учётка, чьё назначение объявляет её модульной
// (`Module SA:`), — признак стоит в самой посеянной строке. Перечень исключений
// здесь не заводится: запись в нём была бы местом, куда бронь вносят
// незамеченной.
//
// Блиндаж этого признака закрыт ВТОРОЙ осью: учётка, названная ПО КОМПОНЕНТУ, но
// модульной себя не объявившая, — тоже находка. Иначе признак снимали бы вместе
// с проверкой.
//
// # ЧЕГО ГЕЙТ НЕ ЛОВИТ (названо, чтобы на него не сослались шире предмета)
//
// Он про СОСТАВ, а не про ПРАВА: учётка существующего компонента, которой выдано
// больше нужного, ему не видна. И он про ПОСЕВ, а не про живую базу: строку,
// заведённую не миграцией, он не читает by construction.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// moduleSAMarker — признак модульной учётки, объявленный самой посеянной строкой.
const moduleSAMarker = "Module SA:"

var (
	reSAInsert = regexp.MustCompile(
		`(?is)^INSERT\s+INTO\s+kacho_iam\.service_accounts\s*\(([^)]*)\)\s*VALUES\s*\((.*)\)$`)
	reSADeleteByID = regexp.MustCompile(
		`(?is)^DELETE\s+FROM\s+kacho_iam\.service_accounts\s+WHERE\s+id\s*(?:=|IN)\s*\(?\s*(.+?)\s*\)?$`)
	reSADeleteByName = regexp.MustCompile(
		`(?is)^DELETE\s+FROM\s+kacho_iam\.service_accounts\s+WHERE\s+name\s*(?:=|IN)\s*\(?\s*(.+?)\s*\)?$`)
	reSQLLineComment = regexp.MustCompile(`--.*`)
	reSQLSpaceRun    = regexp.MustCompile(`\s+`)
	reSQLLiteral     = regexp.MustCompile(`'((?:[^']|'')*)'`)
)

// seededSA — посеянная служебная учётка.
type seededSA struct {
	id, name, description, where string
}

// isModule — учётка объявляет себя модульной.
func (s seededSA) isModule() bool { return strings.Contains(s.description, moduleSAMarker) }

// foldSeededServiceAccounts складывает посев по цепочке: вставка заводит строку,
// удаление её снимает. Возвращает живые строки, находки формы и объём осмотренного.
//
// Формы записи названы ЯВНО, и незнакомая — находка, а не молчание: форма, о
// которой разбор не знает, уводит предмет из-под наблюдения, ничего не нарушив
// (`testing.md` §«Гейт на класс» п. 7).
func foldSeededServiceAccounts(ordered []string, bodies map[string]string) (
	alive map[string]seededSA, unknownForms []string, stmtsTouched int,
) {
	alive = map[string]seededSA{}
	byName := map[string]string{} // имя → id, чтобы снятие по имени находило строку
	for _, name := range ordered {
		body := bodies[name]
		if i := strings.Index(body, "-- +goose Down"); i >= 0 {
			body = body[:i] // обратный ход возвращает снятое — судится только прямой
		}
		code := reSQLLineComment.ReplaceAllString(body, "")
		for _, raw := range strings.Split(code, ";") {
			stmt := strings.TrimSpace(reSQLSpaceRun.ReplaceAllString(raw, " "))
			if stmt == "" || !strings.Contains(stmt, "kacho_iam.service_accounts") {
				continue
			}
			verb := strings.ToUpper(strings.Fields(stmt)[0])
			if verb != "INSERT" && verb != "UPDATE" && verb != "DELETE" {
				continue // объявление таблицы, комментарий столбца, индекс — не посев
			}
			stmtsTouched++
			if m := reSAInsert.FindStringSubmatch(stmt); m != nil {
				if sa, ok := saOfInsert(m[1], m[2], name); ok {
					alive[sa.id] = sa
					byName[sa.name] = sa.id
					continue
				}
			}
			if m := reSADeleteByID.FindStringSubmatch(stmt); m != nil {
				for _, id := range sqlLiterals(m[1]) {
					delete(alive, id)
				}
				continue
			}
			if m := reSADeleteByName.FindStringSubmatch(stmt); m != nil {
				for _, n := range sqlLiterals(m[1]) {
					delete(alive, byName[n])
				}
				continue
			}
			unknownForms = append(unknownForms, fmt.Sprintf(
				"%s: оператор пишет служебные учётки формой, которой разбор не знает: %s",
				name, headOf(stmt)))
		}
	}
	return alive, unknownForms, stmtsTouched
}

func headOf(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func sqlLiterals(s string) []string {
	var out []string
	for _, m := range reSQLLiteral.FindAllStringSubmatch(s, -1) {
		out = append(out, strings.ReplaceAll(m[1], "''", "'"))
	}
	return out
}

func saOfInsert(colList, valList, where string) (seededSA, bool) {
	cols := strings.Split(colList, ",")
	vals := splitSQLValueList(valList)
	if len(cols) != len(vals) {
		return seededSA{}, false
	}
	byCol := map[string]string{}
	for i, c := range cols {
		byCol[strings.TrimSpace(c)] = strings.TrimSpace(vals[i])
	}
	id, okID := sqlLiteralOf(byCol["id"])
	name, okName := sqlLiteralOf(byCol["name"])
	if !okID || !okName {
		return seededSA{}, false
	}
	desc, _ := sqlLiteralOf(byCol["description"])
	return seededSA{id: id, name: name, description: desc, where: where}, true
}

func splitSQLValueList(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote && c == '\'':
			if i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteString("''")
				i++
				continue
			}
			inQuote = false
			cur.WriteByte(c)
		case !inQuote && c == '\'':
			inQuote = true
			cur.WriteByte(c)
		case !inQuote && c == ',':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, cur.String())
	return out
}

func sqlLiteralOf(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if len(v) < 2 || v[0] != '\'' || v[len(v)-1] != '\'' {
		return "", false
	}
	return strings.ReplaceAll(v[1:len(v)-1], "''", "'"), true
}

// componentsOfTree — компоненты, ВЫВЕДЕННЫЕ из дерева, а не выписанные.
//
// paths — пути ОТНОСИТЕЛЬНО корня дерева.
//
// Две ветви, и обе — свойство дерева: каталог сервиса (`services/<X>/`) и
// каталог верхнего уровня, несущий собственную точку входа (`<X>/cmd/<бинарь>/
// main.go`). Рукописного словаря здесь нет намеренно: он разошёлся бы с деревом
// молча — ровно тем классом, который эта проверка и ловит.
func componentsOfTree(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		segs := strings.Split(filepath.ToSlash(p), "/")
		if len(segs) >= 2 && segs[0] == "services" {
			out[segs[1]] = true
			continue
		}
		if len(segs) == 4 && segs[1] == "cmd" && segs[3] == "main.go" {
			out[segs[0]] = true
		}
	}
	return out
}

// namesAComponent — имя учётки называет компонент дерева.
//
// Нормализация ОДНА и объявлена здесь: снимается приставка `kacho-`, и годным
// считается либо остаток целиком, либо его последний токен (`api-gateway` →
// `gateway`). Второй ветви достаточно ровно для того случая, где имя компонента
// длиннее каталога, и она НЕ пропускает `vpc-operator`: `operator` компонентом
// не является.
func namesAComponent(saName string, components map[string]bool) (string, bool) {
	rest := strings.TrimPrefix(saName, "kacho-")
	if components[rest] {
		return rest, true
	}
	if i := strings.LastIndex(rest, "-"); i >= 0 && components[rest[i+1:]] {
		return rest[i+1:], true
	}
	return rest, false
}

// serviceAccountFindings — судья, отделённый от дерева ради инъекции.
func serviceAccountFindings(alive map[string]seededSA, components map[string]bool) []string {
	ids := make([]string, 0, len(alive))
	for id := range alive {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []string
	for _, id := range ids {
		sa := alive[id]
		comp, ok := namesAComponent(sa.name, components)
		switch {
		case sa.isModule() && !ok:
			out = append(out, fmt.Sprintf(
				"%s: модульная учётка %q (%s) называет компонент %q, которого в дереве НЕТ — "+
					"сертификата ему не выпускает никто, а право говорить за него живёт",
				sa.where, sa.name, id, comp))
		case !sa.isModule() && ok:
			out = append(out, fmt.Sprintf(
				"%s: учётка %q (%s) названа по компоненту %q, но модульной себя не объявляет "+
					"(нет %q в назначении) — признак снят вместе с проверкой",
				sa.where, sa.name, id, comp, moduleSAMarker))
		}
	}
	return out
}

// TestSeededModuleServiceAccountNamesAComponentOfTheTree — посев модульных
// учёток не шире перечня компонентов дерева.
func TestSeededModuleServiceAccountNamesAComponentOfTheTree(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	sqlFiles, err := treecorpus.UnderWithSuffix(
		filepath.Join(root, "services", "iam", "internal", "migrations"), ".sql")
	require.NoError(t, err, "перечень миграций берётся у индекса дерева, а не обходом диска")

	bodies := map[string]string{}
	ordered := make([]string, 0, len(sqlFiles))
	for _, f := range sqlFiles {
		raw, rerr := os.ReadFile(filepath.Clean(f)) // #nosec G304 -- путь из индекса дерева
		require.NoError(t, rerr, "чтение %s", f)
		name := filepath.Base(f)
		bodies[name] = string(raw)
		ordered = append(ordered, name)
	}
	sort.Strings(ordered) // порядок применения goose = лексикографический порядок имён

	alive, unknown, stmts := foldSeededServiceAccounts(ordered, bodies)

	all, err := treecorpus.Under(root)
	require.NoError(t, err, "перечень дерева берётся у индекса")
	rel := make([]string, 0, len(all))
	for _, p := range all {
		r, rerr := filepath.Rel(root, p)
		require.NoError(t, rerr, "путь %s не приводится к корню дерева", p)
		rel = append(rel, r)
	}
	components := componentsOfTree(rel)

	require.NotZerof(t, len(ordered), "миграций iam не прочитано ни одной — предпосылка гейта сломана")
	require.NotZerof(t, len(components), "компонентов из дерева не выведено ни одного — "+
		"«ноль находок» означало бы «ноль прочитанного»")
	require.NotZerof(t, len(alive), "в %d миграциях не разобрано ни одной посеянной служебной "+
		"учётки: либо имя таблицы сменилось, либо форма записи перестала попадать под разбор",
		len(ordered))
	require.Emptyf(t, unknown, "форма посева служебных учёток, неизвестная разбору:\n%s",
		strings.Join(unknown, "\n"))

	modules := 0
	for _, sa := range alive {
		if sa.isModule() {
			modules++
		}
	}
	findings := serviceAccountFindings(alive, components)

	t.Logf("перепись: миграций iam прочитано %d, операторов о служебных учётках %d, "+
		"живых учёток посева %d (из них модульных %d), компонентов дерева %d (%s), находок %d",
		len(ordered), stmts, len(alive), modules, len(components), joinComponentNames(components), len(findings))

	require.Empty(t, findings, strings.Join(findings, "\n"))
}

func joinComponentNames(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
