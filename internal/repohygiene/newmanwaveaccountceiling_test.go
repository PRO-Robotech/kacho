// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanwaveaccountceiling_test.go — гейт: волна церемонии обязана держать ЗАПАС
// под потолком аккаунтов одной личности, а не жить в одной единице от него.
//
// # Предмет
//
// Потолок на число аккаунтов ОДНОЙ личности — продуктовая величина
// (`iam.account`, посев миграции 484002). Волна церемонии
// (`services/iam/tests/newman/scripts/run-ceremony.sh`) идёт целиком ПОД ОДНИМ
// человеком: машинный посев человеческого предъявителя не производит, поэтому все
// её коллекции авторизуются одним и тем же живым входом. Значит все аккаунты,
// которые волна заводит, ложатся на ОДИН счётчик — и складываются с теми, которые
// у этого человека уже есть до первой коллекции.
//
// Пока запас равен одной единице, следующее одновременно живое создание аккаунта
// ГДЕ УГОДНО в волне упирается в потолок. И упирается не там, где причина: отказ
// приходит первому, кто попросил после исчерпания, а он про потолок ничего не знает
// и падает утверждением про своё. Наблюдалось: 46 упавших утверждений из одного
// корня, а виновным выглядел кейс, который просто оказался следующим.
//
// # Что именно утверждается
//
// Пик одновременно живых аккаунтов человека церемонии, посчитанный ПО ПОРЯДКУ
// ВОЛНЫ, строго МЕНЬШЕ потолка минус один. То есть запас не меньше двух: одной
// единицы недостаточно — она означает, что любой новый кейс с аккаунтом ломает
// волну, и ломает её не у себя.
//
// # Почему счёт идёт по СГЕНЕРИРОВАННЫМ коллекциям, а не по `cases/*.py`
//
// Коллекции — отслеживаемый артефакт дерева (`git ls-files` их видит), и именно их
// исполняет newman. Источник кейсов до коллекции проходит через генератор, который
// добавляет шаги (поллинг операции, надёжное удаление, обёртки повтора) и задаёт
// порядок шагов внутри кейса, — то есть по источнику пришлось бы ВОСПРОИЗВЕСТИ
// генератор, заведя его вторую копию. Читается при этом ИСПОЛНЯЕМАЯ часть: скрипты
// очищаются от `//`-комментариев (`stripJSComments`), предъявитель берётся из
// строки кода `const __t = pm.environment.get('…')`, а не из комментария рядом с
// ней, метод и путь — из тела запроса.
//
// # Порядок волны НЕ выписывается
//
// Он выводится тем же объявлением, которым его выводит прогонщик:
// `tests/authz-fixtures/ceremony_credentials.py --stems`. Выписанный здесь перечень
// был бы второй копией объявления и разошёлся бы с ним молча — в этом дереве такой
// перечень уже жил в трёх копиях и уже расходился.
//
// # Модель счёта
//
//   - СПИСАНИЕ (+1) — шаг `POST /iam/v1/accounts`, который ЗАХВАТЫВАЕТ id аккаунта
//     из `metadata.accountId` в переменную окружения. Захват и есть признак того,
//     что аккаунт создан и им ещё будут пользоваться;
//   - ВОЗВРАТ (−1) — шаг `DELETE /iam/v1/accounts/{{V}}`, где `V` — переменная,
//     захваченная более ранним списанием. Возврат относится к плательщику
//     СПИСАНИЯ, а не к тому, кто удаляет: носитель потолка — личность ВЛАДЕЛЬЦА
//     аккаунта, и то, что уборка идёт предъявителем с поднятым уровнем того же
//     человека, счётчика не касается;
//   - НЕ СЧИТАЕТСЯ шаг, чей отказ УТВЕРЖДЁН: синхронный (все допускаемые статусы
//     вне 2xx) либо асинхронный (операция ЭТОГО шага опрошена и от неё утверждён
//     `error.code`). Пример второго — создание с занятым именем: край отвечает 200,
//     операция завершается `ALREADY_EXISTS`, строки не остаётся;
//   - НЕ ОТНЕСЁННЫЙ `POST /iam/v1/accounts` — ни захвата, ни утверждённого отказа —
//     это НАХОДКА, а не ноль. Гейт, молча пропустивший такой шаг, занижал бы пик
//     ровно там, где занижение опасно.
//
// Пара «шаг, опубликовавший `opId`» ↔ «шаг, утверждающий `error.code`» берётся
// per-step, а не per-case: в одном кейсе законно соседствуют удаление, которому
// отказано, и уборка, которая проходит (`IAM-ACC-RD-DL-NONEMPTY-RESTRICT-NEG`).
//
// # База — аккаунты, которые есть ДО первой коллекции
//
//  1. личный аккаунт первого входа: `UpsertFromIdentity` заводит его, когда у
//     личности НОЛЬ аккаунтов (продукт, `services/iam/internal`);
//  2. аккаунт, который человек церемонии заводит САМ в посеве
//     (`tests/authz-fixtures/prodseed_ceremony.py`, стадия 8б) и который волна не
//     удаляет.
//
// Оба слагаемых несут проверку своего производителя (см. `waveBaselineAccounts`):
// исчезнет первый — гейт скажет, что объявление базы устарело, а не тихо посчитает
// на единицу меньше; появится третий посевной — счёт производителей вырастет сам.
//
// # Чем это НЕ является
//
// Гейт не судит стенд и не считает аккаунты в базе. Он считает то, что ЗАЯВЛЯЮТ
// коллекции, — и потому остаётся свойством коммита, а не прогона.
//
// # Премисса, которую он проверяет, а не подразумевает
//
// Все списания волны идут под ОДНИМ предъявителем, поэтому складывать их в один
// счётчик законно. Появится второй плательщик — гейт скажет об этом прямо: его
// число станет завышенным (если это разные личности) либо останется верным (если
// это тот же человек с поднятым уровнем входа), и различить эти два случая по
// дереву он не берётся. Оба исхода требуют правки предиката, а не молчания.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

