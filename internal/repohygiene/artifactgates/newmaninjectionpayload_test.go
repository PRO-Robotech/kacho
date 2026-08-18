// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmaninjectionpayload_test.go — гейт по дереву: НАГРУЗКА ВНЕДРЕНИЯ ОБЯЗАНА
// НЕСТИ ТО, ЧЕМ НАЗВАНА.
//
// # Предмет
//
// Кейс `*-CR-SEC-<ВИД>` шлёт в `name` полезную нагрузку, и ВИД в его имени — это
// утверждение о том, какой класс ввода получает край. Утверждение можно сделать
// ложным, не тронув ни одного `pm.test`: достаточно поменять байты нагрузки.
// Отчёт останется зелёным, слот останется занятым, а входа, ради которого проба
// заведена, край не увидит ни разу.
//
// Наблюдалось (#701): нагрузка `nullbyte` шести ресурсов vpc несла `x`, ПРОБЕЛ,
// `y`. Исход совпадал с объявленным — пробел вне класса символов имени ровно так
// же, как нулевой байт, — поэтому кейс был зелёным, и подмена прожила от
// заведения пробы до отдельного разбора. Нулевой байт интересен именно тем, чего
// пробел не даёт: его обработка расходится между слоями (разбор JSON, драйвер БД,
// C-строки libpq), и покрытие этого различия обеспечивала ровно одна проба.
//
// # Что здесь проверяется
//
// По каждому виду — ПРЕДИКАТ, выражающий то, что обещает имя вида (таблица
// `nmInjectionClaims`). Предикаты намеренно слабые: гейт судит о ПРИСУТСТВИИ
// характерного признака класса, а не о «правильности» конкретной строки. Автор
// нагрузки волен выбрать любую — но нагрузка по имени `path` обязана нести выход
// из каталога, а по имени `nullbyte` — байт `0x00`.
//
// # Предмет — СГЕНЕРИРОВАННЫЕ коллекции, а не питоновские исходники
//
// Спрашивается не «что написано в генераторе», а «что уедет на край»: тело
// декодируется тем же порядком, каким его читает край, — сперва коллекция, затем
// строка `raw` как JSON. Нулевой байт в JSON выразим только экранирующей
// последовательностью, поэтому проверять исходник генератора значило бы судить о
// намерении, а не о байтах. Тот же выбор и по той же причине сделан в
// `newmancapturedvar_test.go` и `newmanphantomid_test.go`.
//
// # Два списка, и оба ИСТЕКАЮТ САМИ
//
// `nmInjectionClaims` — виды, о которых гейт умеет судить. Вид, у которого в
// дереве не осталось ни одной нагрузки, — находка: запись без предмета переживёт
// то, что ею обозначалось.
//
// `nmSecFoldersWithoutPayload` — кейсы с меткой `-CR-SEC-`, у которых нагрузки с
// именем НЕТ by construction (предмет кейса — полоса доступа, а не класс байтов).
// Запись, которой больше нечего исключать, — тоже находка. Без второго списка
// пришлось бы либо молчать о папках, где гейт не узнал ничего (слепая зона на
// следующий новый вид), либо краснеть на законных кейсах (первый же ложный
// срабат гейт и отключит).
//
// # Способность упасть доказана инъекцией в ОБЕ стороны
//
// `newmaninjectionpayload_injection_test.go`: подменённая нагрузка краснеет и
// называет координату с видом; рядом — законные близнецы той же формы (та же
// папка с настоящим нулевым байтом, соседний вид, папка из списка исключений), на
// которых гейт молчит. У каждого молчания есть парный контроль по переписи
// (`payloads == 1`), иначе «находок ноль» было бы неотличимо от «предикат ослеп».
package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/validate/nameform"
)

// nmInjectionClaim — обещание, которое даёт ИМЯ вида нагрузки, и предикат этого
// обещания.
type nmInjectionClaim struct {
	kind    string
	promise string
	holds   func(string) bool
}

