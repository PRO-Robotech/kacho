// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// migrate_dsn_test.go — дверь ТОЧКИ НАКАТА: что она спрашивает и что не
// перестала спрашивать.
//
// Каждая проба ниже — ПАРА. Отрицание в одиночку зеленело бы на двери, которая
// отвергает всё, а утверждение в одиночку — на двери, которая не спрашивает
// ничего; вместе они называют границу.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfigFile — фикстура БЕЗ добавок: посадка задаётся телом дословно.
//
// Помощник соседнего файла дописывает объявление домена величин к любой
// фикстуре, у которой его нет, — и это верно для проб СЛУЖБЫ. Здесь предмет
// ровно обратный: конфигурация, которой посадки службы недостаёт.
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("фикстура не записана: %v", err)
	}
	return p
}

// postureLessDevYAML — всё, чем пользуется накат, и НИЧЕГО из посадки службы:
// ни объявления домена величин, ни круга отправителей чужой личности.
const postureLessDevYAML = "mode: dev\n" +
	"repository:\n  postgres:\n    url: postgres://u:p@h:5432/kacho_nlb?sslmode=disable\n"

// TestMigrateDSN_ReadsTheAddressWithoutTheServiceBootGuard — накат читает
// адрес; служба на той же строке отказывает.
//
// Пара отличается ОДНИМ фактом — вызванной дверью, — поэтому расхождение
// исходов означает ровно то, что объявлено, и ничего сверх.
func TestMigrateDSN_ReadsTheAddressWithoutTheServiceBootGuard(t *testing.T) {
	path := writeConfigFile(t, postureLessDevYAML)

	dsn, err := MigrateDSN(path)
	if err != nil {
		t.Fatalf("дверь наката отвергла конфигурацию, в которой есть всё, чем накат "+
			"пользуется:\n%v", err)
	}
	if !strings.HasPrefix(dsn, "postgres://") {
		t.Fatalf("адрес прочитан не тот: %q", dsn)
	}

	if _, lerr := Load(path); lerr == nil {
		t.Fatal("дверь СЛУЖБЫ приняла ту же конфигурацию — страж посадки ослаблен, " +
			"и первая половина пробы больше ничего не различает")
	} else if !strings.Contains(lerr.Error(), "quota.authority") {
		t.Fatalf("отказ двери службы не называет ручку:\n%v", lerr)
	}
}

// TestMigrateDSN_ProductionRefusesPlaintextToItsOwnDatabase — контроль, который
// накат НЕ отдал вместе с чужими.
//
// Соединение к собственной базе — его собственный периметр: по нему идут пароль
// и DDL. Снять эту ось заодно с посадкой службы значило бы разменять «отказ
// называет не тот предмет» на «контроля нет».
func TestMigrateDSN_ProductionRefusesPlaintextToItsOwnDatabase(t *testing.T) {
	const body = "mode: production\n" +
		"repository:\n  postgres:\n    url: postgres://u:p@h:5432/kacho_nlb?sslmode=disable\n"

	if _, err := MigrateDSN(writeConfigFile(t, body)); err == nil {
		t.Fatal("боевая посадка приняла открытый канал к собственной базе")
	} else if !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("отказ не называет ось — оператор не поймёт, что крутить:\n%v", err)
	}

	// Положительный близнец: та же строка с шифрованием обязана пройти. Без него
	// отрицание зеленело бы на двери, отвергающей боевую посадку целиком.
	secure := strings.Replace(body, "sslmode=disable", "sslmode=require", 1)
	if _, err := MigrateDSN(writeConfigFile(t, secure)); err != nil {
		t.Fatalf("боевая посадка с шифрованием отвергнута:\n%v", err)
	}
}

// TestMigrateDSN_UnknownModeIsRefusedNotReadAsNonProduction — величина, которая
// решает, применяется ли контроль, принимается ЯВНО.
//
// [Config.Mode] проглатывает ошибку разбора и отдаёт непроизводственный режим,
// поэтому опечатка в `mode:` снимала бы требование шифрования МОЛЧА: посадка
// названа боевой, контроль не исполняется, отказа нет.
func TestMigrateDSN_UnknownModeIsRefusedNotReadAsNonProduction(t *testing.T) {
	const body = "mode: prod\n" +
		"repository:\n  postgres:\n    url: postgres://u:p@h:5432/kacho_nlb?sslmode=disable\n"

	if _, err := MigrateDSN(writeConfigFile(t, body)); err == nil {
		t.Fatal("неизвестная посадка принята — на ней требование шифрования не " +
			"исполняется, и опечатка в профиле снимает контроль молча")
	} else if !strings.Contains(err.Error(), "prod") {
		t.Fatalf("отказ не называет непринятое значение:\n%v", err)
	}

	// Положительный близнец: известная непроизводственная посадка проходит с тем
	// же открытым каналом — иначе отрицание выше зеленело бы на строгости к
	// локальным фикстурам, а не к разбору посадки.
	dev := strings.Replace(body, "mode: prod\n", "mode: dev\n", 1)
	if _, err := MigrateDSN(writeConfigFile(t, dev)); err != nil {
		t.Fatalf("локальная посадка отвергнута:\n%v", err)
	}
}

// TestMigrateDSN_ExpandsThePasswordPlaceholder — подстановка пароля из
// окружения остаётся на пути наката.
//
// Чарт кладёт в файл настроек `$(KACHO_NLB_DB_PASSWORD)`, а пароль приходит
// Secret'ом отдельно. Дверь, потерявшая подстановку, отдала бы накату literal
// placeholder — и отказ пришёл бы с самого соединения, назвав предметом базу.
func TestMigrateDSN_ExpandsThePasswordPlaceholder(t *testing.T) {
	t.Setenv("KACHO_NLB_DB_PASSWORD", "s3cret")
	const body = "mode: production\n" +
		"repository:\n  postgres:\n" +
		"    url: postgres://u:$(KACHO_NLB_DB_PASSWORD)@h:5432/kacho_nlb?sslmode=require\n" +
		"    password-from-env: KACHO_NLB_DB_PASSWORD\n"

	dsn, err := MigrateDSN(writeConfigFile(t, body))
	if err != nil {
		t.Fatalf("MigrateDSN: %v", err)
	}
	if strings.Contains(dsn, "$(") {
		t.Fatalf("подстановка пароля потеряна — накат соединялся бы плейсхолдером: %q", dsn)
	}
	if !strings.Contains(dsn, "s3cret") {
		t.Fatalf("пароль из окружения не подставлен: %q", dsn)
	}
}

// TestMigrateDSN_EmptyAddressIsNotItsOwnRefusal — незаданный адрес отдаётся
// пустым, БЕЗ своей редакции отказа.
//
// У отказа «адрес не задан» один производитель — общий пакет, перечисляющий все
// источники приоритета. Вторая редакция назвала бы своё подмножество, и оператор
// прочитал бы в подсказке не тот перечень (#1544).
func TestMigrateDSN_EmptyAddressIsNotItsOwnRefusal(t *testing.T) {
	dsn, err := MigrateDSN(writeConfigFile(t, "mode: production\n"))
	if err != nil {
		t.Fatalf("дверь наката завела СВОЙ отказ на незаданном адресе:\n%v", err)
	}
	if dsn != "" {
		t.Fatalf("незаданный адрес прочитан непустым: %q", dsn)
	}
}
