// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journaldoor_test.go — В ДВИЖОК ПИШУТ ТОЛЬКО ЧЕРЕЗ СТРОКУ ЖУРНАЛА.
//
// ЧТО ЗАЩИЩАЕТСЯ. Миграция 0098 объявляет инвариант, на котором стоит проекция
// `relation_fact`, а на ней — форма E: состояние чужого хранилища отношений есть
// свёртка ОДНОГО журнала, `kacho_iam.fga_outbox`. Инвариант держится ровно до тех
// пор, пока в движок нельзя написать мимо журнала. Место, пишущее мимо, отдаёт
// движку кортеж, которого проекция не увидит НИКОГДА, — и своя БД становится
// беднее чужой молча: пустая проекция ничем не отличается от честного отказа.
//
// ПОЧЕМУ ЭТОТ ГЕЙТ, КОГДА РЯДОМ УЖЕ ЕСТЬ ДРУГОЙ. Страж
// `internal/repohygiene/fgaoutboxrowowner_test.go` смотрит в ОБРАТНУЮ сторону: кто
// вправе РЕНДЕРИТЬ строку журнала. Он ничего не говорит о том, пишут ли в движок
// только через неё, — а инвариант ломается именно в эту сторону.
//
// ПОЧЕМУ ПО СВОЙСТВУ, А НЕ ПО ПЕРЕЧНЮ ИМЁН. Перечень расходится с деревом молча, и
// это не гипотеза: задача, заведшая гейт, называла ТРИ пути записи мимо журнала, а
// перепись по свойству нашла СЕМЬ. Четыре из них — включая тогда ещё живую дверь
// `InternalIAMService.WriteCreatorTuple` (снята #788) — в перечне не значились.
// Поэтому места берутся не грепом по именам, а переписью `engineplaces`, где
// перечень методов ВЫВОДИТ КОМПИЛЯТОР из якорного типа клиента движка, а метод без
// рода роняет саму перепись.
//
// ЧТО ГЕЙТ ДЕРЖИТ, А ЧТО НЕТ — СКАЗАНО ПРЯМО. Он держит: НИ ОДНО место записи в
// движок не появится и не исчезнет НЕЗАМЕЧЕННЫМ. Каждое обязано нести вердикт, и
// вердикт обязан иметь место (послабление, которому нечего исключать, — находка).
// Он НЕ доказывает для каждого места, что строка журнала действительно легла: это
// свойство пути исполнения, а не текста. Ближайшее, что проверяется механически, —
// что названная вердиктом точка эмиссии ЖИВА (`TestJournalDoor_JournalBackedRuling…`).
// Заявлять большее значило бы выдать перечень за доказательство.
package engineplaces_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
	"github.com/PRO-Robotech/kacho/tools/authzenginecensus/engineplaces"
)

// ── Вердикты: закрытый перечень из ТРЁХ, четвёртого нет ──────────────────────
//
// «Оставили как есть» вердиктом не является by construction: место без записи в
// ведомости роняет гейт.
const (
	// doorDrain — место САМО и есть свёртка: его вход — строка журнала.
	doorDrain = "дренаж журнала"
	// doorJournalBacked — строка журнала со-коммичена вызывающим до записи;
	// вердикт ОБЯЗАН назвать живую точку эмиссии.
	doorJournalBacked = "строка журнала со-коммичена"
	// doorException — пишет мимо журнала, объявлено, с причиной и предикатом
	// снятия. Послабление самоистекает: исчезнет место — гейт покраснеет.
	doorException = "объявленное исключение"
)

// doorRuling — вердикт по ОДНОМУ файлу, содержащему места записи в движок.
type doorRuling struct {
	Verdict string
	// Places — сколько мест записи в этом файле. Число пиннится НАМЕРЕННО:
	// новая запись в уже отсуженном файле иначе прошла бы молча — а это ровно
	// та форма, в которой обход и заводят. Разошлось — перепроверь путь и
	// обнови число вместе с обоснованием.
	Places int
	// JournalAt — «путь:символ» точки, кладущей строку журнала для этого пути.
	// Обязателен для doorJournalBacked; проверяется на ЖИВОСТЬ разбором AST.
	JournalAt string
	Why       string
	// Predicate — для doorException: чем снимается послабление.
	Predicate string
}

