// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notifytreegates_test.go — гейты дерева NTF-1 по дереву kacho (замысел #2915
// З16), исполняемые целью `make notify-tree-gates` (NTF1-D08, запись (2)):
//
//   - NTF1-B27 (гейт) — нормализация домена idna только в corelib/notify/address;
//   - NTF1-B27 (гейт, приёмник address.Domain);
//   - NTF1-B28 (гейт) — обход типобезопасности, семь видов;
//   - NTF1-B28 (гейт `Put`) — ссылка на feed.Put вне файлов генератора, вход —
//     вывод `notifygen -list` по этому же дереву;
//   - узел «ошибка Value() отброшена» (CX1-41 (б), УК46);
//   - NTF1-B19 — прямая запись в таблицы ленты мимо `feed` и перепись её
//     писателей.
//
// ТОНКИЕ ВЫЗЫВАЮЩИЕ. Узлы, перечни видов и расширений, ведомость писателей и
// реестр исключений живут в corelib (`treehygiene`); от дерева здесь только
// корень и каталог стабов. Копия перечня в этом дереве разошлась бы с corelib
// молча (УК50). Способность каждого узла падать доказана инъекциями в corelib
// (`treehygiene/notify_*_test.go`) на пине, из которого собрана проба; версия
// пина печатается каждым прогоном.
//
// Перечень проб этого файла и перечень цели `notify-tree-gates` — одно
// множество: расхождение — находка TestNTF1D08_NotifyTreeGatesRecipeRunsEveryGate.
package repohygiene_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treehygiene"
)

// notifyStubDir — каталог стабов дерева kacho: сгенерированный файл
// пропускается гейтами только здесь (Р7).
const notifyStubDir = "pkg/api"

// notifygenPkg — генератор постановки, исполняемый версией пина corelib из
// go.mod этого дерева. Тот же путь зовёт цель `notifications-check`.
const notifygenPkg = "github.com/PRO-Robotech/corelib/cmd/notifygen"

// kachoModule — путь модуля дерева, которое обязан обойти гейт.
const kachoModule = "github.com/PRO-Robotech/kacho"

// logCorelibPin печатает версию corelib, из которой собрана проба, — модуль,
// выбранный графом модулей дерева (`go list -m`), — и требует пин без
// `replace`. Версию из отчёта гейта (`debug.ReadBuildInfo`) тестовый бинарь
// не несёт: она печатается «не в графе модулей процесса».
func logCorelibPin(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}} {{with .Replace}}=> {{.Path}} {{.Version}}{{end}}", // #nosec G204 -- argv фиксирован
		"github.com/PRO-Robotech/corelib")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: версия corelib в графе модулей дерева не установлена: %v\n%s", err, stderr.String())
	}
	v := strings.TrimSpace(string(out))
	if !strings.HasPrefix(v, "v") || strings.Contains(v, "=>") {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: corelib в графе модулей дерева — %q, а не пин без replace", v)
	}
	t.Logf("corelib, из которого собрана проба (go list -m): %s", v)
}

// requireNotifyTreeCensus — объём осмотренного: «ноль находок» отличим от
// «ноль прочитанного»; рядом — версия corelib, из которой исполнен гейт.
func requireNotifyTreeCensus(t *testing.T, root string, c treehygiene.TreeCensus) {
	t.Helper()
	logCorelibPin(t, root)
	if c.Module != kachoModule {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: обойдён модуль %q, а не %s", c.Module, kachoModule)
	}
	if c.Packages == 0 || c.Files == 0 {
		t.Fatalf("пустой обход — не вердикт: %s", c)
	}
	if c.Generated == 0 {
		t.Fatalf("обход не тот: пропущено сгенерированных 0 — стабы %s есть в дереве, и не увидеть их "+
			"значит не прочитать каталог стабов: %s", notifyStubDir, c)
	}
	if c.BuildFailures != 0 {
		t.Fatalf("пакетов с отказом сборки %d — их узлы не осмотрены: %s", c.BuildFailures, c)
	}
}

// notifygenList — вывод `notifygen -list` по дереву root версией пина.
// Генератор, не исполнившийся, — отказ прогона, а не пустое множество.
func notifygenList(t *testing.T, root string) []byte {
	t.Helper()
	cmd := exec.Command("go", "run", notifygenPkg, "-list", "-root", root) // #nosec G204 -- argv фиксирован, корень — дерево пробы
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: notifygen -list по %s: %v\n%s", root, err, stderr.String())
	}
	if len(bytes.TrimSpace(out)) == 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: notifygen -list по %s напечатал пустое множество, "+
			"а шаблон services/notify/notifications есть в дереве", root)
	}
	return out
}

