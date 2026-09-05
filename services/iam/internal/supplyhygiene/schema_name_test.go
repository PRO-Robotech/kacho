// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

// schema_name_test.go — схема Postgres зовётся именем СВОЕГО продукта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба получила собственное имя продукта — Kaname, — а её схема продолжала
// звать себя именем платформы с суффиксом домена. Имя схемы не деталь
// хранилища: оно стоит в `search_path` каждого соединения, в квалификаторе
// каждого оператора, в тексте каждой миграции и в каждом отказе, где Postgres
// называет объект. То есть это ровно то, чем продукт себя называет, — а не код,
// который он исполняет.
//
// Различение проведено по вопросу владельца: имя, которым продукт себя
// называет, — своё; код, который он исполняет, — берётся у платформы как есть.
// Поэтому имена ФУНКЦИЙ общего фундамента внутри схемы (учёт величин, проверка
// меток) остаются прежними: они рендерятся одним шаблоном на шесть владельцев,
// и их байт-идентичность держит отдельный гейт.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — три вещи, и все три вместе
//
//  1. каноническое имя схемы ПРИСУТСТВУЕТ в дереве службы. Без этого условия
//     проверка зеленела бы на дереве, из которого вынесли всё разом: отрицание
//     без положительного близнеца есть вакуумное утверждение;
//  2. отставленного имени схемы в дереве службы НЕТ ни в одном виде — ни
//     квалификатором оператора, ни значением `search_path`, ни строкой запроса
//     к системному каталогу, ни прозой;
//  3. перепись НАЗЫВАЕТСЯ числами: файлов прочитано, вхождений канонического,
//     вхождений отставленного, пропущено по каждой причине. Пустой обход —
//     отказ, иначе «ноль находок» неотличимо от «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛЬ ОБЯЗАН ЗНАТЬ СОСЕДЕЙ, ДЕЛЯЩИХ С ИМЕНЕМ СХЕМЫ ПРИСТАВКУ
//
// Главный способ ошибиться здесь — судить по приставке. Приставку с именем
// схемы делят ТРИ соседних пространства имён, и ни одно из них схемой не
// является:
//
//	kacho_iam_<слово>   канал уведомления · имя метрики · тип ресурса
//	                    провайдера инфраструктуры. Пространства свои,
//	                    потребители свои, гейты свои — их переименование есть
//	                    предмет других полос, и захват их сюда сделал бы вердикт
//	                    этой непрослеживаемым;
//	/kacho_iam          имя БАЗЫ в строке подключения. База и схема — разные
//	name: kacho_iam     объекты Postgres; схема `kaname` внутри базы прежнего
//	database: kacho_iam имени работает, и половинчатость здесь названа, а не
//	                    получена молча. Форм записи у имени базы ДВЕ, и знать
//	                    обязаны обе: последний сегмент строки подключения и
//	                    значение ключа профиля развёртывания. Форма, о которой
//	                    распознаватель не знает, даёт не красное и не зелёное,
//	                    а находку на месте, где нарушения нет.
//
// Обе полосы распознаватель ПРОПУСКАЕТ и считает пропущенное отдельным числом —
// «пропущено 0» отличимо от «полоса не рассматривалась».
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРЕТЬЯ ПРОПУЩЕННАЯ ПОЛОСА — ОДОБРЕННЫЕ ПРИЁМКИ, и причина у неё ДРУГАЯ
//
// Каталог приёмок службы пропускается не потому, что имя схемы там иное, а
// потому, что правка одобренного документа ОТЗЫВАЕТ его вердикт: одобрение
// относится к точному содержимому, а не к файлу по имени. Массовая правка
// обратила бы двадцать четыре шапки APPROVED в утверждение об одобрении текста,
// которого никто не читал.
//
// Отдельно: приёмки несут ЗАПИСИ ЗАМЕРОВ со своими предикатами вида
// `git grep … kacho_iam.<таблица>` и ссылки `git show <ревизия>:…`. Такой
// предикат привязан к прошлому состоянию дерева, и правка имени сделала бы
// ложным утверждение, которое было верным.
//
// Полоса СЧИТАЕТСЯ отдельным числом переписи — иначе «в приёмках 0» стало бы
// неотличимо от «приёмки не рассматривались».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕТВЁРТАЯ ПОЛОСА — САМА ПРОВЕРКА И ЕЁ ДОКАЗАТЕЛЬСТВА
//
// Проверка обязана НАЗЫВАТЬ то, что запрещает: без имени отставленной схемы у
// распознавателя нет входа, у инъекции нет дефекта, а у сквозной пробы нет
// способа утверждать, что прежней схемы в базе НЕТ. Судя себя, проверка
// краснела бы на собственном объяснении — тот же класс, что гейт, ищущий слово
// в сыром тексте и находящий его в комментарии рядом.
//
// Полоса задана ПЕРЕЧНЕМ ФАЙЛОВ, а не признаком формы: признак («строка
// объявляет константу») освободил бы и всякий прод-код, объявивший ту же
// строку, то есть ровно тех, кого проверка и заводилась ловить. Перечень
// закрыт, назван здесь и сосчитан отдельным числом; файл ВНЕ него судится как
// любой другой, и это доказано инъекцией.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ЗАКРЫВАЕТ — сказано прямо
//
// Она судит ДЕРЕВО СЛУЖБЫ и молчит обо всём, что лежит вне его корня: о гейтах
// монорепо, о профилях развёртывания, о перечне владельцев учёта в общем
// фундаменте. Там имя схемы тоже стоит, и правится оно тем же изменением, но
// предикатом этой проверки не удержано: у неё нет доступа к чужому корню.
package supplyhygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
	"github.com/stretchr/testify/require"
)

