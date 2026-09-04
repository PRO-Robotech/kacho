// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// clientdocsmechanismdenial_test.go — гейт «клиентская документация не отрицает
// механизм, который в дереве ЕСТЬ».
//
// # Предмет
//
// Утверждение «такого механизма нет» переживает свой предмет молча. Наблюдалось
// четырьмя страницами сразу (#1389): край дважды объявлял «Watch-RPC нет», storage —
// «Метода подписки на изменения не существует», compute — «Подписки на журнал
// снаружи нет», будучи в том же дереве назначенным первым владельцем журнала.
// Клиент, читающий документацию, останавливался и отвечал «платформа этого не
// умеет». Имитация «разработчик» назвала цену прямо: около трёх часов на то, чтобы
// НЕ ПОВЕРИТЬ четырём документам и полезть в контракты. Дороже: тот, кто поверил,
// строит на опросе архитектуру, которую переделывают месяцами.
//
// # Почему этот гейт заведён вторым к TestSubscriptionOwnersSaySoInTheirClientDocs
//
// Тот требует ПОЛОЖИТЕЛЬНОГО и связывает только ВЛАДЕЛЬЦЕВ журнала (сегодня два —
// compute и nlb). Из четырёх страниц наблюдавшегося случая он покрывает две: край и
// storage владельцами не являются и под него не подпадают ни при каком тексте.
// Половина предмета оставалась незакрытой, и закрыть её положительным требованием
// нельзя — требовать от каждого домена УПОМИНАТЬ платформенную возможность значило
// бы краснеть на законно молчащей странице.
//
// # Возражение, которое здесь снято ЗАМЕРОМ, а не доводом
//
// Соседний гейт отказался от обратного предиката с доводом: лексикон над
// естественным языком не отличит законного близнеца «подписки на ОПЕРАЦИИ нет» —
// утверждение верное и обязанное остаться. Довод был бы решающим, если бы
// различение шло по ФОРМЕ ОТРИЦАНИЯ. Здесь оно идёт по ПРЕДМЕТУ отрицания, и
// предмет — величина закрытая и измеримая:
//
//   - отрицание, СУЖЕННОЕ до операций либо до конкретного домена, — истинно и молчит;
//   - отрицание платформенного механизма БЕЗ сужения — ложно, потому что механизм в
//     дереве есть, и это проверяется отдельным утверждением (предпосылка ниже).
//
// Довод проверен на населении, а не принят на слово: в клиентской документации
// сегодня 27 предложений, несущих отрицание рядом с именем механизма, и ВСЕ 27
// истинны. Они и служат законными близнецами — гейт обязан молчать на каждом.
// Инъекция подаёт ЧЕТЫРЕ ИСТОРИЧЕСКИХ отрицания дословно (`95ea80b68^`), а не
// синтетику: настоящий вход из дерева.
//
// # Чем истекает
//
// ОТ ФАКТА В ДЕРЕВЕ, в обе стороны. Пропадёт стрим-глагол из контракта — гейт
// падает предпосылкой, а не молчит: тогда отрицания стали БЫ правдой, и их надо
// перечитать, а не оставить под мёртвым запретом. Появится второй механизм — его
// имя добавляется в словарь, и перепись это покажет числом.

// denialStreamMarker — по чему гейт узнаёт, что механизм в дереве есть.
//
// Судится ОБЪЯВЛЕНИЕ серверного стрима в контракте: подписка на изменения ресурсов
// объявлена однажды, и это её единственный признак, не зависящий ни от имени
// сервиса, ни от прозы.
const denialStreamMarker = "returns (stream"

// denialForms — формы отрицания. Перечень открыт и печатается переписью: форма, о
// которой распознаватель не знает, даёт не красное и не зелёное, а МОЛЧАНИЕ, —
// поэтому она обязана быть видна числом, а не подразумеваться.
var denialForms = []string{
	"не существует", "не планируется", "нет и не", "отсутству", "не предусмотр",
	"не поддержив", "не бывает", "не заводит", "нет публичн", " нет", " нет.", " нет,",
	" нет:", " нет—", " нет —", "нету",
}

// denialMechanismTokens — имена механизма, о котором идёт спор.
var denialMechanismTokens = []string{
	"watch", "подписк", "подписыва", "стрим", "streaming", "поток изменен", "server-sent",
}

