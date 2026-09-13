// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

// producerFile — где живёт ПРОИЗВОДИТЕЛЬ строк журнала storage.
//
// У соседних владельцев производитель — Go-код репозитория, и их пробы разбирают
// синтаксическое дерево. Здесь производитель ДРУГОЙ: строку журнала пишет триггер
// базы, потому что писателей ресурсных строк у storage больше, чем репозиториев
// (сверщик мутирует `volumes`/`snapshots`/`images` мимо них, на голом пуле), а
// снятие в сверщике не несёт `RETURNING project_id` — якорь у него взять неоткуда.
// Значит и разбирать надо SQL миграции, а не Go.
const producerFile = "../migrations/20260828153423_storage_subscription_journal.sql"

// emitCall — вызов вставки строки журнала в теле триггерной функции. Слово рода
// изменения стоит в нём пятым значением.
var emitCall = regexp.MustCompile(`(?s)INSERT INTO kacho_storage\.storage_outbox\s*\([^)]*\)\s*VALUES\s*\((.*?)\);`)

// changeLiteral — слово рода изменения, как его пишет производитель.
var changeLiteral = regexp.MustCompile(`'(CREATED|UPDATED|DELETED)'`)

// checkVocabulary — закрытый словарь рода изменения на стороне базы.
var checkVocabulary = regexp.MustCompile(`(?s)CHECK\s*\(\s*event_type\s+IN\s*\((.*?)\)\s*\)`)

// kindArgument — вид предмета, которым триггер снабжается при навешивании.
var kindArgument = regexp.MustCompile(`EXECUTE FUNCTION kacho_storage\.storage_outbox_emit[a-z_]*\('([A-Za-z]+)'\)`)

// wordLiteral — одиночное слово в кавычках.
var wordLiteral = regexp.MustCompile(`'([A-Z_]+)'`)

func readProducer(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(producerFile)
	if err != nil {
		t.Fatalf("путь производителя не разрешился: %v", err)
	}
	body, err := os.ReadFile(src) // #nosec G304 -- путь константен и лежит в дереве
	if err != nil {
		t.Fatalf("файла производителя нет (%s): разбор судил бы пустоту — %v", producerFile, err)
	}
	return string(body)
}

// TestChangeDictionaryIsDerivedFromTheEmitter — словарь родов изменения сверяется
// с ПРОИЗВОДИТЕЛЕМ, а не со вторым рукописным перечнем.
//
// Утверждаются ОБЕ стороны:
//
//	каждое слово производителя названо словарём — иначе строка недоставляема,
//	                                              и потеря тихая;
//	каждое слово словаря имеет производителя    — иначе запись переживёт свой
//	                                              предмет и будет читаться как
//	                                              способность журнала.
//
// Пустой обход — отказ: ноль найденных вставок означает, что разбор сломан
// (переименовали таблицу, сменили форму вставки), и тогда «расхождений нет»
// получено даром.
func TestChangeDictionaryIsDerivedFromTheEmitter(t *testing.T) {
	body := readProducer(t)

	calls := emitCall.FindAllStringSubmatch(body, -1)
	if len(calls) == 0 {
		t.Fatalf("в %s не найдено ни одной вставки в журнал — разбор сломан, "+
			"и «расхождений нет» получено даром", producerFile)
	}

	produced := map[string]int{}
	for _, c := range calls {
		for _, w := range changeLiteral.FindAllStringSubmatch(c[1], -1) {
			produced[w[1]]++
		}
	}
	if len(produced) == 0 {
		t.Fatalf("вставок %d, а слов рода изменения ноль — разбор аргументов сломан", len(calls))
	}

	declared := Journal().Mapping.Changes
	for word := range produced {
		if declared[word] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			t.Errorf("производитель пишет род %q, а словарь его НЕ называет: строка с ним "+
				"недоставляема, и потеря эта тихая — ни отказа, ни пропуска в нумерации", word)
		}
	}
	for word := range declared {
		if produced[word] == 0 {
			t.Errorf("словарь называет род %q, которого производитель не пишет НИ РАЗУ: "+
				"запись пережила свой предмет и читается как способность журнала", word)
		}
	}

	words := make([]string, 0, len(produced))
	for w := range produced {
		words = append(words, w)
	}
	sort.Strings(words)
	t.Logf("осмотрено вставок производителя %d; слов рода изменения различных %d: %v; "+
		"объявлено словарём %d", len(calls), len(produced), words, len(declared))
}