// engineWriteDoors — ВЕДОМОСТЬ. Ключ — файл дерева; вердикт — по каждому.
//
// Ведомость не является способом обхода: она не расширяет права места, она
// ФИКСИРУЕТ принятое решение так, чтобы его нельзя было принять молча.
var engineWriteDoors = map[string]doorRuling{
	// ── свёртка ──────────────────────────────────────────────────────────────
	"services/iam/internal/clients/fga_applier.go": {
		Verdict: doorDrain, Places: 2,
		Why: "дренаж: вход этого места — сама строка журнала, оно и есть свёртка журнала в состояние движка",
	},

	// ── строка журнала со-коммичена вызывающим ───────────────────────────────
	"services/iam/internal/apps/kacho/api/access_binding/delete.go": {
		Verdict: doorJournalBacked, Places: 1,
		JournalAt: "services/iam/internal/repo/kacho/pg/access_binding_repo.go:emitFGAOutbox",
		Why:       "AccessBinding.Delete: снятие кортежей эмитится в writer-tx удаления привязки, синхронная запись — ускоритель",
	},
	"services/iam/internal/apps/kacho/api/access_binding/revoke.go": {
		Verdict: doorJournalBacked, Places: 1,
		JournalAt: "services/iam/internal/repo/kacho/pg/access_binding_repo.go:emitFGAOutbox",
		Why:       "AccessBinding.Revoke: та же эмиссия в writer-tx, что у Delete",
	},
	"services/iam/internal/clients/hierarchy_tuple_applier.go": {
		Verdict: doorJournalBacked, Places: 2,
		JournalAt: "services/iam/internal/apps/kacho/api/internal_iam/register_resource.go:emitGrant",
		Why:       "RegisterResource/UnregisterResource: намерение ложится в журнал ДО применения, применение — ускоритель",
	},
	"services/iam/internal/repo/kacho/pg/reconcile_adapter.go": {
		Verdict: doorJournalBacked, Places: 4,
		JournalAt: "services/iam/internal/repo/kacho/pg/reconcile_adapter.go:EmitTupleWrite",
		Why:       "реконсайлер: набор эмитится в writer-tx (EmitTupleWrite/EmitTupleDelete), синхронная запись — ускоритель окна материализации",
	},

	// ── объявленных исключений в Go-дереве БОЛЬШЕ НЕТ ────────────────────────
	//
	// Здесь стояли два: `RelationProjector.WriteRaw` (← административный
	// `InternalAuthorizeService.WriteTuples`) и `InternalIAMService.
	// WriteCreatorTuple`. Оба писали кортёж мимо журнала, у обоих вызывающих
	// было НОЛЬ, и предикат снятия у обоих был один — снять RPC.
	//
	// Предикат исполнен (#788): RPC удалены с контракта, места записи ушли
	// вместе с ними, и ведомость назвала записи истёкшими САМА — прогон дал
	// ровно две находки «вердикт без места» до того, как эти строки были сняты.
	// Это и есть самоистечение послабления: оно не пережило своего предмета.
	//
	// ПУСТОТА ЗДЕСЬ — ЦЕЛЬ, А НЕ ЗАБЫТАЯ КАРТА. Единственное оставшееся
	// исключение — не-Go: посев чарта (nonGoWriteDoors ниже).
}

// ── не-Go писатели ───────────────────────────────────────────────────────────