// denialNarrowing — сужения, делающие отрицание ИСТИННЫМ.
//
// Каждое взято из живого текста дерева, а не придумано:
//
//   - «операци» — подписки на операции действительно нет, и это решение
//     (`pkg/subscription/doc.go`: «НЕ подписывает на операции»);
//
//   - «собственн», «у сервиса», «у нас» — отрицание СВОЕГО стрима домена, снятого в
//     пользу платформенного;
//
//   - имя конкретного домена — отрицание журнала У ЭТОГО домена: владельцами
//     объявлены не все, и «журнала подписки у vpc пока нет» верно.
//
//   - «метрик» — отрицание про НАБЛЮДАЕМОСТЬ, а не про механизм: страница compute
//     перечисляет непубликуемые метрики, и «число активных потоков подписки» стоит
//     там ИМЕНЕМ МЕТРИКИ. Найдено первым же прогоном по дереву, а не предположено.
//
// Список — словарь ПРЕДМЕТОВ отрицания, а не ведомость прощённых мест: он говорит,
// о чём речь, и потому растёт вместе с языком документации, а не вместе с числом
// нарушений. Каждый предмет виден переписью отдельным числом, поэтому «замолчали
// сужением» отличимо от «нечего было находить».
//
// Слова «платформ» здесь НЕТ намеренно: «у платформы подписки на изменения нет» —
// ровно то ложное утверждение, ради которого гейт заведён.
var denialNarrowing = []string{"операци", "собственн", "у сервиса", "у нас", "метрик"}

// denialPage — страница клиентской документации, как её видит суждение.
//
// Отдельным типом ради ОДНОГО: доказательство способности гейта упасть обязано
// прогонять ТУ ЖЕ функцию суждения, а не её копию. В бою страницы приходят из
// состава дерева, в инъекции — синтетическим набором, не заводя репозитория.
type denialPage struct {
	rel  string
	body string
}

// denialCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от «ноль
// прочитанного», поэтому перепись печатается ВСЕГДА и по каждой оси отдельно.
type denialCensus struct {
	pages      int
	sentences  int
	candidates int
	narrowed   map[string]int
	findings   int
}

// clientDocsDenialFindings — суждение. Вынесено отдельно, чтобы инъекция прогоняла
// его, а не свою копию.
func clientDocsDenialFindings(pages []denialPage, domains []string) ([]string, denialCensus) {
	census := denialCensus{narrowed: map[string]int{}}
	narrowing := append([]string{}, denialNarrowing...)
	for _, d := range domains {
		// Сужение до КОНКРЕТНОГО домена: «журнала подписки у vpc пока нет».
		narrowing = append(narrowing, strings.ToLower(d))
	}
	out := make([]string, 0, 4)
	for _, page := range pages {
		census.pages++
		for _, s := range denialSentences(page.body) {
			census.sentences++
			low := strings.ToLower(s.text)
			if !denialContainsAny(low, denialMechanismTokens) || !denialContainsAny(low, denialForms) {
				continue
			}
			census.candidates++
			if q := denialFirstMatch(low, narrowing); q != "" {
				census.narrowed[q]++
				continue
			}
			census.findings++
			out = append(out, fmt.Sprintf(
				"%s:%d — клиентская страница ОТРИЦАЕТ механизм, который в дереве есть: %q.\n"+
					"    Подписка на изменения ресурсов объявлена контрактом (серверный стрим, "+
					"одно объявление на всю платформу), поэтому это утверждение ложно, а читает "+
					"его клиент.\n"+
					"    Исходов три: (а) переписать по факту, назвав НОВОЕ состояние, а не молча "+
					"удалив прежнее — опрос остаётся штатным путём; (б) сузить отрицание до его "+
					"истинного предмета («подписки на ОПЕРАЦИИ нет», «журнала подписки у <домена> "+
					"нет») — тогда гейт замолчит сам; (в) снять предложение вместе с предметом.\n"+
					"    Ровно этот исход наблюдался четырьмя страницами сразу (#1389).",
				page.rel, s.line, strings.TrimSpace(s.text)))
		}
	}
	return out, census
}

// denialSentence — предложение и строка, на которой оно начинается.
type denialSentence struct {
	text string
	line int
}

// denialSentences режет страницу на предложения, НЕ теряя привязки к строке.
//
// Абзац склеивается прежде разреза: отрицание и его сужение сплошь и рядом стоят на
// РАЗНЫХ строках одного абзаца, и построчный разбор терял бы сужение — то есть
// краснел бы на верном тексте.
func denialSentences(body string) []denialSentence {
	var out []denialSentence
	lines := strings.Split(body, "\n")
	para := make([]string, 0, 8)
	start := 1
	flush := func() {
		if len(para) == 0 {
			return
		}
		joined := strings.Join(para, " ")
		offset := 0
		for _, piece := range splitSentences(joined) {
			// Строка = стартовая строка абзаца плюс переводы, «съеденные» до этого куска.
			out = append(out, denialSentence{text: piece, line: start + lineOffsetOf(para, offset)})
			offset += len(piece)
		}
		para = para[:0]
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			flush()
			start = i + 2
			continue
		}
		if len(para) == 0 {
			start = i + 1
		}
		para = append(para, ln)
	}
	flush()
	return out
}

