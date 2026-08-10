// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// uisourcereadtest_test.go — проба интерфейса не подтверждает себя чтением
// модуля консоли как текста.
//
// # Предмет — КЛАСС, а не механизм
//
// Прежняя редакция гейта ловила механизм: «в файле встречается readFileSync».
// Механизм — не дефект. По этому предикату в перечень-долг попали конформансные
// пробы, для которых чтение исходников И ЕСТЬ проверка: они обходят дерево и
// утверждают его СОСТАВ (какой модуль что объявляет, нет ли второй копии
// поверхности, согласуется ли реестр консоли со связываниями `proto/`). Такой
// факт исполнением одного модуля не получить — читать здесь нечем заменить.
//
// Дефект — другое: проба берёт координату, которую САМА ЖЕ и написала, читает
// по ней **модуль консоли** и утверждает о его символах. Эталонная форма (её в
// дереве было 139 копий):
//
//	const source = readFileSync(join(here, "DetailShell.tsx"), "utf8");
//	expect(source).toContain("DetailShell");
//
// Компонент не отрисован, функция не вызвана, ни один исход не утверждён: проба
// зелена, пока файл существует, и не краснеет ни на одном изменении поведения.
// Слот занят, отчёт зелёный, поведение не проверено ничем — это хуже
// отсутствующей пробы: отсутствие видно, ложная уверенность нет.
//
// # Предикат
//
// Находка = проба, которая (1) читает содержимое файла, (2) НЕ обходит дерево и
// (3) называет в исполняемой части путь модуля консоли (`.ts`/`.tsx`).
// Отдельно и независимо от обхода — проба, читающая СВОЙ модуль (`X.test.tsx` →
// `X.tsx`): это буквально «подтвердить себя своим же текстом», и обход дерева
// такую форму не оправдывает.
//
// Каждое из трёх условий несёт свой смысл:
//
//   - (1) без чтения предмета нет вовсе;
//   - (2) обход = перепись состава дерева, законный предмет (см. выше);
//   - (3) читается **загружаемый** модуль. Контракт ствола (`.proto`), исходник
//     другого языка (`.go`), объявление сборки (`.cjs`) проба исполнить не
//     может — там чтение единственный доступный способ, и запрет был бы просто
//     запретом на проверку. А `.ts`/`.tsx` она обязана ЗАГРУЗИТЬ: `vite.config.ts`
//     отдаёт `server.proxy` объектом, реестр — записями, компонент — разметкой.
//
// Разбор идёт по исполняемой части: комментарии и содержимое строковых/
// регулярных литералов из поиска вызовов исключены. Иначе гейт нашёл бы
// `readFileSync` в абзаце, который сам же и объясняет запрет, — и остался бы
// зелёным при снятой защите (`testing.md` §«Гейт на класс», п. 4).
//
// # Объём осмотренного
//
// Гейт печатает перепись на каждом прогоне: сколько проб осмотрено, сколько из
// них читает с диска, сколько обходит дерево, сколько признано находками.
// «Ноль находок» обязано быть отличимо от «ноль прочитанного», а непустой
// счётчик переписей доказывает, что дискриминатору есть что различать.
//
// # Способность упасть
//
// Доказана инъекцией в три стороны — `uisourcereadtest_injection_test.go`:
// настоящий дефект краснеет с координатой; законная перепись (обход дерева) и
// законная сверка со стволом (`.proto` по фиксированному пути) молчат, И ПРИ
// ЭТОМ объём осмотренного растёт — иначе молчание означало бы «не прочитал».
//
// Отслеживается PRO-Robotech/kacho#5.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Разбор исполняемой части TypeScript.
// ─────────────────────────────────────────────────────────────────────────────

