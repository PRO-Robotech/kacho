// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzmapgen_test

// typetablesurvey_injection_test.go — доказательство того, что перепись таблиц
// типов СПОСОБНА упасть и способна смолчать (задача #1092).
//
// Инъекция подаёт синтетический каталог ТЕМ ЖЕ путём, каким гейт подаёт
// настоящий, поэтому доказательство относится к `SurveyTypeTables`, а не к её
// копии. Каждая проба снимает СВОЁ свойство у пакета, чьи остальные свойства
// на месте: карты добавляются и переносятся между файлами, а не заводится
// синтетический пакет целиком под каждый случай.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmapgen"
)

// injTypes — набор типов, которым судится синтетика. Тот же вид имени, что у
// модели: буквы и подчёркивание, точки НЕТ никогда.
var injTypes = map[string]struct{}{
	"vpc_network": {}, "vpc_subnet": {}, "nlb_listener": {},
}

// injHandWritten — рукописный файл: тип стоит ЗНАЧЕНИЕМ (точечное имя → тип).
const injHandWritten = `package authzmap

var objectTypes = map[string]string{
	"vpc.network": "vpc_network",
	"vpc.subnet":  "vpc_subnet",
}
`

// injGenerated — порождённый файл: тип стоит КЛЮЧОМ (набор отношений типа).
// Обе стороны обязаны опознаваться: таблицы держат тип по-разному, и вопрос к
// одной стороне не увидел бы половину предмета.
const injGenerated = `package authzmap

var typeVerbRelations = map[string][]string{
	"vpc_network": {"v_get"},
	"vpc_subnet":  {"v_get"},
}
`

// injNotATypeTable — законный близнец: package-level карта, типами НЕ
// являющаяся. Её ключи и значения — дотированные `модуль.ресурс` и проза, то
// есть ровно то, чем набиты соседние карты настоящего пакета.
const injNotATypeTable = `package authzmap

var catalogSpellingByServiceName = map[string]string{
	"loadbalancer.listener": "loadbalancer.listeners",
}

var catalogResourcesWithoutOwnService = map[string]string{
	"registry.repositories": "объект составной, службы в контрактах нет",
}
`

// injSecondTableGenerated — вторая таблица УЕХАЛА в порождённый файл (#1930
// закрыт). Рукописных ноль, и объявленный остаток обязан покраснеть.
const injSecondTableGenerated = `package authzmap

var typeVerbRelations = map[string][]string{
	"vpc_network": {"v_get"},
}

var objectTypes = map[string]string{
	"vpc.network": "vpc_network",
}
`

// injThirdHandWritten — в пакет ВЕРНУЛАСЬ рукописная таблица типов. Вывод
// перестал быть единственным источником.
const injThirdHandWritten = `package authzmap

var materializableTypes = map[string]bool{
	"vpc_network":  true,
	"nlb_listener": true,
}
`

// writePkg — синтетический каталог продукта. Имя порождённого файла берётся у
// того же объявления, что читает гейт: второе объявление развело бы их молча.
func writePkg(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("пишу %s: %v", name, err)
		}
	}
	return dir
}

func generatedBase() string { return filepath.Base(authzmapgen.GeneratedRelPath) }

func survey(t *testing.T, files map[string]string) authzmapgen.TypeTableSurvey {
	t.Helper()
	s, err := authzmapgen.SurveyTypeTables(writePkg(t, files), injTypes)
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	return s
}