const (
	waveSuiteDir      = "services/iam/tests/newman"
	waveDeclRel       = "tests/authz-fixtures/ceremony_credentials.py"
	waveCeremonySeed  = "tests/authz-fixtures/prodseed_ceremony.py"
	waveIAMMigrations = "services/iam/internal/migrations"
	waveIAMInternal   = "services/iam/internal"
	waveQuotaKind     = "iam.account"

	// Личный аккаунт первого входа. Производитель — литерал имени в прод-коде iam;
	// он же и проверяется, потому что комментарии про эту ветку пережили бы её
	// снятие, а литерал — нет.
	wavePersonalAccountLiteral = "personal-cloud-"
)

// ─────────────────────────────────────────────────────────────────────────────
// Распознаватели. Все — по ИСПОЛНЯЕМОЙ части.
// ─────────────────────────────────────────────────────────────────────────────

var (
	// Предъявитель шага. Берётся из КОДА, а не из комментария `// per-step auth: …`
	// над ним: комментарий переживёт смену строки, и гейт приписал бы шагу
	// принципала, под которым тот больше не ходит.
	reWaveStepAuth = regexp.MustCompile(`const __t = pm\.environment\.get\('([A-Za-z0-9_]+)'\)`)

	// Захват id созданного аккаунта в переменную окружения.
	reWaveAccountCapture = regexp.MustCompile(
		`j\.metadata\.accountId\s*\)\s*;\s*if\s*\([^;]*\)\s*pm\.environment\.set\('([A-Za-z0-9_]+)'`)

	// Утверждение об ОШИБКЕ операции — форма, которую производит `assert_op_error`.
	reWaveOpErrorAssert = regexp.MustCompile(`j\.error\s*&&\s*j\.error\.code`)

	// Шаг публикует `opId` — то есть ИМЕННО его операцию опрашивают следующие шаги.
	reWaveSetOpID = regexp.MustCompile(`pm\.environment\.set\('opId'`)

	// Путь удаления аккаунта по переменной.
	reWaveAccountDelete = regexp.MustCompile(`^/iam/v1/accounts/\{\{([A-Za-z0-9_]+)\}\}$`)

	// Ведущая подстановка базового адреса в сыром URL.
	reWaveURLPrefix = regexp.MustCompile(`^\{\{[A-Za-z0-9_]+\}\}`)

	// Посев величины: строка каталога пределов.
	reWaveLimitInsert = regexp.MustCompile(`(?is)INSERT\s+INTO\s+kacho_iam\.limits\b[^;]*?VALUES(.*?);`)
	reWaveLimitTuple  = regexp.MustCompile(
		`\(\s*'([^']*)'\s*,\s*'([^']*)'\s*,\s*'([^']*)'\s*,\s*'([^']*)'\s*,\s*(-?\d+)\s*\)`)
	// Любая ДРУГАЯ правка каталога пределов — гейт обязан о ней знать, а не считать
	// величину по первой найденной строке посева.
	reWaveLimitMutate = regexp.MustCompile(`(?is)(UPDATE|DELETE\s+FROM)\s+kacho_iam\.limits\b[^;]*;`)

	// Посев церемонии заводит аккаунт человеком.
	reWaveSeedAccountPost = regexp.MustCompile(`_req\(\s*"POST"\s*,\s*f?"\{PUBLIC\}/iam/v1/accounts"`)
)