// tsScan разделяет исходник TypeScript на две части, которые задают РАЗНЫЕ
// вопросы:
//
//	code     — только исполняемая часть: комментарии удалены, тела строковых,
//	           шаблонных и регулярных литералов вычищены. По ней спрашивают
//	           «зовётся ли здесь такая функция».
//	literals — тела строковых литералов, встреченных В КОДЕ (не в комментарии).
//	           По ним спрашивают «называет ли проба такую координату».
//
// Регулярные литералы распознаются, потому что в нашем корпусе они сплошь несут
// кавычки (`/"(\/[^"]+)":/g`): принятые за деление, они переключили бы разбор в
// строку и съели бы половину файла — гейт замолчал бы, не сказав об этом.
func tsScan(src string) (code string, literals []string) {
	var b strings.Builder
	b.Grow(len(src))
	rs := []rune(src)

	// prevSignificant — последний значимый символ кода: по нему решается, что
	// такое `/` — начало регулярного литерала или деление.
	prevSignificant := rune(0)
	regexAllowedAfter := func(r rune) bool {
		switch r {
		case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '\n', '+', '*', '~', '%', '<', '>', '^':
			return true
		}
		return false
	}

	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case r == '/' && i+1 < len(rs) && rs[i+1] == '/':
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
		case r == '/' && i+1 < len(rs) && rs[i+1] == '*':
			i += 2
			for i+1 < len(rs) && !(rs[i] == '*' && rs[i+1] == '/') {
				i++
			}
			i += 2
		case r == '\'' || r == '"':
			quote := r
			i++
			var lit strings.Builder
			for i < len(rs) && rs[i] != quote {
				if rs[i] == '\\' && i+1 < len(rs) {
					lit.WriteRune(rs[i+1])
					i += 2
					continue
				}
				if rs[i] == '\n' { // незакрытая строка — не глотаем остаток файла
					break
				}
				lit.WriteRune(rs[i])
				i++
			}
			i++
			literals = append(literals, lit.String())
			b.WriteString(`""`)
			prevSignificant = '"'
		case r == '`':
			i++
			var lit strings.Builder
			for i < len(rs) && rs[i] != '`' {
				if rs[i] == '\\' && i+1 < len(rs) {
					lit.WriteRune(rs[i+1])
					i += 2
					continue
				}
				// ${…} — снова код: его содержимое обязано попасть в code,
				// иначе вызов внутри подстановки стал бы невидим.
				if rs[i] == '$' && i+1 < len(rs) && rs[i+1] == '{' {
					depth := 0
					j := i + 1
					for j < len(rs) {
						if rs[j] == '{' {
							depth++
						} else if rs[j] == '}' {
							depth--
							if depth == 0 {
								break
							}
						}
						j++
					}
					inner, innerLits := tsScan(string(rs[i+2 : min(j, len(rs))]))
					b.WriteString(inner)
					literals = append(literals, innerLits...)
					i = j + 1
					continue
				}
				lit.WriteRune(rs[i])
				i++
			}
			i++
			literals = append(literals, lit.String())
			b.WriteString(`""`)
			prevSignificant = '`'
		case r == '/' && regexAllowedAfter(prevSignificant):
			i++
			inClass := false
			for i < len(rs) && rs[i] != '\n' {
				if rs[i] == '\\' && i+1 < len(rs) {
					i += 2
					continue
				}
				if rs[i] == '[' {
					inClass = true
				} else if rs[i] == ']' {
					inClass = false
				} else if rs[i] == '/' && !inClass {
					break
				}
				i++
			}
			i++
			b.WriteString(" ")
			prevSignificant = '/'
		default:
			b.WriteRune(r)
			if !isTSSpace(r) {
				prevSignificant = r
			}
			i++
		}
	}
	return b.String(), literals
}

func isTSSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\r' }

// ─────────────────────────────────────────────────────────────────────────────
// Предикат класса.
// ─────────────────────────────────────────────────────────────────────────────

// contentReadCalls — вызовы, которыми читают СОДЕРЖИМОЕ файла. `existsSync`/
// `statSync` сюда не входят намеренно: они спрашивают о составе дерева, а не о
// символах модуля, — то есть относятся к переписи.
var contentReadCalls = []string{
	"readFileSync(", "readFile(", "createReadStream(", "openSync(", "readFileAsync(",
}

// treeWalkCalls — вызовы, которыми ОБХОДЯТ дерево. Их наличие означает, что
// предмет пробы — состав дерева, а не текст названного ею модуля.
var treeWalkCalls = []string{
	"readdirSync(", "readdir(", "opendirSync(", "opendir(", "globSync(", "globby(", "fastGlob(", "fg(",
}

// consoleModuleLiteral — координата ЗАГРУЖАЕМОГО модуля консоли. Именно её
// проба обязана импортировать, а не читать.
var consoleModuleLiteral = regexp.MustCompile(`(^|/)[A-Za-z0-9_.\-]+\.tsx?$`)

type uiSourceFinding struct {
	File   string
	Why    string
	Coords []string
}