// nonGoWriteDoors — ведомость исполняемых файлов, шлющих запись в движок.
var nonGoWriteDoors = map[string]doorRuling{
	"deploy/helm/umbrella/charts/openfga-bootstrap/files/bootstrap.sh": {
		Verdict: doorException, Places: 1,
		Why: "посев чарта: ставит кортежи одиночки-кластера ДО того, как журнал вообще может быть применён — " +
			"хранилище и модель, без которых дренаж не адресуем, создаёт этот же скрипт.",
		Predicate: "послабление ОГРАНИЧЕНО: BootstrapDeclaredTuples пиннит НАБОР кортежей; третий кортеж роняет гейт " +
			"(TestJournalDoor_BootstrapExceptionIsBoundedToItsDeclaredTuples).",
	},
	"deploy/tests/conformance/fga-model/run-secl-model-conformance.sh": {
		Verdict: doorException, Places: 1,
		Why:       "оснастка проб: поднимает СВОЙ одноразовый движок в docker и пишет в СВОЁ хранилище — не в наше",
		Predicate: "истекает вместе с самим файлом: исчезнет проба — вердикт без места покраснеет",
	},
	"deploy/tests/helm/bootstrap-refusal-classified-test.sh": {
		Verdict: doorException, Places: 1,
		Why:       "оснастка проб: вносит дефект в КОПИЮ bootstrap.sh, доказывая, что страж посева умеет краснеть; сама ничего не пишет",
		Predicate: "истекает вместе с самим файлом",
	},
}

// BootstrapDeclaredTuples — кортежи, которые посеву чарта РАЗРЕШЕНО поставить
// мимо журнала. Набор, а не число: третий кортеж — расширение обхода, и его
// обязан увидеть человек.
var bootstrapDeclaredTuples = []string{
	"cluster:${CLUSTER_SINGLETON_ID}#system_viewer@user:bootstrap_marker",
	"cluster:${CLUSTER_SINGLETON_ID}#viewer@user:*",
}

// ── Судья ────────────────────────────────────────────────────────────────────

// adjudicateDoors сверяет места записи с ведомостью В ОБЕ СТОРОНЫ.
//
// Вынесен отдельной функцией НАМЕРЕННО: инъекция обязана судить тем же кодом,
// которым судится дерево. Судья, у которого своя копия логики, доказывает
// свойство своей копии.
func adjudicateDoors(files map[string]int, roster map[string]doorRuling) []string {
	var findings []string

	names := make([]string, 0, len(files))
	for f := range files {
		names = append(names, f)
	}
	sort.Strings(names)

	for _, f := range names {
		got := files[f]
		ruling, declared := roster[f]
		if !declared {
			findings = append(findings, fmt.Sprintf(
				"ОБХОД БЕЗ ВЕРДИКТА: %s — %d мест(а) записи в движок, вердикта нет. "+
					"Состояние движка обязано быть свёрткой журнала (0098): либо путь кладёт строку "+
					"kacho_iam.fga_outbox, либо он снимается, либо исключение объявляется здесь с причиной и предикатом",
				f, got))
			continue
		}
		if ruling.Places != got {
			findings = append(findings, fmt.Sprintf(
				"ЧИСЛО МЕСТ РАЗОШЛОСЬ: %s — в ведомости %d, в дереве %d. "+
					"Число пиннится, чтобы новая запись в уже отсуженном файле не прошла молча: "+
					"перепроверь путь и обнови число вместе с обоснованием",
				f, ruling.Places, got))
		}
		if ruling.Verdict == doorJournalBacked && ruling.JournalAt == "" {
			findings = append(findings, fmt.Sprintf(
				"ВЕРДИКТ БЕЗ ТОЧКИ ЭМИССИИ: %s объявлен %q, но не назвал, где кладётся строка журнала",
				f, doorJournalBacked))
		}
		if ruling.Verdict == doorException && ruling.Predicate == "" {
			findings = append(findings, fmt.Sprintf(
				"ИСКЛЮЧЕНИЕ БЕЗ ПРЕДИКАТА СНЯТИЯ: %s — послабление без предиката бессрочно, "+
					"а бессрочное послабление переживает свой предмет", f))
		}
	}

	rulings := make([]string, 0, len(roster))
	for f := range roster {
		rulings = append(rulings, f)
	}
	sort.Strings(rulings)
	for _, f := range rulings {
		if _, present := files[f]; !present {
			findings = append(findings, fmt.Sprintf(
				"ВЕРДИКТ БЕЗ МЕСТА: %s больше не пишет в движок — запись в ведомости, которой нечего "+
					"исключать, унаследует следующая слепая зона. Снять вместе с предметом", f))
		}
	}
	return findings
}