// ─────────────────────────────────────────────────────────────────────────────
// Разбор
// ─────────────────────────────────────────────────────────────────────────────

// waveCollection — одна коллекция волны в ТОМ порядке, в котором её гоняет прогонщик.
type waveCollection struct {
	Stem string // имя без расширения — им прогонщик её и адресует
	Rel  string // координата в дереве (она же координата находки)
	Body []byte
}

type waveQuotaScan struct {
	Collections  int
	Steps        int
	Charges      int
	Releases     int
	Baseline     int
	Peak         int
	PeakAt       string            // шаг, на котором достигнут пик
	PerStem      map[string]int    // коллекция → пик внутри неё (с базой)
	Payers       map[string]int    // предъявитель-плательщик → сколько списаний
	Ledger       []string          // ход счётчика, для переписи
	Unattributed []string          // POST, который не отнести ни к списанию, ни к отказу
	LiveAtEnd    []string          // захваченные и не возвращённые
	SawCharge    bool              // распознаватель списания подтверждён на живых данных
	SawRefusal   bool              // распознаватель утверждённого отказа подтверждён
	chargedBy    map[string]string // переменная → предъявитель, сделавший списание
}

// scanWaveAccountQuota — ход счётчика аккаунтов одной личности по порядку волны.
//
// Ошибка возвращается только на неразбираемом входе: «коллекция не читается» не
// имеет права стать «нулём находок».
func scanWaveAccountQuota(colls []waveCollection, baseline int) (waveQuotaScan, error) {
	out := waveQuotaScan{
		Baseline:  baseline,
		Peak:      baseline,
		PerStem:   map[string]int{},
		Payers:    map[string]int{},
		chargedBy: map[string]string{},
	}
	live := baseline
	liveVars := map[string]bool{}

	for _, c := range colls {
		var coll pmCollection
		if err := json.Unmarshal(c.Body, &coll); err != nil {
			return out, fmt.Errorf("%s: коллекция не разбирается: %w", c.Rel, err)
		}
		out.Collections++
		steps := flattenItems(coll.Item, nil)
		stemPeak := live

		// Первый проход: чья операция объявлена УПАВШЕЙ.
		failedOp := map[int]bool{}
		lastOp := -1
		for i, s := range steps {
			code := stripJSComments(stepScript(s, "test"))
			if reWaveSetOpID.MatchString(code) {
				lastOp = i
			}
			if reWaveOpErrorAssert.MatchString(code) && lastOp >= 0 {
				failedOp[lastOp] = true
			}
		}

		for i, s := range steps {
			out.Steps++
			if s.Request == nil {
				continue
			}
			method := strings.ToUpper(strings.TrimSpace(s.Request.Method))
			path := waveRequestPath(s)
			test := stripJSComments(stepScript(s, "test"))
			refused := expectsRefusal(s) || failedOp[i]
			if refused {
				out.SawRefusal = true
			}

			switch {
			case method == "POST" && strings.TrimRight(path, "/") == "/iam/v1/accounts":
				m := reWaveAccountCapture.FindStringSubmatch(test)
				switch {
				case m != nil && !refused:
					v := m[1]
					payer := waveStepPrincipal(s)
					live++
					out.Charges++
					out.SawCharge = true
					out.Payers[payer]++
					liveVars[v] = true
					out.chargedBy[v] = payer
					out.Ledger = append(out.Ledger, fmt.Sprintf(
						"%3d  +1  %-24s %-58s %-24s %s", live, c.Stem, s.Name, payer, v))
					if live > out.Peak {
						out.Peak = live
						out.PeakAt = c.Rel + " :: " + s.Name
					}
					if live > stemPeak {
						stemPeak = live
					}
				case refused:
					// Отказ утверждён — строки не остаётся, счётчик не двигается.
				default:
					out.Unattributed = append(out.Unattributed, fmt.Sprintf(
						"%s :: %s — POST /iam/v1/accounts без захвата id и без "+
							"утверждённого отказа", c.Rel, s.Name))
				}

			case method == "DELETE":
				m := reWaveAccountDelete.FindStringSubmatch(path)
				if m == nil || !liveVars[m[1]] || refused {
					continue
				}
				v := m[1]
				delete(liveVars, v)
				live--
				out.Releases++
				out.Ledger = append(out.Ledger, fmt.Sprintf(
					"%3d  -1  %-24s %-58s %-24s %s", live, c.Stem, s.Name,
					waveStepPrincipal(s), v))
			}
		}
		out.PerStem[c.Stem] = stemPeak
	}

	for v := range liveVars {
		out.LiveAtEnd = append(out.LiveAtEnd, fmt.Sprintf("%s (списан под %s)", v, out.chargedBy[v]))
	}
	sort.Strings(out.LiveAtEnd)
	return out, nil
}