// TestTypeTableSurveyCountsBothSidesAndIgnoresNonTypeMaps — КОНТРОЛЬ: пакет,
// каков он есть. Одна порождённая, одна рукописная, соседние карты не в счёт.
func TestTypeTableSurveyCountsBothSidesAndIgnoresNonTypeMaps(t *testing.T) {
	s := survey(t, map[string]string{
		generatedBase(): injGenerated,
		"fga_types.go":  injHandWritten,
		"spelling.go":   injNotATypeTable,
	})
	if s.FilesRead != 3 {
		t.Fatalf("прочитано %d файлов из трёх — обход не видит предмета", s.FilesRead)
	}
	if s.MapsRead != 4 {
		t.Fatalf("прочитано %d package-level карт из четырёх — половина предмета "+
			"не осмотрена, и «таблиц типов две» относилось бы к непрочитанному", s.MapsRead)
	}
	if s.Generated != 1 || s.HandWritten != 1 {
		t.Fatalf("порождено %d, рукописно %d — ожидались 1 и 1 (%v)",
			s.Generated, s.HandWritten, s.HandWrittenNames)
	}
	if len(s.HandWrittenNames) != 1 || s.HandWrittenNames[0] != "objectTypes" {
		t.Errorf("перепись не называет рукописную таблицу по имени: %v", s.HandWrittenNames)
	}
}

// TestTypeTableSurveyRedsWhenTheSecondTableBecomesGenerated — САМОИСТЕЧЕНИЕ.
//
// Это не «поломка», а цель: остаток обязан упасть до нуля вместе с #1930, и
// именно этот прогон заставит опустить объявленный остаток и правку прозы шапки.
func TestTypeTableSurveyRedsWhenTheSecondTableBecomesGenerated(t *testing.T) {
	s := survey(t, map[string]string{generatedBase(): injSecondTableGenerated})
	if s.Generated != 2 {
		t.Fatalf("порождённых таблиц %d из двух — инъекция беспредметна", s.Generated)
	}
	if s.HandWritten != 0 {
		t.Fatalf("рукописных %d при нуле ожидаемых (%v)", s.HandWritten, s.HandWrittenNames)
	}
	if s.HandWritten == handWrittenTypeTablesRemaining {
		t.Fatal("объявленный остаток совпал с нулём — самоистечение не сработало бы: " +
			"вывод второй таблицы прошёл бы молча")
	}
}

// TestTypeTableSurveyRedsWhenAHandWrittenTypeTableReturns — вторая сторона.
func TestTypeTableSurveyRedsWhenAHandWrittenTypeTableReturns(t *testing.T) {
	s := survey(t, map[string]string{
		generatedBase():  injGenerated,
		"fga_types.go":   injHandWritten,
		"materialize.go": injThirdHandWritten,
	})
	if s.HandWritten != 2 {
		t.Fatalf("рукописных %d из двух — вернувшаяся таблица не опознана (%v)",
			s.HandWritten, s.HandWrittenNames)
	}
	if s.HandWritten == handWrittenTypeTablesRemaining {
		t.Fatal("возврат рукописной таблицы прошёл бы молча")
	}
}

// TestTypeTableSurveyRefusesAnEmptyTypeSet — предпосылка САМОЙ переписи.
//
// На пустом наборе предикат «все элементы стороны суть типы» не выполняется
// никогда, и перепись объявила бы пакет свободным от таблиц, ничего не прочитав.
func TestTypeTableSurveyRefusesAnEmptyTypeSet(t *testing.T) {
	_, err := authzmapgen.SurveyTypeTables(writePkg(t, map[string]string{
		generatedBase(): injGenerated,
	}), nil)
	if err == nil {
		t.Fatal("пустой набор типов принят: перепись отдала бы «таблиц типов ноль» " +
			"вместо отказа")
	}
}

// TestTypeTableSurveySkipsTestFilesAndReportsAnEmptySweep — синтетика проб
// держит собственные карты типов, и их счёт сделал бы перепись функцией числа
// проб. Пустой обход обязан быть ВИДЕН числом, а не молчанием.
func TestTypeTableSurveySkipsTestFilesAndReportsAnEmptySweep(t *testing.T) {
	s := survey(t, map[string]string{"fga_types_test.go": injHandWritten})
	if s.FilesRead != 0 || s.MapsRead != 0 || s.Generated != 0 || s.HandWritten != 0 {
		t.Fatalf("тестовый файл прочитан: файлов %d, карт %d, порождено %d, рукописно %d",
			s.FilesRead, s.MapsRead, s.Generated, s.HandWritten)
	}
}
