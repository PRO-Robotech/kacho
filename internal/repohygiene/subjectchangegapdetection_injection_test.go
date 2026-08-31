// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subjectchangegapdetection_injection_test.go — ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ ГЕЙТА
// УПАСТЬ, и упасть ровно там, где написано.
//
// # Почему прогонов больше двух
//
// Утверждений у гейта четыре, и они об одном шве. Инъекция, роняющая сразу
// несколько, доказательством не является: красное пришло бы от соседа, а
// проверяемое утверждение могло бы при этом быть вакуумным и не показать этого
// ничем. Поэтому каждое звено снимается ОТДЕЛЬНО у дерева, где остальные ЦЕЛЫ, и
// от каждого прогона требуется РОВНО ОДНА находка — своя.
//
// Контроль (всё цело — молчат все) стоит первым: без него любая находка ниже
// означала бы лишь то, что гейт краснеет всегда.
//
// # Законный близнец — обязателен, иначе гейт ловит форму
//
// В синтетике лежит вторая функция, называющая тот же журнал и читающая его БЕЗ
// окна (снятие строк, счёт). Такая функция пропуска не создаёт, и гейт обязан о
// ней молчать: краснеющий на ней распознаватель ловил бы упоминание таблицы, а не
// чтение окном, — и первый же ложный срабат его отключил бы.

// gapTree — синтетическое дерево. Каждое поле снимает ОДНО звено.
type gapTree struct {
	// noFloor — окно чтения без вопроса о нижней границе.
	noFloor bool
	// floorViaAdvance — ВТОРАЯ законная форма наполнить границу: полное
	// наблюдение вместо узкого вопроса.
	floorViaAdvance bool
	// noProducer — отказ никем не производится.
	noProducer bool
	// noParser — отказ никем не разбирается.
	noParser bool
	// secondToken — признак полосы написан ВТОРОЙ раз.
	secondToken bool
	// noWindow — журнал окном не читается вовсе (предпосылка разбора).
	noWindow bool
	// floorViaObserve — ТРЕТЬЯ законная форма: наблюдение и ответ из него одним
	// вызовом (нужна там, где наблюдатель общий).
	floorViaObserve bool
	// floorBeforePage — пол берётся РАНЬШЕ страницы: дофиксовый порядок, дающий
	// молчаливый пропуск под конкурентной уборкой.
	floorBeforePage bool
}

func writeGapTree(t *testing.T, tree gapTree) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}

	// ── Пакет читателя: объявление признака и его разбор ─────────────────────
	token := "\tconst ReasonPositionLost = \"SUBJECT_CHANGE_POSITION_LOST\"\n"
	if tree.secondToken {
		token += "\tconst reasonCopy = \"SUBJECT_CHANGE_POSITION_LOST\"\n"
	}
	write("pkg/subjectchange/positionlost.go", ""+
		"package subjectchange\n\n"+
		"func Declare() string {\n"+token+"\treturn ReasonPositionLost\n}\n\n"+
		"func AsPositionLost(err error) (int64, bool) { return 0, err == nil }\n\n"+
		"func PositionLost(pos int64) error { return nil }\n")

	parse := "\tif _, ok := AsPositionLost(err); ok {\n\t\treturn 1\n\t}\n"
	if tree.noParser {
		parse = "\t_ = err\n"
	}
	write("pkg/subjectchange/watcher.go", ""+
		"package subjectchange\n\n"+
		"func Poll(err error) int {\n"+parse+"\treturn 0\n}\n")

	// ── Владелец журнала: окно чтения, пол и производство отказа ─────────────
	ask := "\t\th.RefreshEarliest()\n\t\tfloor := h.Floor()\n\t\t_ = floor\n"
	if tree.floorViaAdvance {
		ask = "\t\th.Advance()\n\t\tfloor := h.Floor()\n\t\t_ = floor\n"
	}
	if tree.floorViaObserve {
		ask = "\t\tfloor, _ := h.ObserveFloor()\n\t\t_ = floor\n"
	}
	if tree.noFloor {
		ask = ""
	}
	window := "`SELECT id, subject_id FROM kacho_iam.subject_change_outbox " +
		"WHERE id > $1 AND id <= $2 ORDER BY id ASC`"
	if tree.noWindow {
		window = "`SELECT count(*) FROM kacho_iam.subject_change_outbox`"
	}
	produce := "\t\treturn subjectchange.PositionLost(1)\n"
	if tree.noProducer {
		produce = "\t\treturn nil\n"
	}
	// Пол стоит ПОСЛЕ страницы — так исполняется дерево после фикса. Инъекция
	// `floorBeforePage` возвращает дофиксовый порядок, ничего больше не трогая.
	body := "\t\tif err := q.Query(" + window + "); err != nil {\n" + produce + "\t\t}\n" + ask
	if tree.floorBeforePage {
		body = ask + "\t\tif err := q.Query(" + window + "); err != nil {\n" + produce + "\t\t}\n"
	}
	write("services/owner/repo.go", ""+
		"package owner\n\n"+
		"import \"example.test/pkg/subjectchange\"\n\n"+
		"type watermark struct{}\n\n"+
		"func (watermark) RefreshEarliest() {}\n"+
		"func (watermark) Advance()         {}\n"+
		"func (watermark) Floor() int64     { return 0 }\n"+
		"func (watermark) ObserveFloor() (int64, error) { return 0, nil }\n\n"+
		"func Read(h watermark, q interface{ Query(string) error }) error {\n"+
		body+
		"\t\treturn nil\n}\n")

	// ── ЗАКОННЫЙ БЛИЗНЕЦ: тот же журнал, но не окном ────────────────────────
	write("services/owner/sweep.go", ""+
		"package owner\n\n"+
		"func Sweep(q interface{ Query(string) error }) error {\n"+
		"\t\treturn q.Query(`DELETE FROM kacho_iam.subject_change_outbox WHERE id <= $1`)\n}\n")

	return root
}