// lineOffsetOf — на сколько строк вглубь абзаца попадает байтовое смещение.
func lineOffsetOf(para []string, offset int) int {
	acc := 0
	for i, ln := range para {
		acc += len(ln) + 1 // +1 — склеивающий пробел
		if offset < acc {
			return i
		}
	}
	if len(para) == 0 {
		return 0
	}
	return len(para) - 1
}

func splitSentences(s string) []string {
	var out []string
	cur := strings.Builder{}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		cur.WriteRune(runes[i])
		isEnd := strings.ContainsRune(".!?;:", runes[i])
		if isEnd && i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\t') {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		if runes[i] == '|' {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func denialContainsAny(low string, needles []string) bool {
	return denialFirstMatch(low, needles) != ""
}

func denialFirstMatch(low string, needles []string) string {
	for _, n := range needles {
		if strings.Contains(low, n) {
			return strings.TrimSpace(n)
		}
	}
	return ""
}

// clientDocPages собирает клиентские страницы края и всех сервисов.
func clientDocPages(root string, list subscriptionDocsLister) ([]denialPage, []string, error) {
	var domains []string
	roots := []string{filepath.Join(root, "gateway", "docs", "content")}
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		domains = append(domains, e.Name())
		roots = append(roots, filepath.Join(root, "services", e.Name(), "docs", "content"))
	}
	sort.Strings(domains)
	var pages []denialPage
	for _, dir := range roots {
		files, listErr := list(dir, ".mdx", ".md")
		if listErr != nil {
			continue
		}
		for _, f := range files {
			body, readErr := os.ReadFile(f) // #nosec G304 -- путь из состава дерева
			if readErr != nil {
				return nil, nil, readErr
			}
			rel, _ := filepath.Rel(root, f)
			pages = append(pages, denialPage{rel: filepath.ToSlash(rel), body: string(body)})
		}
	}
	return pages, domains, nil
}

// denialMechanismDeclared — предпосылка гейта, измеренная по дереву.
func denialMechanismDeclared(root string, list subscriptionDocsLister) (int, int, error) {
	files, err := list(filepath.Join(root, "proto"), ".proto")
	if err != nil {
		return 0, 0, err
	}
	declared := 0
	for _, f := range files {
		body, readErr := os.ReadFile(f) // #nosec G304 -- путь из состава дерева
		if readErr != nil {
			return 0, 0, readErr
		}
		declared += strings.Count(string(body), denialStreamMarker)
	}
	return declared, len(files), nil
}

// TestClientDocsDoNotDenyTheSubscriptionTheTreeHas — гейт.
func TestClientDocsDoNotDenyTheSubscriptionTheTreeHas(t *testing.T) {
	root := repoRoot(t)
	list := subscriptionDocsLister(treecorpus.UnderWithSuffix)

	declared, protoRead, err := denialMechanismDeclared(root, list)
	if err != nil {
		t.Fatalf("состав контрактов у корпуса дерева: %v", err)
	}
	pages, domains, err := clientDocPages(root, list)
	if err != nil {
		t.Fatalf("состав клиентской документации: %v", err)
	}
	findings, census := clientDocsDenialFindings(pages, domains)

	narrowed := 0
	kinds := make([]string, 0, len(census.narrowed))
	for k, v := range census.narrowed {
		narrowed += v
		kinds = append(kinds, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(kinds)
	t.Logf("перепись: контрактов осмотрено %d · объявлений серверного стрима %d · "+
		"доменов %d %v · клиентских страниц %d · предложений %d · "+
		"отрицаний рядом с именем механизма %d · из них СУЖЕННЫХ (истинных) %d %v · находок %d",
		protoRead, declared, len(domains), domains, census.pages, census.sentences,
		census.candidates, narrowed, kinds, census.findings)

	if protoRead == 0 || census.pages == 0 || census.sentences == 0 {
		t.Fatal("обход пуст — прочитано ноль, и зелёное здесь неотличимо от неосмотренного дерева")
	}
	if declared == 0 {
		t.Fatal("в контракте НЕТ ни одного объявления серверного стрима — предпосылка гейта " +
			"исчезла. Молчать здесь нельзя: если механизм действительно снят, отрицания в " +
			"клиентской документации стали ПРАВДОЙ и их надо перечитать поимённо, а не " +
			"оставить под запретом, которому больше нечего запрещать")
	}
	if census.candidates == 0 {
		t.Fatal("ни одного отрицания рядом с именем механизма не найдено во всей клиентской " +
			"документации — распознаватель ослеп: истинные отрицания («подписки на операции " +
			"нет») из дерева не исчезали, и их отсутствие означает сломанный обход, а не чистоту")
	}
	for _, f := range findings {
		t.Error(f)
	}
}