// waveRequestPath — путь запроса без подстановки базового адреса и без строки
// запроса. Читается тело запроса, а не текст скрипта.
func waveRequestPath(it pmItem) string {
	if it.Request == nil {
		return ""
	}
	raw := rawURL(it.Request.URL)
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	return reWaveURLPrefix.ReplaceAllString(raw, "")
}

// waveStepPrincipal — под каким предъявителем идёт шаг. Шаг без строки чтения
// предъявителя идёт анонимно — так его и называем, чтобы «неизвестно» не смешалось
// с «под человеком».
func waveStepPrincipal(it pmItem) string {
	if m := reWaveStepAuth.FindStringSubmatch(stripJSComments(stepScript(it, "prerequest"))); m != nil {
		return m[1]
	}
	return "anonymous"
}

// ─────────────────────────────────────────────────────────────────────────────
// Потолок — ИЗ ДЕРЕВА
// ─────────────────────────────────────────────────────────────────────────────

// waveAccountCeiling — величина потолка `iam.account`, прочитанная у миграций.
//
// Числом в гейте она быть не может: тогда смена продуктовой величины разошлась бы
// с проверкой молча, и гейт продолжал бы стеречь запас под потолком, которого нет.
func waveAccountCeiling(t *testing.T, root string) (int, string) {
	t.Helper()
	dir := filepath.Join(root, waveIAMMigrations)
	files, err := treecorpus.UnderWithSuffix(dir, ".sql")
	if err != nil {
		t.Fatalf("состав миграций iam: %v — потолок неоткуда взять, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	if len(files) == 0 {
		t.Fatalf("под %s нет ни одной отслеживаемой миграции — предпосылка чтения "+
			"потолка сломана", waveIAMMigrations)
	}
	type located struct {
		rel string
		waveLimitSeed
	}
	var seeds []located
	var mutations []string
	for _, abs := range files {
		body, rerr := os.ReadFile(abs) //nolint:gosec // путь из индекса git
		if rerr != nil {
			t.Fatalf("читаю %s: %v", abs, rerr)
		}
		rel, _ := filepath.Rel(root, abs)
		rel = filepath.ToSlash(rel)
		got, mutates, perr := waveLimitsInMigration(string(body), waveQuotaKind)
		if perr != nil {
			t.Fatalf("%s: %v", rel, perr)
		}
		for _, sd := range got {
			seeds = append(seeds, located{rel: rel, waveLimitSeed: sd})
		}
		if mutates {
			mutations = append(mutations, rel)
		}
	}
	if len(mutations) > 0 {
		t.Fatalf("каталог пределов правят и после посева (%v) — гейт не вправе "+
			"считать потолок %q по строке посева, пока не прочитана эта правка",
			mutations, waveQuotaKind)
	}
	if len(seeds) != 1 {
		var where []string
		for _, s := range seeds {
			where = append(where, s.rel)
		}
		t.Fatalf("строк посева потолка %q в секциях Up найдено %d (%v), а обязана быть "+
			"ровно одна — иначе действующая величина неизвестна",
			waveQuotaKind, len(seeds), where)
	}
	if seeds[0].Scope != "DEFAULT" {
		t.Fatalf("%s: потолок %q посеян в области %q, а не DEFAULT — человек церемонии "+
			"не принадлежит ни аккаунту, ни проекту, поэтому иная область к нему "+
			"не применима", seeds[0].rel, waveQuotaKind, seeds[0].Scope)
	}
	if seeds[0].Value < 2 {
		t.Fatalf("%s: потолок %q объявлен равным %d — при таком значении «запас ≥ 2» "+
			"невыразим, и предикат гейта теряет предмет",
			seeds[0].rel, waveQuotaKind, seeds[0].Value)
	}
	return seeds[0].Value, seeds[0].rel
}

// waveLimitSeed — посевная строка каталога пределов.
type waveLimitSeed struct {
	Scope string
	Value int
}

// waveLimitsInMigration — что ОДНА миграция объявляет про потолок вида kind.
//
// Читается только секция `+goose Up` и только вне `--`-комментариев: секция отката
// описывает состояние, к которому дерево возвращается, а комментарий — намерение.
// Обе половины уже наблюдались в этом каталоге: `353001` несёт посев величины
// ИМЕННО в откате, и гейт, читающий файл целиком, засчитал бы его как действующий.
//
// Второй возврат — признак того, что каталог правят не посевом (UPDATE/DELETE по
// тому же виду). Вычислять по такой миграции действующую величину гейт не берётся:
// он говорит об этом вслух, а не выбирает первую попавшуюся строку.
func waveLimitsInMigration(src, kind string) ([]waveLimitSeed, bool, error) {
	up := stripSQLComments(gooseUpSection(src))
	var seeds []waveLimitSeed
	for _, stmt := range reWaveLimitInsert.FindAllStringSubmatch(up, -1) {
		for _, tup := range reWaveLimitTuple.FindAllStringSubmatch(stmt[1], -1) {
			if tup[4] != kind {
				continue
			}
			n, err := strconv.Atoi(tup[5])
			if err != nil {
				return nil, false, fmt.Errorf("величина потолка %q не число", tup[5])
			}
			seeds = append(seeds, waveLimitSeed{Scope: tup[2], Value: n})
		}
	}
	mutates := false
	for _, stmt := range reWaveLimitMutate.FindAllString(up, -1) {
		if strings.Contains(stmt, "'"+kind+"'") {
			mutates = true
		}
	}
	return seeds, mutates, nil
}

// gooseUpSection — часть файла до маркера отката. Маркер ищется ДО снятия
// комментариев: он сам записан комментарием.
func gooseUpSection(src string) string {
	idx := strings.Index(src, "-- +goose Down")
	if idx < 0 {
		return src
	}
	return src[:idx]
}

// stripSQLComments — снять `--`-комментарии, не трогая строковые литералы.
func stripSQLComments(src string) string {
	var b strings.Builder
	inStr := false
	rs := []rune(src)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if inStr {
			b.WriteRune(c)
			if c == '\'' {
				inStr = false
			}
			continue
		}
		if c == '\'' {
			inStr = true
			b.WriteRune(c)
			continue
		}
		if c == '-' && i+1 < len(rs) && rs[i+1] == '-' {
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			b.WriteRune('\n')
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// База — с проверкой ОБОИХ производителей
// ─────────────────────────────────────────────────────────────────────────────

// waveBaselineAccounts — сколько аккаунтов у человека церемонии есть ДО первой
// коллекции волны, и как это число получено.
//
// Чего эта проверка НЕ покрывает, и это названо прямо: место запроса в посеве
// считается один раз, а вызывается столько раз, сколько его позвали. Сегодня место
// одно и зовут его один раз (`stage_own_account` из `main`); появится цикл — счёт
// занизится, и поймает это уже не гейт, а прогон.
func waveBaselineAccounts(t *testing.T, root string) (int, string) {
	t.Helper()

	personal := countIAMPersonalAccountProducers(t, root)
	if personal == 0 {
		t.Fatalf("в прод-коде %s не найдено ни одного производителя личного аккаунта "+
			"первого входа (строковый литерал %q) — объявление базы устарело: либо "+
			"ветка снята и база стала меньше, либо её переименовали и гейт больше её "+
			"не видит. Молча считать на единицу меньше нельзя",
			waveIAMInternal, wavePersonalAccountLiteral)
	}

	seedPath := filepath.Join(root, waveCeremonySeed)
	body, err := os.ReadFile(seedPath) //nolint:gosec // путь собран из констант гейта
	if err != nil {
		t.Fatalf("читаю посев церемонии %s: %v — без него база волны неизвестна",
			waveCeremonySeed, err)
	}
	seedAccounts := len(reWaveSeedAccountPost.FindAllString(stripPythonComments(string(body)), -1))
	if seedAccounts == 0 {
		t.Fatalf("в %s не найдено ни одного запроса создания аккаунта — распознаватель "+
			"не подтверждён на живых данных, поэтому «база = %d» тут значит «не смотрел»",
			waveCeremonySeed, personal)
	}

	return personal + seedAccounts, fmt.Sprintf(
		"личный аккаунт первого входа %d (производителей литерала %q в %s) + "+
			"посев церемонии %d (запросов создания в %s)",
		personal, wavePersonalAccountLiteral, waveIAMInternal, seedAccounts, waveCeremonySeed)
}

// countIAMPersonalAccountProducers — сколько раз прод-код iam ИМЕНУЕТ личный
// аккаунт. Считается по разобранному дереву Go (строковые литералы), а не поиском
// подстроки: то же имя стоит в трёх комментариях того же файла, и подстрочный
// счёт остался бы верным после снятия самой ветки.
func countIAMPersonalAccountProducers(t *testing.T, root string) int {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, waveIAMInternal), ".go")
	if err != nil {
		t.Fatalf("состав прод-дерева iam: %v", err)
	}
	n := 0
	fset := token.NewFileSet()
	for _, abs := range files {
		if strings.HasSuffix(abs, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, abs, nil, 0)
		if perr != nil {
			continue // синтаксически негодный файл — предмет другого гейта
		}
		ast.Inspect(f, func(nd ast.Node) bool {
			lit, ok := nd.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if v, uerr := strconv.Unquote(lit.Value); uerr == nil && v == wavePersonalAccountLiteral {
				n++
			}
			return true
		})
	}
	return n
}

// stripPythonComments — снять `#`-комментарии вне строковых литералов.
func stripPythonComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		inS, inD := false, false
		cut := -1
		for i, c := range line {
			switch {
			case c == '\'' && !inD:
				inS = !inS
			case c == '"' && !inS:
				inD = !inD
			case c == '#' && !inS && !inD:
				cut = i
			}
			if cut >= 0 {
				break
			}
		}
		if cut >= 0 {
			line = line[:cut]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Волна — ПОРЯДОК И СОСТАВ ИЗ ОБЪЯВЛЕНИЯ
// ─────────────────────────────────────────────────────────────────────────────

// waveCollectionsInOrder — коллекции волны церемонии в порядке прогона.
func waveCollectionsInOrder(t *testing.T, root string) []waveCollection {
	t.Helper()
	python := pythonInterpreter(t)
	decl := filepath.Join(root, waveDeclRel)
	if _, err := os.Stat(decl); err != nil {
		t.Fatalf("нет объявления волны %s (%v) — порядок неоткуда вывести, а выписать "+
			"его здесь значило бы завести вторую копию перечня", waveDeclRel, err)
	}
	cmd := exec.Command(python, decl, "--root", root, "--suite", waveSuiteDir, "--stems")
	cmd.Dir = root
	outBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s --stems: %v — набор волны не выведен, это не «волна пуста»",
			waveDeclRel, err)
	}
	var stems []string
	for _, l := range strings.Split(string(outBytes), "\n") {
		if s := strings.TrimSpace(l); s != "" {
			stems = append(stems, s)
		}
	}
	if len(stems) == 0 {
		t.Fatalf("набор волны вывелся ПУСТЫМ. Это не «церемония больше не нужна» — " +
			"это «ничего не прочитано»: коллекции не сгенерированы либо объявление " +
			"разошлось с деревом")
	}

	tracked, err := treecorpus.UnderWithSuffix(
		filepath.Join(root, waveSuiteDir, "collections"), ".postman_collection.json")
	if err != nil {
		t.Fatalf("состав коллекций набора iam: %v", err)
	}
	byStem := map[string]string{}
	for _, abs := range tracked {
		base := filepath.Base(abs)
		byStem[strings.TrimSuffix(base, ".postman_collection.json")] = abs
	}

	out := make([]waveCollection, 0, len(stems))
	for _, stem := range stems {
		abs, ok := byStem[stem]
		if !ok {
			t.Fatalf("волна называет коллекцию %q, а отслеживаемого файла под неё нет — "+
				"прогонщик доложит по ней «(no-report)», и вердикта у неё не будет "+
				"вовсе", stem)
		}
		body, rerr := os.ReadFile(abs) //nolint:gosec // путь из индекса git
		if rerr != nil {
			t.Fatalf("читаю %s: %v", abs, rerr)
		}
		rel, _ := filepath.Rel(root, abs)
		out = append(out, waveCollection{Stem: stem, Rel: filepath.ToSlash(rel), Body: body})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт
// ─────────────────────────────────────────────────────────────────────────────

// TestCeremonyWaveKeepsHeadroomUnderTheAccountCeiling — по дереву.
func TestCeremonyWaveKeepsHeadroomUnderTheAccountCeiling(t *testing.T) {
	root := repoRoot(t)

	ceiling, ceilingRel := waveAccountCeiling(t, root)
	baseline, baselineHow := waveBaselineAccounts(t, root)
	colls := waveCollectionsInOrder(t, root)

	got, err := scanWaveAccountQuota(colls, baseline)
	if err != nil {
		t.Fatalf("разбор волны: %v", err)
	}

	stems := make([]string, 0, len(colls))
	for _, c := range colls {
		stems = append(stems, c.Stem)
	}
	payers := make([]string, 0, len(got.Payers))
	for p, n := range got.Payers {
		payers = append(payers, fmt.Sprintf("%s=%d", p, n))
	}
	sort.Strings(payers)
	perStem := make([]string, 0, len(got.PerStem))
	for _, c := range colls {
		perStem = append(perStem, fmt.Sprintf("%s=%d", c.Stem, got.PerStem[c.Stem]))
	}

	t.Logf("осмотрено: коллекций %d, шагов %d\n"+
		"порядок волны (выведен %s --stems): %s\n"+
		"потолок %q = %d (%s)\n"+
		"база = %d: %s\n"+
		"списаний %d, возвратов %d, не отнесено %d, не возвращено к концу волны %d\n"+
		"плательщики: %s\n"+
		"пик по коллекциям (с базой): %s\n"+
		"ПИК = %d при потолке %d (запас %d)\n"+
		"ход счётчика:\n%s",
		got.Collections, got.Steps,
		waveDeclRel, strings.Join(stems, " "),
		waveQuotaKind, ceiling, ceilingRel,
		baseline, baselineHow,
		got.Charges, got.Releases, len(got.Unattributed), len(got.LiveAtEnd),
		strings.Join(payers, " "),
		strings.Join(perStem, " "),
		got.Peak, ceiling, ceiling-got.Peak,
		strings.Join(got.Ledger, "\n"))

	// Предпосылки РАСПОЗНАВАТЕЛЕЙ. Проверяются всегда: молчание гейта, у которого
	// сломан распознаватель, неотличимо от молчания на исправном дереве.
	if got.Collections == 0 || got.Steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов %d — перепись беспредметна",
			got.Collections, got.Steps)
	}
	if !got.SawCharge {
		t.Fatalf("в %d шагах волны не распознано НИ ОДНОГО создания аккаунта — "+
			"распознаватель списания не подтверждён на живых данных, поэтому «пик = "+
			"база» тут значит «не смотрел»", got.Steps)
	}
	if !got.SawRefusal {
		t.Fatalf("в %d шагах волны не распознано НИ ОДНОГО утверждённого отказа — "+
			"распознаватель отказа не подтверждён, и всякий отвергаемый POST уехал бы "+
			"в «не отнесено»", got.Steps)
	}

	// ПРЕМИССА МОДЕЛИ: волна идёт под ОДНИМ человеком, поэтому все списания ложатся
	// на ОДИН счётчик. Пока плательщик один, «пик по волне» и «пик по личности» —
	// одно и то же число. Появится второй — пул станет ЗАВЫШЕНИЕМ, и молчать об
	// этом нельзя: гейт обязан сказать, что его предикат больше не описывает мир,
	// а не выдать правдоподобное число, посчитанное не про то.
	if len(got.Payers) > 1 {
		t.Errorf("списания в волне идут под РАЗНЫМИ предъявителями (%s) — премисса "+
			"модели («волна идёт под одним человеком, счётчик один») больше не "+
			"выполняется.\n\n"+
			"Гейт складывает все списания в один счётчик, поэтому его число теперь "+
			"ЗАВЫШЕНО, если эти предъявители принадлежат разным личностям, и ВЕРНО, "+
			"если одной (например, тот же человек с поднятым уровнем входа).\n\n"+
			"Исход: либо свести списания к одному предъявителю, либо переписать "+
			"предикат — считать пик ПО КАЖДОЙ личности и научить гейт различать их "+
			"по дереву. Молча считать дальше нельзя ни при одном из двух.",
			strings.Join(payers, " "))
	}

	if len(got.Unattributed) > 0 {
		sort.Strings(got.Unattributed)
		t.Errorf("шагов создания аккаунта, которые не отнести ни к списанию, ни к "+
			"утверждённому отказу: %d\n  %s\n\n"+
			"Гейт не вправе считать их нулём: аккаунт, созданный и не захваченный, "+
			"занимает место под потолком и не возвращается никогда. Исход: либо шаг "+
			"захватывает id и убирает за собой, либо утверждает свой отказ (синхронный "+
			"статус вне 2xx или `error.code` опрошенной операции).",
			len(got.Unattributed), strings.Join(got.Unattributed, "\n  "))
	}

	if len(got.LiveAtEnd) > 0 {
		t.Errorf("аккаунтов, созданных волной и не удалённых до её конца: %d\n  %s\n\n"+
			"Каждый такой аккаунт — единица под потолком, которую волна больше не "+
			"вернёт: счётчик после него только растёт. Кейс убирает за собой.",
			len(got.LiveAtEnd), strings.Join(got.LiveAtEnd, "\n  "))
	}

	if got.Peak >= ceiling-1 {
		t.Errorf("пик одновременно живых аккаунтов человека церемонии = %d при потолке "+
			"%d — запас %d, а обязан быть не меньше 2 (пик строго меньше %d).\n"+
			"Достигнут на: %s\n"+
			"Потолок объявлен: %s\n"+
			"База: %s\n\n"+
			"Следствие: следующее одновременно живое создание аккаунта ГДЕ УГОДНО в "+
			"волне упрётся в потолок, и отказ придёт НЕ тому кейсу, который его "+
			"вызвал, — а тому, кто просто оказался следующим.\n\n"+
			"Исход: развести создания так, чтобы одновременно живым был один аккаунт "+
			"волны. Поднимать потолок нельзя — это продуктовая величина, и подгонять "+
			"её под пробы значит подгонять продукт под тест.\n"+
			"Ход счётчика напечатан выше.",
			got.Peak, ceiling, ceiling-got.Peak, ceiling-1,
			got.PeakAt, ceilingRel, baselineHow)
	}
}
