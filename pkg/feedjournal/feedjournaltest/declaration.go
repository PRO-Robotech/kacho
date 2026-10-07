// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package feedjournaltest

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/subscription"
)

// JournalDeclaration — объявление журнала модуля `journal.yaml` рядом с
// каталогом `notifications/` владельца: вход `notifygen init -journal`
// (corelib `cmd/notifygen`), из которого генератор выводит тело функции базы
// `resource-event` на таблице журнала (NTF-3 З10). Поля закрыты так же, как у
// читателя генератора: неизвестный ключ — отказ разбора.
type JournalDeclaration struct {
	Module  string                     `yaml:"module"`
	Table   string                     `yaml:"table"`
	Columns map[string]string          `yaml:"columns"`
	Kinds   map[string]KindDeclaration `yaml:"kinds"`
	Changes map[string]string          `yaml:"changes"`
}

// KindDeclaration — строка таблицы видов объявления: форма имени и якорь.
type KindDeclaration struct {
	NameForm string `yaml:"name_form"`
	Scope    string `yaml:"scope"`
}

// DeclarationOf — объявление, которое обязан нести `journal.yaml` модуля
// module с журналом j. Источник один — объявление журнала на Go
// (`subscription.Journal`, его читает сервер потока): таблица и колонки — из
// `Storage`, виды с формой имени и якорем — из `Mapping.Kinds`, род изменения —
// из `Mapping.Changes`. Журнал берётся при ВКЛЮЧЁННОМ флаге ленты: ключ строки
// сигнала (`feed.JournalKey`) объявлен только тогда, а генератор требует его в
// таблице видов.
//
// Отказ — объявление на Go не переводится в форму генератора: форма имени или
// якорь вида не объявлены, ключа строки сигнала нет.
func DeclarationOf(module string, j subscription.Journal) (JournalDeclaration, error) {
	if _, ok := j.Mapping.Kinds[feed.JournalKey]; !ok {
		return JournalDeclaration{}, fmt.Errorf("журнал %s без ключа строки сигнала %q: объявление берётся у журнала при включённом флаге ленты",
			j.Storage.Table, feed.JournalKey)
	}
	s := j.Storage
	d := JournalDeclaration{
		Module: module,
		Table:  s.Table,
		Columns: map[string]string{
			"kind": s.KindColumn, "id": s.IDColumn, "change": s.ChangeColumn, "payload": s.PayloadColumn,
			"project": s.ProjectColumn, "initiator": s.InitiatorColumn, "occurred_at": s.OccurredAtColumn,
		},
		Kinds:   make(map[string]KindDeclaration, len(j.Mapping.Kinds)),
		Changes: make(map[string]string, len(j.Mapping.Changes)),
	}
	for word, k := range j.Mapping.Kinds {
		var kd KindDeclaration
		switch k.NameForm {
		case subscription.NameFormDNS:
			kd.NameForm = "dns"
		case subscription.NameFormNone:
			kd.NameForm = "none"
		default:
			return JournalDeclaration{}, fmt.Errorf("вид %s журнала %s не объявил форму имени", word, s.Table)
		}
		switch k.Scope {
		case subscription.ScopeProject:
			kd.Scope = "project"
		case subscription.ScopeCluster:
			kd.Scope = "cluster"
		default:
			return JournalDeclaration{}, fmt.Errorf("вид %s журнала %s не объявил якорь", word, s.Table)
		}
		d.Kinds[word] = kd
	}
	for word, c := range j.Mapping.Changes {
		d.Changes[word] = c.String()
	}
	return d, nil
}

// RequireJournalDeclaration — файл path несёт ровно объявление DeclarationOf
// (module, j). Расхождение — красный с ожидаемым содержимым: объявление на Go
// и вход генератора о том же журнале — два места об одном предмете, и правка
// одного без другого дала бы функцию базы, переводящую строку журнала иначе,
// чем сервер потока.
func RequireJournalDeclaration(t testing.TB, path, module string, j subscription.Journal) {
	t.Helper()
	want, err := DeclarationOf(module, j)
	if err != nil {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: объявление журнала на Go не переводится в форму генератора: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- путь задаёт проба модуля, а не ввод
	if err != nil {
		t.Fatalf("%s не читается — функции resource-event модуля %s не из чего выводиться: %v", path, module, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var got JournalDeclaration
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("%s: объявление журнала не читается: %v", path, err)
	}
	if !reflect.DeepEqual(got, want) {
		exp, merr := yaml.Marshal(want)
		if merr != nil {
			t.Fatalf("ожидаемое объявление не печатается: %v", merr)
		}
		t.Fatalf("%s расходится с объявлением журнала %s на Go — ожидалось:\n%s", path, j.Storage.Table, exp)
	}
}