// writePlacesByFile — места РОДА ЗАПИСИ, сгруппированные по файлу.
func writePlacesByFile(c *engineplaces.Census) map[string]int {
	out := map[string]int{}
	for _, p := range c.Places {
		if p.Kind == engineplaces.KindWriteStore {
			out[p.File]++
		}
	}
	return out
}

// ── Пробы дерева ─────────────────────────────────────────────────────────────

// TestJournalDoor_EveryEngineWriteIsAdjudicated — гейт.
func TestJournalDoor_EveryEngineWriteIsAdjudicated(t *testing.T) {
	c := buildTree(t)
	if c.Void() {
		t.Fatalf("перепись НЕГОДНА — судить нечем, и это не «ноль находок»: %v", c.Errors)
	}

	byFile := writePlacesByFile(c)

	// ПРЕДПОСЫЛКА. «Мест записи ноль» на этом дереве означает, что перепись
	// перестала их видеть, а не что писать в движок стало неоткуда. Молчаливый
	// успех здесь был бы ровно тем классом, против которого гейт и заведён.
	if len(byFile) == 0 {
		t.Fatal("перепись не нашла НИ ОДНОГО места записи в движок — предпосылка не выполнена: " +
			"в дереве заведомо есть дренаж журнала. Гейт судил бы пустоту")
	}

	total := 0
	for _, n := range byFile {
		total += n
	}
	// ОБЪЁМ ОСМОТРЕННОГО — печатается ВСЕГДА: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	t.Logf("осмотрено: мест записи в движок %d в %d файлах; всего мест обращения %d в %d файлах; вердиктов в ведомости %d",
		total, len(byFile), len(c.Places), c.FileCount(), len(engineWriteDoors))
	for _, v := range []string{doorDrain, doorJournalBacked, doorException} {
		n := 0
		for f, r := range engineWriteDoors {
			if r.Verdict == v {
				n += byFile[f]
			}
		}
		t.Logf("  %-32s %d мест(а)", v, n)
	}

	if findings := adjudicateDoors(byFile, engineWriteDoors); len(findings) > 0 {
		t.Errorf("состояние движка перестало быть свёрткой журнала — находок %d:\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestJournalDoor_JournalBackedRulingNamesALiveEmission — вердикт
// «строка журнала со-коммичена» обязан называть ЖИВУЮ точку эмиссии.
//
// Без этого вердикт переживает то, что им обозначалось: эмиссию снимут или
// переименуют, место записи останется, а ведомость продолжит утверждать, что
// журнал его покрывает.
//
// Символ ищется РАЗБОРОМ AST, а не подстрокой: этот самый файл называет
// `emitFGAOutbox` в ведомости, и предикат по тексту зеленел бы на собственном
// объяснении.
func TestJournalDoor_JournalBackedRulingNamesALiveEmission(t *testing.T) {
	root := repoRoot(t)
	checked := 0

	for file, ruling := range engineWriteDoors {
		if ruling.Verdict != doorJournalBacked {
			continue
		}
		at := ruling.JournalAt
		idx := strings.LastIndex(at, ":")
		if idx < 0 {
			t.Errorf("%s: точка эмиссии %q не имеет формы «путь:символ»", file, at)
			continue
		}
		path, symbol := at[:idx], at[idx+1:]

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(path)), nil, 0)
		if err != nil {
			t.Errorf("%s: точка эмиссии %s не разбирается — вердикт ссылается в пустоту: %v", file, path, err)
			continue
		}
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if ok && fd.Name != nil && fd.Name.Name == symbol {
				found = true
			}
			return !found
		})
		if !found {
			t.Errorf("%s: вердикт %q называет точку эмиссии %s, но функции %q там НЕТ — "+
				"либо эмиссию сняли (тогда путь стал обходом), либо переименовали (тогда вердикт лжёт)",
				file, doorJournalBacked, path, symbol)
			continue
		}
		checked++
	}

	if checked == 0 {
		t.Fatal("не проверено НИ ОДНОЙ точки эмиссии — либо вердиктов такого рода не осталось, " +
			"либо проба перестала их видеть; и то и другое обязано быть замечено")
	}
	t.Logf("осмотрено: точек эмиссии журнала проверено %d", checked)
}