func auditGapTree(t *testing.T, tree gapTree) ([]SubjectChangeGapDetectionFinding, SubjectChangeGapDetectionCensus, error) {
	t.Helper()
	var log strings.Builder
	return AuditSubjectChangeGapDetection(SubjectChangeGapDetectionOptions{
		Root:          writeGapTree(t, tree),
		ReaderPackage: "pkg/subjectchange",
	}, &log)
}

// requireOneFindingAbout — ровно ОДНА находка, и она про названное.
func requireOneFindingAbout(t *testing.T, findings []SubjectChangeGapDetectionFinding, about string) {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась РОВНО одна (про %s): %v", len(findings), about, findings)
	}
	if !strings.Contains(findings[0].What, about) {
		t.Fatalf("находка не про %s: %s", about, findings[0].What)
	}
}

// TestGapDetectionInjection_IntactTreeIsSilent — ПРОГОН 1, контроль.
func TestGapDetectionInjection_IntactTreeIsSilent(t *testing.T) {
	findings, census, err := auditGapTree(t, gapTree{})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("целое дерево дало находки — гейт краснеет всегда: %v", findings)
	}
	// Законный близнец назвал журнал, но окном его не читает: в окна он попасть
	// не вправе, иначе гейт ловит упоминание таблицы, а не чтение.
	if len(census.Windows) != 1 {
		t.Errorf("окон %d, ожидалось 1: снятие строк окном чтения не является", len(census.Windows))
	}
	if census.JournalLiterals < 2 {
		t.Errorf("литералов, называющих журнал, %d — близнец не прочитан, и его молчание "+
			"ничего не доказывает", census.JournalLiterals)
	}
	if len(census.Producers) != 1 || len(census.Parsers) != 1 || len(census.TokenDeclaration) != 1 {
		t.Errorf("перепись шва: производителей %d, разборщиков %d, объявлений %d — ожидалось по одному",
			len(census.Producers), len(census.Parsers), len(census.TokenDeclaration))
	}
}