// canonicalSchema — имя схемы Postgres службы. ОДНО объявление на дерево: и
// проверка, и её инъекция читают отсюда, поэтому «что проверяется» и «что
// объявлено» разойтись не могут.
const canonicalSchema = "kaname"

// retiredSchema — имя, которое схема носила, пока служба звалась доменом
// платформы. Оставлено здесь не памяти ради: это ВХОД распознавателя, и без
// него проверка не смогла бы назвать координату находки.
const retiredSchema = "kacho_iam"

// schemaCensus — объём осмотренного. Печатается всегда, включая зелёный
// прогон: «ноль находок» обязано быть отличимо от «ноль прочитанного».
type schemaCensus struct {
	filesRead        int // файлов дерева прочитано
	filesBinary      int // пропущено как двоичные
	canonicalHits    int // вхождений канонического имени схемы
	retiredHits      int // вхождений отставленного имени схемы
	skippedNeighbour int // пропущено: сосед по приставке (канал · метрика · тип провайдера)
	skippedDatabase  int // пропущено: имя базы в строке подключения
	skippedApproved  int // пропущено: одобренная приёмка (вердикт привязан к отпечатку)
	filesApproved    int // файлов одобренных приёмок пропущено целиком
	skippedOwn       int // пропущено: сама проверка и её доказательства
	filesOwn         int // файлов перечня «предмет = это переименование»
}

// checkOwnFiles — файлы, ПРЕДМЕТ которых есть само переименование: сама
// проверка, её доказательство инъекцией и сквозная проба, утверждающая
// отсутствие прежней схемы в поднятой базе. Каждый обязан называть
// отставленное имя, чтобы делать свою работу.
var checkOwnFiles = map[string]bool{
	"internal/supplyhygiene/schema_name_test.go":                true,
	"internal/supplyhygiene/schema_name_injection_test.go":      true,
	"cmd/kaname/schema_raised_from_scratch_integration_test.go": true,
}

// isCheckOwnFile — файл принадлежит перечню выше.
func isCheckOwnFile(rel string) bool { return checkOwnFiles[filepath.ToSlash(rel)] }

// approvedAcceptanceDir — каталог одобренных приёмок службы относительно её
// корня. Объявлен ОДИН раз: и обход, и перепись читают отсюда.
const approvedAcceptanceDir = "docs/engineering/acceptance/"

// inApprovedAcceptance — файл лежит в каталоге одобренных приёмок.
func inApprovedAcceptance(rel string) bool {
	return strings.HasPrefix(filepath.ToSlash(rel), approvedAcceptanceDir)
}

// schemaFinding — одно вхождение отставленного имени схемы.
type schemaFinding struct {
	file string
	line int
	text string
}

func (f schemaFinding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.file, f.line, strings.TrimSpace(f.text))
}

// isWordByte — байт, продолжающий идентификатор Postgres.
func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// classifySchemaHit судит ОДНО вхождение отставленной приставки по соседям
// слева и справа. Три исхода, и «сосед» отличается от «находки» тем, что за ним
// стоит СВОЁ пространство имён со своими потребителями.
type hitKind int

const (
	hitSchema    hitKind = iota // имя схемы — находка
	hitNeighbour                // сосед по приставке: канал · метрика · тип провайдера
	hitDatabase                 // имя базы в строке подключения
)

func classifySchemaHit(line string, at int) hitKind {
	end := at + len(retiredSchema)

	// Продолжение идентификатора вправо: `kacho_iam_<слово>` — соседнее
	// пространство имён, не схема.
	if end < len(line) && isWordByte(line[end]) {
		return hitNeighbour
	}
	// Косая черта слева: последний сегмент строки подключения — имя БАЗЫ.
	if at > 0 && line[at-1] == '/' {
		return hitDatabase
	}
	// Значение ключа профиля развёртывания (`name:` под разделом `db:`,
	// `database:` у поставщика Postgres) — вторая форма записи имени БАЗЫ.
	// Судится ВЕСЬ отступ слева: иначе под правило попала бы проза, где те же
	// слова стоят посреди предложения.
	if isDatabaseKeyValue(line[:at]) {
		return hitDatabase
	}
	return hitSchema
}