// auditUISourceReads — предикат класса. Вход — соответствие «путь пробы → её
// исходник», чтобы инъекция гоняла ТУ ЖЕ функцию, что и обход дерева, а не свою
// копию логики.
func auditUISourceReads(sources map[string]string) (findings []uiSourceFinding, reads, walks int) {
	for rel, src := range sources {
		code, literals := tsScan(src)

		readsFile := containsAny(code, contentReadCalls)
		walksTree := containsAny(code, treeWalkCalls)
		if readsFile {
			reads++
		}
		if walksTree {
			walks++
		}
		if !readsFile {
			continue
		}

		// Свой собственный модуль: `X.test.tsx` → `X.ts` / `X.tsx`. Обход дерева
		// такую форму НЕ оправдывает — она и есть «подтвердить себя своим текстом».
		own := ownModuleNames(rel)
		var ownHits []string
		for _, lit := range literals {
			// Своим считается ТОЛЬКО сосед по каталогу, названный без пути:
			// `readFileSync(join(here, "DetailShell.tsx"))`. Координата с
			// каталогом (`shared/src/test/antd-icons-stub.tsx`) указывает на
			// ЧУЖОЙ файл, даже когда имя совпадает с собственным, — сравнение
			// по одному лишь имени дало бы ложную находку на переписи, которая
			// обходит одноимённые файлы всех приложений.
			if strings.ContainsRune(lit, '/') {
				continue
			}
			if own[lit] {
				ownHits = append(ownHits, lit)
			}
		}
		if len(ownHits) > 0 {
			findings = append(findings, uiSourceFinding{
				File: rel, Why: "читает СВОЙ модуль как текст", Coords: dedupSorted(ownHits),
			})
			continue
		}

		if walksTree {
			continue // перепись состава дерева — законный предмет
		}

		var moduleHits []string
		for _, lit := range literals {
			if consoleModuleLiteral.MatchString(lit) {
				moduleHits = append(moduleHits, lit)
			}
		}
		if len(moduleHits) > 0 {
			findings = append(findings, uiSourceFinding{
				File: rel, Why: "читает модуль консоли по своей же координате", Coords: dedupSorted(moduleHits),
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].File < findings[j].File })
	return findings, reads, walks
}

// ownModuleNames — имена файлов, которые для пробы `<name>.test.ts[x]` являются
// её собственным модулем.
func ownModuleNames(rel string) map[string]bool {
	base := filepath.Base(rel)
	stem := strings.TrimSuffix(strings.TrimSuffix(base, ".tsx"), ".ts")
	stem = strings.TrimSuffix(stem, ".test")
	return map[string]bool{stem + ".ts": true, stem + ".tsx": true}
}

func containsAny(hay string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

func dedupSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт по дереву.
// ─────────────────────────────────────────────────────────────────────────────

func uiProbeSources(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasPrefix(rel, "ui-future/") {
			continue
		}
		if !strings.HasSuffix(rel, ".test.ts") && !strings.HasSuffix(rel, ".test.tsx") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v — состав проб неизвестен, значит вердикт был бы утверждением ни о чём", rel, err)
		}
		out[rel] = string(b)
	}
	return out
}

// TestUITestsDoNotReadTheirOwnSourceAsText — проба интерфейса не вправе
// подтверждать себя чтением модуля консоли как текста.
func TestUITestsDoNotReadTheirOwnSourceAsText(t *testing.T) {
	root := repoRoot(t)
	sources := uiProbeSources(t, root)

	if len(sources) == 0 {
		t.Fatal("обход не нашёл ни одной пробы интерфейса — гейт беспредметен. " +
			"Либо каталог переехал, либо расширение проб изменилось; в обоих случаях " +
			"зелёный вердикт ниже был бы получен даром.")
	}

	findings, reads, walks := auditUISourceReads(sources)

	// Предпосылки дискриминатора. Он различает «читает свою координату» и
	// «обходит дерево»; если в дереве нет ни того, ни другого вида, различать
	// нечего, и молчание гейта означало бы поломку разбора, а не чистоту.
	if reads == 0 {
		t.Error("ни одна проба интерфейса не читает с диска — распознавание чтения сломано. " +
			"«Ноль находок» здесь неотличимо от «ноль прочитанного».")
	}
	if walks == 0 {
		t.Error("ни одна проба интерфейса не обходит дерево — распознавание переписи сломано. " +
			"Дискриминатору нечего отличать от находки, значит молчание ниже ничего не стоит.")
	}

	for _, f := range findings {
		t.Errorf("проба интерфейса %s:\n  %s\n  координаты: %s\n\n"+
			"Такая проба зелена, пока файл существует: компонент не рендерится, функция не "+
			"зовётся, ни один исход не утверждается — слот занят, поведение не проверено.\n"+
			"Исходы: загрузи модуль и утверждай его значение / рендери компонент и утверждай "+
			"наблюдаемое / удали пробу, если предмета у неё нет.\n"+
			"Читать с диска законно то, что проба исполнить НЕ может (`.proto`, `.go`, "+
			"объявление сборки), и то, что найдено ОБХОДОМ дерева, — перепись состава.",
			f.Why, f.File, strings.Join(f.Coords, ", "))
	}

	t.Logf("перепись: проб интерфейса осмотрено %d, читают с диска %d, обходят дерево %d, "+
		"находок %d", len(sources), reads, walks, len(findings))
}