// engineWriteEndpoint — признак ЗАПИСИ в движок из исполняемого файла.
var engineWriteEndpoint = regexp.MustCompile(`/stores/[^"'\s]*/write`)

// shellExecutablePart срезает КОММЕНТАРИИ shell, не трогая содержимое строк.
//
// Наивное «обрезать по первому #» здесь НЕВЕРНО, и это стоило первой редакции
// пробы: решётка — часть кортежа (`cluster:<id>#viewer@user:*`), поэтому такой
// предикат уничтожал ровно тот текст, ради которого читал файл, и объявлял
// «вызовов записи не найдено» на файле, где их два. Комментарием решётка
// является, только если стоит в начале строки либо отделена пробелом.
func shellExecutablePart(body string) string {
	var out strings.Builder
	for _, line := range strings.Split(body, "\n") {
		// Перебираются ВСЕ решётки, а не первая: решётка кортежа стоит раньше
		// настоящего комментария, и остановка на первой оставляла бы хвост
		// строки неразобранным.
		for i := 0; i < len(line); i++ {
			if line[i] != '#' {
				continue
			}
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				line = line[:i]
				break
			}
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// TestJournalDoor_NonGoWritersAreAdjudicated — перепись `engineplaces` видит
// только Go. Скрипт, шлющий ту же запись, обходит и журнал, и её.
//
// Отбираются ИСПОЛНЯЕМЫЕ файлы (расширение `.sh` либо шебанг), а не всё, где
// встретился адрес: страница документации, называющая эндпоинт, ничего не пишет,
// и считать её писателем значило бы мерить упоминания вместо предмета.
func TestJournalDoor_NonGoWritersAreAdjudicated(t *testing.T) {
	root := repoRoot(t)
	files := map[string]int{}
	scanned, executable := 0, 0

	// Состав дерева спрашивается У ИНДЕКСА, а не у диска: обход диска не знает
	// правил игнорирования и потому судит чужой рабочий каталог — произведённые
	// файлы, чужие копии, остатки прогонов. Требование держит гейт
	// `internal/repohygiene.TestTreeWalkersAskTheIndex`, и он поймал ровно это
	// место в первом же прогоне после заведения гейта.
	out, err := gitenv.Command(root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || strings.HasSuffix(rel, ".go") {
			continue // пустая запись либо покрыто переписью
		}
		raw, rerr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if rerr != nil {
			continue // файл в индексе, но снят с диска — не предмет этого гейта
		}
		scanned++
		body := string(raw)
		if !strings.HasSuffix(rel, ".sh") && !strings.HasPrefix(body, "#!") {
			continue
		}
		executable++
		// Комментарии срезаются: гейт обязан читать ИСПОЛНЯЕМУЮ часть, иначе
		// краснеет на объяснении рядом с самой защитой.
		if engineWriteEndpoint.MatchString(shellExecutablePart(body)) {
			files[filepath.ToSlash(rel)]++
		}
	}

	if executable == 0 {
		t.Fatal("в дереве не нашлось НИ ОДНОГО исполняемого файла — обход не читал того, " +
			"что обязан был читать: «ноль находок» здесь означало бы «ноль прочитанного»")
	}
	t.Logf("осмотрено: файлов прочитано %d, из них исполняемых %d; пишут в движок %d; вердиктов %d",
		scanned, executable, len(files), len(nonGoWriteDoors))

	if findings := adjudicateDoors(files, nonGoWriteDoors); len(findings) > 0 {
		t.Errorf("исполняемый файл пишет в движок мимо журнала — находок %d:\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestJournalDoor_BootstrapExceptionIsBoundedToItsDeclaredTuples — послабление
// посеву ОГРАНИЧЕНО набором, а не выдано на предъявителя.
//
// Это и есть предикат самоистечения бутстрапа: «посев вправе писать мимо
// журнала» без границы означало бы, что завтра он напишет что угодно и перепись
// «движок против журнала» перестанет сходиться, никого не разбудив.
func TestJournalDoor_BootstrapExceptionIsBoundedToItsDeclaredTuples(t *testing.T) {
	const script = "deploy/helm/umbrella/charts/openfga-bootstrap/files/bootstrap.sh"
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(script)))
	if err != nil {
		t.Fatalf("посев чарта не прочитан — границу послабления нечем проверить: %v", err)
	}

	// Ярлык — первый аргумент `fga_write`; он и называет кортеж. Берётся из
	// ИСПОЛНЯЕМОЙ части: объявление самой функции и объяснения рядом с ней
	// содержат то же слово.
	var got []string
	for _, line := range strings.Split(shellExecutablePart(string(raw)), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "fga_write ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "fga_write "))
		if len(rest) > 1 && rest[0] == '"' {
			if end := strings.Index(rest[1:], `"`); end >= 0 {
				got = append(got, rest[1:1+end])
			}
		}
	}
	sort.Strings(got)

	want := append([]string(nil), bootstrapDeclaredTuples...)
	sort.Strings(want)

	if len(got) == 0 {
		t.Fatal("в посеве не найдено НИ ОДНОГО вызова записи — либо посев перестал писать " +
			"(тогда послабление снимается), либо проба перестала его читать")
	}
	t.Logf("осмотрено: кортежей, которые посев ставит мимо журнала — %d (объявлено %d)", len(got), len(want))

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("посев чарта ставит мимо журнала НЕ ТОТ набор кортежей, что объявлен.\n"+
			"в дереве:\n  %s\nобъявлено:\n  %s\n"+
			"Расширение набора — расширение обхода: перепись «движок против журнала» перестанет "+
			"сходиться на разницу, и разбирать её будут в правах, а не в наполнении",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestJournalDoor_CIRunsThisGate — вторая половина шва: гейт, которого конвейер
// не зовёт, стоит ровно столько же, сколько гейт, ничего не проверяющий.
//
// Пакет пропускает себя под кратким режимом (типизирует всё дерево), поэтому
// быстрая джоба до него не доходит, а отбор интеграционной смотрит внутрь
// `services/<svc>/internal/...`, куда `tools/` не входит вовсе. Шаг конвейера у
// пакета ОДИН, и он идёт с фильтром по имени: гейт, чьё имя из фильтра выпало,
// перестаёт исполняться МОЛЧА — прогон остаётся зелёным, потому что его никто не
// запускал. Проверяется поэтому не «шаг есть», а «фильтр называет ЭТОТ гейт».
func TestJournalDoor_CIRunsThisGate(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci.yaml"))
	if err != nil {
		t.Fatalf("ci.yaml не прочитан — провязку нечем проверить: %v", err)
	}
	text := string(body)

	const pkg = "go test ./tools/authzenginecensus/engineplaces/"
	var step string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, pkg) {
			step = strings.TrimSpace(line)
			break
		}
	}
	if step == "" {
		t.Fatalf("ci.yaml не зовёт пакет %s вовсе — гейт не исполняется НИГДЕ", pkg)
	}
	if !strings.Contains(step, "TestJournalDoor") {
		t.Fatalf("шаг конвейера зовёт пакет, но его фильтр НЕ называет TestJournalDoor — "+
			"гейт «в движок пишут только через строку журнала» не исполняется, и его зелёное "+
			"не значит ничего.\nшаг: %s", step)
	}
	if strings.Contains(step, "-short") {
		t.Fatalf("шаг зовёт пакет с -short, то есть ровно с тем пропуском, ради обхода "+
			"которого он и заведён: %s", step)
	}
}