// isDatabaseKeyValue — префикс строки есть ровно ключ профиля со своим отступом
// и ничего сверх: `  name: ` либо `  database: `.
func isDatabaseKeyValue(prefix string) bool {
	rest := strings.TrimLeft(prefix, " \t-")
	for _, key := range [...]string{"name:", "database:"} {
		if !strings.HasPrefix(rest, key) {
			continue
		}
		if strings.TrimLeft(rest[len(key):], " ") == "" {
			return true
		}
	}
	return false
}

// scanSchemaName разбирает ПРОИЗВОЛЬНЫЙ корень: настоящее дерево службы и
// синтетический корень инъекции проходят одну и ту же функцию, поэтому
// доказанное на втором верно для первого.
func scanSchemaName(tree *treecorpus.Tree) (schemaCensus, []schemaFinding, error) {
	var census schemaCensus
	var findings []schemaFinding

	root := tree.Root()
	for _, rel := range tree.SortedFiles() {
		if inSkippedDir(rel) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return census, nil, fmt.Errorf("схема: файл %s не прочитан: %w", rel, err)
		}
		if strings.IndexByte(string(raw), 0) >= 0 {
			census.filesBinary++
			continue
		}
		if inApprovedAcceptance(rel) {
			census.filesApproved++
			census.skippedApproved += strings.Count(string(raw), retiredSchema)
			continue
		}
		if isCheckOwnFile(rel) {
			census.filesOwn++
			census.skippedOwn += strings.Count(string(raw), retiredSchema)
			continue
		}
		census.filesRead++

		for i, line := range strings.Split(string(raw), "\n") {
			census.canonicalHits += countCanonical(line)

			for at := 0; ; {
				idx := strings.Index(line[at:], retiredSchema)
				if idx < 0 {
					break
				}
				abs := at + idx
				switch classifySchemaHit(line, abs) {
				case hitNeighbour:
					census.skippedNeighbour++
				case hitDatabase:
					census.skippedDatabase++
				case hitSchema:
					census.retiredHits++
					findings = append(findings, schemaFinding{file: rel, line: i + 1, text: line})
				}
				at = abs + len(retiredSchema)
			}
		}
	}

	if census.filesRead == 0 {
		return census, nil, fmt.Errorf("схема: обход пуст — вердикт беспредметен (корень %q)", root)
	}
	return census, findings, nil
}

// countCanonical считает вхождения канонического имени как ОТДЕЛЬНОГО
// идентификатора: `kaname` внутри более длинного слова именем схемы не
// является, и засчитывать его в положительный контроль значило бы позволить
// контролю выполниться чем угодно.
func countCanonical(line string) int {
	n := 0
	for at := 0; ; {
		idx := strings.Index(line[at:], canonicalSchema)
		if idx < 0 {
			return n
		}
		abs := at + idx
		end := abs + len(canonicalSchema)
		leftOK := abs == 0 || !isWordByte(line[abs-1])
		rightOK := end >= len(line) || !isWordByte(line[end])
		if leftOK && rightOK {
			n++
		}
		at = end
	}
}

// TestServiceSchemaIsNamedForItsOwnProduct — гейт класса.
func TestServiceSchemaIsNamedForItsOwnProduct(t *testing.T) {
	tree, err := treecorpus.NewTree(serviceRoot)
	require.NoError(t, err, "состав дерева службы не собран — вердикт беспредметен")

	census, findings, err := scanSchemaName(tree)
	require.NoError(t, err)

	t.Logf("перепись: файлов прочитано %d · двоичных пропущено %d · "+
		"вхождений %q %d · вхождений %q %d · пропущено соседей по приставке %d · "+
		"пропущено имён базы %d · пропущено одобренных приёмок %d файлов (%d вхождений) · "+
		"пропущено файлов самой проверки %d (%d вхождений)",
		census.filesRead, census.filesBinary,
		canonicalSchema, census.canonicalHits,
		retiredSchema, census.retiredHits,
		census.skippedNeighbour, census.skippedDatabase,
		census.filesApproved, census.skippedApproved,
		census.filesOwn, census.skippedOwn)

	require.NotZero(t, census.canonicalHits,
		"положительный контроль пуст: канонического имени схемы %q в дереве нет вовсе — "+
			"отрицание ниже выполнилось бы на дереве, из которого вынесли всё", canonicalSchema)

	if len(findings) > 0 {
		shown := findings
		if len(shown) > 20 {
			shown = shown[:20]
		}
		var b strings.Builder
		for _, f := range shown {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("схема службы зовётся отставленным именем %q в %d местах "+
			"(показаны первые %d):%s\n\nимя схемы — то, чем продукт себя называет; "+
			"канон объявлен константой canonicalSchema в этом файле",
			retiredSchema, len(findings), len(shown), b.String())
	}
}