// TestGapDetectionInjection_WindowWithoutTheFloorIsAFinding — ПРОГОН 2: снято
// НОВОЕ звено у дерева, где остальные целы.
func TestGapDetectionInjection_WindowWithoutTheFloorIsAFinding(t *testing.T) {
	findings, census, err := auditGapTree(t, gapTree{noFloor: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	requireOneFindingAbout(t, findings, "нижняя удержанная граница не спрашивается")
	if !strings.Contains(findings[0].What, "services/owner/repo.go") {
		t.Errorf("находка не называет координату: %s", findings[0].What)
	}
	// Соседние звенья ЦЕЛЫ — значит красное пришло не от них.
	if len(census.Producers) != 1 || len(census.Parsers) != 1 || len(census.TokenDeclaration) != 1 {
		t.Errorf("инъекция задела соседние полосы: производителей %d, разборщиков %d, объявлений %d",
			len(census.Producers), len(census.Parsers), len(census.TokenDeclaration))
	}
}

// TestGapDetectionInjection_RefusalWithoutAProducerIsAFinding — ПРОГОН 3: снято
// СУЩЕСТВУЮЩЕЕ звено; окно при этом пол спрашивает, и утверждение о нём молчит.
func TestGapDetectionInjection_RefusalWithoutAProducerIsAFinding(t *testing.T) {
	findings, census, err := auditGapTree(t, gapTree{noProducer: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	requireOneFindingAbout(t, findings, "НЕ ПРОИЗВОДИТСЯ")
	if len(census.Windows) != len(census.WindowsAskFloor) {
		t.Errorf("инъекция задела полосу пола: окон %d, спрашивают пол %d",
			len(census.Windows), len(census.WindowsAskFloor))
	}
}

// TestGapDetectionInjection_RefusalNobodyParsesIsAFinding — вторая сторона шва.
func TestGapDetectionInjection_RefusalNobodyParsesIsAFinding(t *testing.T) {
	findings, _, err := auditGapTree(t, gapTree{noParser: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	requireOneFindingAbout(t, findings, "НЕ РАЗБИРАЕТСЯ")
}

// TestGapDetectionInjection_SecondSpellingOfTheTokenIsAFinding — два написания
// одного признака.
func TestGapDetectionInjection_SecondSpellingOfTheTokenIsAFinding(t *testing.T) {
	findings, _, err := auditGapTree(t, gapTree{secondToken: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	requireOneFindingAbout(t, findings, "объявлен 2 раз")
}

// TestGapDetectionInjection_NoWindowIsAnEmptyTraversalNotSilence — предпосылка
// разбора.
//
// Дерево, в котором журнал окном не читается, для этого гейта БЕСПРЕДМЕТНО: его
// молчание означало бы «проверять нечего», а не «нарушений нет». Это тот же
// класс, что «ноль находок неотличим от ноль прочитанного», и он обязан быть
// отказом обхода, а не зелёным вердиктом.
func TestGapDetectionInjection_NoWindowIsAnEmptyTraversalNotSilence(t *testing.T) {
	findings, census, err := auditGapTree(t, gapTree{noWindow: true})
	if err == nil {
		t.Fatalf("беспредметный обход дал вердикт вместо отказа: находок %d", len(findings))
	}
	if !strings.Contains(err.Error(), "ослеп") {
		t.Errorf("отказ не называет своей причины: %v", err)
	}
	if census.GoFiles == 0 {
		t.Error("объём осмотренного не назван — «ноль находок» неотличимо от «ноль прочитанного»")
	}
}

// TestGapDetectionInjection_FloorFilledByFullObservationIsSilent — ВТОРАЯ
// законная форма (`testing.md` §«Гейт на класс», п. 7).
//
// Нижнюю границу наполняют ДВЕ формы, и обе живут в дереве: полное наблюдение
// спрашивает максимум и минимум ОДНИМ запросом (так делает владелец журнала
// смены субъекта, у которого наблюдение идёт на каждый вызов), узкий вопрос —
// только минимум (так делает поток подписки, где полного наблюдения на каждой
// партии не происходит).
//
// Распознаватель, знающий одну форму, объявил бы вторую ОТСУТСТВУЮЩЕЙ — не
// нарушением, а невидимостью: всё, записанное во второй, ушло бы из наблюдения,
// а гейт остался бы зелёным. Проба требует МОЛЧАНИЯ, потому что вторая форма
// законна ровно так же, как первая.
func TestGapDetectionInjection_FloorFilledByFullObservationIsSilent(t *testing.T) {
	findings, census, err := auditGapTree(t, gapTree{floorViaAdvance: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("вторая законная форма объявлена нарушением — гейт ловит имя вызова, "+
			"а не наполнение границы: %v", findings)
	}
	if len(census.WindowsAskFloor) != 1 {
		t.Errorf("окон, спрашивающих пол, %d — вторая форма не засчитана, то есть невидима",
			len(census.WindowsAskFloor))
	}
}

// TestGapDetectionInjection_FloorTakenByOneObservingCallIsSilent — ТРЕТЬЯ
// законная форма (`testing.md` §«Гейт на класс», п. 7).
//
// Там, где наблюдатель ОБЩИЙ, пол обязан приходить из СВОЕГО наблюдения: общее
// поле пишется каждым проходом безусловно, поэтому проход, чей запрос отработал
// раньше, а запись легла позже, возвращает поле назад. Форма «наблюсти и
// ответить одним вызовом» — законный ответ на это, и распознаватель, её не
// знающий, объявил бы правильный код нарушением.
func TestGapDetectionInjection_FloorTakenByOneObservingCallIsSilent(t *testing.T) {
	findings, census, err := auditGapTree(t, gapTree{floorViaObserve: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("третья законная форма объявлена нарушением: %v", findings)
	}
	if len(census.WindowsAskFloor) != 1 {
		t.Errorf("окон, спрашивающих пол, %d — форма не засчитана, то есть невидима",
			len(census.WindowsAskFloor))
	}
}

// TestGapDetectionInjection_FloorTakenBeforeThePageIsAFinding — ПОРЯДОК.
//
// Это тот самый дефект, который прошёл локальный прогон и упал в конвейере: пол
// брался раньше страницы, между двумя запросами фиксировалась уборка, и страница
// со снятым префиксом уезжала как полная. Утверждение о ФОРМЕ его не ловит —
// вызовы на месте, оба; ловит только порядок.
//
// Инъекция трогает РОВНО порядок: тот же вызов пола, та же страница, тот же
// производитель отказа — переставлены местами два оператора.
func TestGapDetectionInjection_FloorTakenBeforeThePageIsAFinding(t *testing.T) {
	findings, census, err := auditGapTree(t, gapTree{floorBeforePage: true})
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	requireOneFindingAbout(t, findings, "пол берётся РАНЬШЕ страницы")
	// Соседние полосы целы — красное пришло от порядка, а не от них.
	if len(census.Producers) != 1 || len(census.Parsers) != 1 || len(census.TokenDeclaration) != 1 {
		t.Errorf("инъекция задела соседние полосы: производителей %d, разборщиков %d, объявлений %d",
			len(census.Producers), len(census.Parsers), len(census.TokenDeclaration))
	}
	// И окно распознано — иначе молчание объяснялось бы слепотой, а не порядком.
	if len(census.Windows) != 1 {
		t.Errorf("окон %d, ожидалось 1", len(census.Windows))
	}
}