// TestChangeWordsCoverTheDatabaseConstraint — словарь покрывает ЗАКРЫТЫЙ перечень
// базы.
//
// Ограничение — второй производитель того же предмета, и он строже эмиттера:
// эмиттер называет то, что пишет СЕГОДНЯ, ограничение — всё, что вообще может
// оказаться в колонке. Слово, разрешённое базой и не названное словарём, сделало
// бы строку недоставляемой при первом же новом писателе.
func TestChangeWordsCoverTheDatabaseConstraint(t *testing.T) {
	body := readProducer(t)

	m := checkVocabulary.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("в %s не найдено ограничение `CHECK (event_type IN (…))` — закрытого "+
			"словаря у колонки нет, и в неё уедет что угодно", producerFile)
	}
	allowed := wordLiteral.FindAllStringSubmatch(m[1], -1)
	if len(allowed) == 0 {
		t.Fatalf("ограничение найдено, а слов в нём ноль — разбор сломан")
	}

	declared := Journal().Mapping.Changes
	for _, w := range allowed {
		if declared[w[1]] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			t.Errorf("база разрешает род %q, а словарь его не называет: первый же писатель "+
				"этого слова сделал бы строку недоставляемой", w[1])
		}
	}
	for word := range declared {
		found := false
		for _, w := range allowed {
			if w[1] == word {
				found = true
			}
		}
		if !found {
			t.Errorf("словарь называет род %q, которого база НЕ принимает: запись не может "+
				"иметь предмета — вставка с этим словом отвергается ограничением", word)
		}
	}
	t.Logf("осмотрено слов ограничения %d; объявлено словарём %d", len(allowed), len(declared))
}

// TestKindWordsAreDerivedFromTheEmitter — словарь видов сверяется с тем, чем
// триггеры снабжены при навешивании.
//
// Слово вида задаётся аргументом триггера, а не выводится из имени таблицы:
// на `volume_attachments` навешен триггер, пишущий вид `Volume` — привязка есть
// изменение ТОМА, а не собственный предмет. Вывод из имени таблицы ошибся бы
// ровно здесь.
func TestKindWordsAreDerivedFromTheEmitter(t *testing.T) {
	body := readProducer(t)

	args := kindArgument.FindAllStringSubmatch(body, -1)
	if len(args) == 0 {
		t.Fatalf("в %s не найдено ни одного навешивания триггера эмиссии — разбор сломан", producerFile)
	}
	produced := map[string]int{}
	for _, a := range args {
		produced[a[1]]++
	}

	declared := Journal().Mapping.Kinds
	for word := range produced {
		if _, ok := declared[word]; !ok {
			t.Errorf("триггер пишет вид %q, а словарь его НЕ называет: строка с ним не имеет "+
				"типа объекта модели прав, вопрос о видимости задать нечем, и она не "+
				"доставляется", word)
		}
	}
	for word := range declared {
		if produced[word] == 0 {
			t.Errorf("словарь называет вид %q, которого не пишет НИ ОДИН триггер: запись "+
				"пережила свой предмет", word)
		}
	}

	words := make([]string, 0, len(produced))
	for w := range produced {
		words = append(words, w)
	}
	sort.Strings(words)
	t.Logf("осмотрено навешиваний триггера %d; видов различных %d: %v; объявлено словарём %d",
		len(args), len(produced), words, len(declared))
}