// nmInjectionClaims — закрытая таблица видов. Закрытая намеренно: «что-нибудь
// похожее на нагрузку» засчитывало бы за проверку саму строку.
var nmInjectionClaims = []nmInjectionClaim{
	{
		kind:    "sqli",
		promise: "разрыв строкового литерала SQL: апостроф плюс продолжение выражения (`--` или ` OR `)",
		holds: func(v string) bool {
			return strings.Contains(v, "'") &&
				(strings.Contains(v, "--") || strings.Contains(strings.ToLower(v), " or "))
		},
	},
	{
		kind:    "union",
		promise: "склейка выборок: апостроф плюс `UNION` и `SELECT`",
		holds: func(v string) bool {
			low := strings.ToLower(v)
			return strings.Contains(v, "'") &&
				strings.Contains(low, "union") && strings.Contains(low, "select")
		},
	},
	{
		kind:    "xss",
		promise: "открывающий тег сценария `<script`",
		holds:   func(v string) bool { return strings.Contains(strings.ToLower(v), "<script") },
	},
	{
		kind:    "cmd",
		promise: "метасимвол оболочки: `;` `|` `&` `$` или обратная кавычка",
		holds:   func(v string) bool { return strings.ContainsAny(v, ";|&$`") },
	},
	{
		kind:    "path",
		promise: "выход из каталога — `../`",
		holds:   func(v string) bool { return strings.Contains(v, "../") },
	},
	{
		kind: "nullbyte",
		// Именно БАЙТ, а не его экранирующая запись: гейт судит о том, что
		// получит край после разбора JSON, а не о том, как это записано.
		promise: "настоящий нулевой байт 0x00",
		holds:   func(v string) bool { return strings.ContainsRune(v, 0) },
	},
	{
		kind: "longpayload",
		// Предел берётся из ЕДИНСТВЕННОЙ формы имени дерева, а не выписывается
		// числом: сменят форму — гейт поедет вместе с ней, а не разойдётся молча.
		promise: fmt.Sprintf("длина больше предела имени (nameform.MaxLen = %d)", nameform.MaxLen),
		holds:   func(v string) bool { return len(v) > nameform.MaxLen },
	},
}

// nmSecFoldersWithoutPayload — кейс `-CR-SEC-`, у которого нагрузки с именем нет
// by construction. Ключ — идентификатор кейса, значение — причина.
var nmSecFoldersWithoutPayload = map[string]string{
	"LST-CR-SEC-TG-CROSS-PROJECT": "предмет кейса — целевая группа ЧУЖОГО проекта, то есть полоса доступа, " +
		"а не класс символов в имени; нагрузки, о которой можно судить по имени вида, у него нет",
}

var (
	// nmSecFolderRe — папка кейса внедрения. Имя папки строится генераторами как
	// `<id> — <заголовок>`, поэтому вид берётся из идентификатора.
	nmSecFolderRe = regexp.MustCompile(`-CR-SEC-([A-Z0-9-]+)`)
	// nmPayloadStepRe — шаг, несущий нагрузку: `cr-<вид>` либо `sec-<вид>` с
	// необязательным уникализирующим хвостом (`cr-nullbyte-rya142`). Больше одного
	// хвоста НЕ допускается намеренно: `cr-lst-cross-tg` — фикстурный шаг чужого
	// кейса, и попытка прочесть его как нагрузку дала бы ложную находку.
	nmPayloadStepRe = regexp.MustCompile(`^(?:cr|sec)-([a-z][a-z0-9]*)(?:-[a-z0-9]+)?$`)
)

// nmPayloadFinding — одна находка: нагрузка, чьи байты не отвечают имени вида,
// либо папка внедрения, в которой гейт не узнал нагрузки и не имеет на это записи.
type nmPayloadFinding struct {
	collection string
	caseID     string
	step       string
	kind       string
	what       string
}

func (f nmPayloadFinding) String() string {
	if f.step == "" {
		return fmt.Sprintf("%s :: %s — %s", f.collection, f.caseID, f.what)
	}
	return fmt.Sprintf("%s :: %s :: %s [вид %s] — %s", f.collection, f.caseID, f.step, f.kind, f.what)
}

