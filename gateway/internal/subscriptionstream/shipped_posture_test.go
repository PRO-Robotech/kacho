// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// shipped_posture_test.go — ОБЪЯВЛЕННАЯ посадка действительно снимает `501`.
//
// # Предмет — разрыв, невидимый ни с одной стороны по отдельности
//
// Код ручки был верен и покрыт пробами: пустой словарь владельцев отвечает `501`,
// непустой открывает поток. Объявление чарта было валидно и рендерилось. А между
// ними никто не смотрел — и перечень владельцев прожил всю жизнь пустым, то есть
// возможность отвечала `501` на любом стенде, включая боевой (kacho#1388).
//
// Ни одна проба края покраснеть не могла: каждая утверждала о своей половине.
// Поэтому здесь вопрос ставится СКВОЗЬ ОБЕ: взять имена из того самого
// объявления, которым поднимается стенд, собрать ручку ровно с ними и спросить
// её так, как спросит клиент.
//
// # Почему подставной владелец, а не живой
//
// Предмет — не поведение владельца, а то, что ОБЪЯВЛЕННОЕ ИМЯ доезжает до
// словаря ручки. Живой владелец потребовал бы поднятого стенда, то есть пробы,
// которая пропускается там, где её нет, — а пропущенная проба не краснеет
// никогда.

// edgeChartValues — объявление чарта края от каталога этого пакета.
const edgeChartValues = "../../deploy/values.yaml"

// ownersDeclaredByEdgeChart читает перечень владельцев так же, как его читает
// край: пустые элементы отбрасываются, поэтому вырожденное значение даёт НОЛЬ
// имён при непустой строке.
func ownersDeclaredByEdgeChart(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(edgeChartValues))
	if err != nil {
		t.Fatalf("объявление чарта края %s не читается (%v) — предпосылка пробы исчезла, "+
			"а не поставка стала полной", edgeChartValues, err)
	}
	var values struct {
		SubscriptionStream struct {
			Owners string `yaml:"owners"`
		} `yaml:"subscriptionStream"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("разбор объявления чарта края: %v", err)
	}
	names := make([]string, 0, 4)
	for _, part := range strings.Split(values.SubscriptionStream.Owners, ",") {
		if name := strings.TrimSpace(part); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// TestDeclaredPostureAnswersSomethingOtherThanNotImplemented — каждое имя,
// объявленное посадкой, ручка обслуживает.
//
// `501` здесь означает ровно одно: «владелец не объявлен». Получить его на имени,
// которое посадка НАЗВАЛА, значит, что объявление до словаря не доехало, — а
// именно этого разрыва не видит ни одна проба половины.
func TestDeclaredPostureAnswersSomethingOtherThanNotImplemented(t *testing.T) {
	declared := ownersDeclaredByEdgeChart(t)
	t.Logf("перепись: посадка называет владельцев %d %v", len(declared), declared)

	if len(declared) == 0 {
		t.Fatalf("объявление чарта края не называет НИ ОДНОГО владельца: ручка %s отвечает "+
			"501 на любом стенде, поднятом этим чартом, — возможность объявлена и "+
			"неисполнима", subscriptionstream.Path)
	}

	owners := make(subscriptionstream.Owners, len(declared))
	stub := &ownerStub{}
	for _, name := range declared {
		owners[name] = dialStub(t, stub)
	}
	h := newHandler(t, stub, func(cfg *subscriptionstream.Config) { cfg.Owners = owners })

	for _, name := range declared {
		rec := serve(t, h, request("owner="+name))
		if rec.Code == http.StatusNotImplemented {
			t.Errorf("посадка называет владельцем %q, а ручка отвечает 501 «владелец не "+
				"объявлен»: объявление до словаря не доехало", name)
		}
	}
}

// TestNotImplementedStillMeansTheOwnerIsUndeclared — контроль в обратную сторону.
//
// Без него утверждение выше зеленело бы на ручке, разучившейся отвечать `501`
// вовсе: «не 501» верно и тогда, когда этот код не производится ни при каких
// условиях. Здесь он производится — и ровно на том входе, который его означает.
func TestNotImplementedStillMeansTheOwnerIsUndeclared(t *testing.T) {
	h, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		StreamBudget: time.Second, Heartbeat: 100 * time.Millisecond,
		MaxStreams: 1, MaxStreamsPerSubject: 1,
	})
	if err != nil {
		t.Fatalf("сборка ручки без владельцев обязана проходить: %v", err)
	}
	rec := serve(t, h, request("owner=compute"))
	t.Logf("перепись: владельцев объявлено 0 · ответ %d", rec.Code)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("пустая посадка ответила %d вместо 501: тогда «не 501» в утверждении "+
			"выше ничего не означает", rec.Code)
	}
}