func reportNotifyFindings(t *testing.T, gate string, fs []treehygiene.Finding) {
	t.Helper()
	for _, f := range fs {
		t.Errorf("%s · %s", gate, f)
	}
}

// TestNTF1B27Gate_OnKacho — NTF1-B27 (гейт) по дереву kacho: ссылок на функции
// idna вне corelib/notify/address 0.
func TestNTF1B27Gate_OnKacho(t *testing.T) {
	root := repoRootFor(t)
	r, err := treehygiene.AuditIDNASingular(root, notifyStubDir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(r.String())
	requireNotifyTreeCensus(t, root, r.Census)
	reportNotifyFindings(t, "NTF1-B27 (гейт)", r.Findings)
}

// TestNTF1B27Receiver_OnKacho — NTF1-B27 (гейт, приёмник address.Domain) по
// дереву kacho: узлов приёмника 0.
func TestNTF1B27Receiver_OnKacho(t *testing.T) {
	root := repoRootFor(t)
	r, err := treehygiene.AuditDomainReceivers(root, notifyStubDir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(r.String())
	requireNotifyTreeCensus(t, root, r.Census)
	reportNotifyFindings(t, "NTF1-B27 (гейт, приёмник address.Domain)", r.Findings)
}

// TestNTF1B28Gate_OnKacho — NTF1-B28 (гейт) по дереву kacho: узлов обхода
// типобезопасности 0 по каждому из семи видов, стабы pkg/api пропущены.
func TestNTF1B28Gate_OnKacho(t *testing.T) {
	root := repoRootFor(t)
	r, err := treehygiene.AuditTypeSafetyBypass(root, notifyStubDir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(r.String())
	requireNotifyTreeCensus(t, root, r.Census)
	if r.Parsed == 0 {
		t.Fatalf("обход не тот: разобрано без ограничений сборки 0 файлов")
	}
	reportNotifyFindings(t, "NTF1-B28 (гейт)", r.Findings)
}

// TestNTF1B28PutGate_OnKacho — NTF1-B28 (гейт `Put`) по дереву kacho: ссылок
// на feed.Put вне файлов генератора 0; вход — вывод генератора пина. Файл
// генератора в дереве есть (шаблон пробы-источника), и ссылка в нём обязана
// быть увидена: ноль ссылок в файлах генератора значил бы, что вход -list
// обхода не достиг.
func TestNTF1B28PutGate_OnKacho(t *testing.T) {
	root := repoRootFor(t)
	r, err := treehygiene.AuditFeedPutReferences(root, notifyStubDir, notifygenList(t, root))
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(r.String())
	requireNotifyTreeCensus(t, root, r.Census)
	if r.GeneratorFiles == 0 || r.RefsInGenerated == 0 {
		t.Fatalf("обход не тот: файлов генератора %d, ссылок на feed.Put в них %d — "+
			"порождённый файл шаблона пробы-источника есть в дереве", r.GeneratorFiles, r.RefsInGenerated)
	}
	reportNotifyFindings(t, "NTF1-B28 (гейт Put)", r.Findings)
}

// TestUK46ValueErrorDiscard_OnKacho — узел «ошибка Value() отброшена» по
// дереву kacho (CX1-41 (б)).
func TestUK46ValueErrorDiscard_OnKacho(t *testing.T) {
	root := repoRootFor(t)
	r, err := treehygiene.AuditValueErrorDiscard(root, notifyStubDir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(r.String())
	requireNotifyTreeCensus(t, root, r.Census)
	reportNotifyFindings(t, "УК46", r.Findings)
}

// TestNTF1B19FeedWriters_OnKacho — NTF1-B19 по дереву kacho: прямой записи в
// таблицы ленты мимо `feed` нет; перепись писателей печатается. Имя таблицы
// ленты производит внутренний пакет corelib, и вне `notify/feed` его не
// импортирует компилятор, поэтому писатель в kacho — только находка.
func TestNTF1B19FeedWriters_OnKacho(t *testing.T) {
	root := repoRootFor(t)
	r, err := treehygiene.AuditFeedTableWrites(root, notifyStubDir)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(r.String())
	requireNotifyTreeCensus(t, root, r.Census)
	if r.SQLFiles == 0 {
		t.Fatalf("пустой обход SQL — не вердикт: миграции kacho отслеживаются, а осмотрено файлов *.sql 0")
	}
	reportNotifyFindings(t, "NTF1-B19", r.Findings)
}