// nmPayloadCensus — объём осмотренного. Печатается вместе с вердиктом, чтобы
// «ноль находок» было отличимо от «ноль прочитанного».
type nmPayloadCensus struct {
	collections int
	secFolders  int
	payloads    int
	byKind      map[string]int
	// waiversUsed — записи списка исключений, у которых нашёлся предмет. Считается
	// ЗДЕСЬ, а не в теле гейта: проба, доказывающая самоистечение, обязана гонять
	// ту же функцию, а не свою копию.
	waiversUsed map[string]bool
}

// nmStepLeafName — имя шага без префикса кейса. Один генератор пишет шаг как
// `cr-sqli`, другой — как `IAM-ACC-CR-SEC-INJECTION :: sec-sqli`; читать надо обе
// формы, иначе половина дерева станет гейту невидима (замер: без этого гейт
// пропускал 2 набора из 3 неvpc-шных).
func nmStepLeafName(name string) string {
	if i := strings.LastIndex(name, " :: "); i >= 0 {
		return name[i+len(" :: "):]
	}
	return name
}

// nmCaseIDOf — идентификатор кейса из имени папки `<id> — <заголовок>`.
func nmCaseIDOf(folder string) string {
	if i := strings.IndexAny(folder, " \t"); i >= 0 {
		return folder[:i]
	}
	return folder
}

// nmBodyName — значение поля `name` в теле шага, декодированное ТЕМ ЖЕ порядком,
// каким его читает край: строка `raw` разбирается как JSON. Второй результат —
// было ли поле вообще.
func nmBodyName(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return "", false
	}
	v, ok := body["name"]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

func TestInjectionPayloadCarriesWhatItsNamePromises(t *testing.T) {
	root := repoRoot(t)

	// Состав берётся из ИНДЕКСА git: под корнем лежат рабочие копии агентов и
	// распаковки отчётов прогонов, и вердикт по ним был бы свойством чужого
	// рабочего каталога, а не коммита.
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	findings, cen, err := auditInjectionPayloadNames(root, cols)
	if err != nil {
		t.Fatal(err)
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Гейт, чей распознаватель перестал что-либо
	// узнавать (переезд коллекций, смена имён шагов, смена формы кейса), молча
	// стал бы вечнозелёным.
	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.secFolders == 0 {
		t.Fatalf("прочитано коллекций %d, кейсов внедрения 0 — предикат папки ослеп", cen.collections)
	}
	if cen.payloads == 0 {
		t.Fatalf("кейсов внедрения %d, нагрузок распознано 0 — предикат шага ослеп; "+
			"чинить надо гейт, а не выходить успехом", cen.secFolders)
	}

	kinds := make([]string, 0, len(cen.byKind))
	for k := range cen.byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	var perKind []string
	for _, k := range kinds {
		perKind = append(perKind, fmt.Sprintf("%s %d", k, cen.byKind[k]))
	}
	t.Logf("осмотрено: коллекций %d, кейсов внедрения %d, нагрузок %d (по видам: %s), "+
		"записей исключений %d из них с предметом %d",
		cen.collections, cen.secFolders, cen.payloads, strings.Join(perKind, ", "),
		len(nmSecFoldersWithoutPayload), len(cen.waiversUsed))

	// САМОИСТЕЧЕНИЕ ТАБЛИЦЫ ВИДОВ: запись, у которой в дереве не осталось предмета,
	// переживёт то, чем обозначалась, и следующий читатель примет её за покрытие.
	for _, c := range nmInjectionClaims {
		if cen.byKind[c.kind] == 0 {
			t.Errorf("вид нагрузки %q объявлен в таблице гейта, но в дереве его больше нет — "+
				"запись без предмета: либо нагрузка снята и запись идёт следом, либо "+
				"распознаватель перестал её видеть", c.kind)
		}
	}
	// САМОИСТЕЧЕНИЕ СПИСКА ИСКЛЮЧЕНИЙ — по той же причине.
	for id := range nmSecFoldersWithoutPayload {
		if !cen.waiversUsed[id] {
			t.Errorf("кейс %q стоит в списке «нагрузки нет by construction», но такого кейса "+
				"в дереве нет — исключению нечего исключать, запись снимается", id)
		}
	}

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "нагрузок, не отвечающих своему имени: %d\n\n", len(findings))
		b.WriteString("ВИД в идентификаторе кейса — утверждение о том, какой класс ввода получает\n")
		b.WriteString("край. Оно ломается без правки единого `pm.test`: достаточно поменять байты,\n")
		b.WriteString("и слот останется занятым, отчёт зелёным, а вход — неотправленным.\n\n")
		b.WriteString("Чинится в генераторе или cases/*.py набора, после чего коллекции\n")
		b.WriteString("перегенерируются scripts/gen.py. Переименовать нагрузку по тому, чем она\n")
		b.WriteString("на самом деле является, — тоже законный исход, но исход, а не умолчание.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}

// auditInjectionPayloadNames — весь разбор одним входом.
//
// Вынесено из тела гейта намеренно: проба, доказывающая способность гейта упасть,
// обязана гонять ТУ ЖЕ функцию, а не свою копию логики.
func auditInjectionPayloadNames(root string, cols []string) ([]nmPayloadFinding, nmPayloadCensus, error) {
	claim := map[string]nmInjectionClaim{}
	for _, c := range nmInjectionClaims {
		claim[c.kind] = c
	}

	var findings []nmPayloadFinding
	cen := nmPayloadCensus{byKind: map[string]int{}, waiversUsed: map[string]bool{}}

	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++

		var walk func(items []nmItem)
		walk = func(items []nmItem) {
			for _, it := range items {
				if !it.isFolder() {
					continue
				}
				if nmSecFolderRe.MatchString(it.Name) {
					cen.secFolders++
					auditOneSecCase(rel, it, claim, &cen, &findings)
				}
				walk(it.Item)
			}
		}
		walk(col.Item)
	}
	return findings, cen, nil
}

// auditOneSecCase — разбор одного кейса внедрения: судятся нагрузки известных
// видов, а папка без единой узнанной нагрузки требует записи в списке исключений.
func auditOneSecCase(
	rel string, folder nmItem, claim map[string]nmInjectionClaim,
	cen *nmPayloadCensus, findings *[]nmPayloadFinding,
) {
	caseID := nmCaseIDOf(folder.Name)
	judged := 0
	for _, st := range folder.Item {
		if st.isFolder() {
			continue
		}
		m := nmPayloadStepRe.FindStringSubmatch(nmStepLeafName(st.Name))
		if m == nil {
			continue
		}
		c, known := claim[m[1]]
		if !known {
			// Шаг фикстуры внутри кейса внедрения (`cr-net`, `sec-setup`).
			// Судить его как нагрузку значило бы ловить форму имени, а не
			// существо; предметом гейта он не является.
			continue
		}
		name, ok := nmBodyName(st.bodyRaw())
		if !ok {
			continue
		}
		cen.payloads++
		cen.byKind[c.kind]++
		judged++
		if c.holds(name) {
			continue
		}
		*findings = append(*findings, nmPayloadFinding{
			collection: rel, caseID: caseID, step: nmStepLeafName(st.Name), kind: c.kind,
			what: fmt.Sprintf("имя обещает %s, а на край уходит %s", c.promise, nmDescribeBytes(name)),
		})
	}
	if judged > 0 {
		return
	}
	if _, waived := nmSecFoldersWithoutPayload[caseID]; waived {
		cen.waiversUsed[caseID] = true
		return
	}
	*findings = append(*findings, nmPayloadFinding{
		collection: rel, caseID: caseID,
		what: "кейс помечен как внедрение, но нагрузки, о которой можно судить по имени вида, " +
			"в нём не найдено — либо шаг назван не по виду, либо вид гейту неизвестен и ему " +
			"нужен предикат; если нагрузки нет by construction, запись идёт в " +
			"nmSecFoldersWithoutPayload с причиной",
	})
}

// nmDescribeBytes — то, что уходит на край, названо ПОБАЙТНО.
//
// Печатать такую строку как есть нельзя: разница между пробелом и нулевым байтом
// в выводе теста невидима, а именно она и есть предмет находки (#701).
func nmDescribeBytes(v string) string {
	const head = 24
	b := []byte(v)
	if len(b) > head {
		return fmt.Sprintf("%x… (длина %d)", b[:head], len(b))
	}
	return fmt.Sprintf("%x (длина %d)", b, len(b))
}
