// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Обратное чтение приводит нули края обратно в «не задано».
//
// Край отдаёт незаданные поля нулями — пустой строкой у чужой формы адресации и нулём у
// веса. Записанные значением, они объявили бы пользователю то, чего он не писал, и
// разошлись бы с настройкой на первом же плане.
func TestApplyTargetGroupMapsEdgeZeroesToNull(t *testing.T) {
	raw := []byte(`{"id":"tgr1","projectId":"prj1","regionId":"ru-central1","name":"tg",
		"port":80,"status":"ACTIVE","deregistrationDelay":"0s","slowStart":"0s",
		"healthCheck":{"interval":"5s","timeout":"2s","healthyThreshold":"2",
			"unhealthyThreshold":"3","effectivePort":80,"http":{"port":"0","path":"","headers":{}}},
		"targets":[{"externalIp":{"address":"203.0.113.10","zoneId":"ru-central1-a"},
			"instanceId":"","nicId":"","weight":0,"status":"ACTIVE"}]}`)

	var m targetGroupModel
	if err := applyTargetGroup(context.Background(), &m, raw); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	ts := targetsOf(context.Background(), m.Targets)
	if len(ts) != 1 {
		t.Fatalf("целей: %d, ожидалась 1", len(ts))
	}
	if !ts[0].InstanceID.IsNull() || !ts[0].NicID.IsNull() {
		t.Errorf("пустая чужая форма адресации записана значением: instance=%v nic=%v",
			ts[0].InstanceID, ts[0].NicID)
	}
	if !ts[0].Weight.IsNull() {
		t.Errorf("нулевой вес записан значением: %v", ts[0].Weight)
	}
	// Парный положительный контроль: заданное значение НЕ теряется. Без него проба выше
	// зеленела бы на функции, которая обнуляет всё подряд.
	if got := ts[0].ExternalIP; got == nil || got.Address.ValueString() != "203.0.113.10" {
		t.Errorf("заданный адрес потерян: %+v", got)
	}
	// Порог приезжает строкой (protojson кодирует 64-разрядные целые строкой) и обязан
	// стать числом: иначе состояние не соберётся.
	if m.HealthCheck.UnhealthyThreshold.ValueInt64() != 3 {
		t.Errorf("порог из строки не разобран: %v", m.HealthCheck.UnhealthyThreshold)
	}
}

// Ключ цели включает ВЕС — и это следствие измеренного поведения края, а не вкус:
// добавление на уже существующую личность край принимает как no-op и вес НЕ меняет.
// Значит смена веса обязана выражаться снятием и добавлением.
func TestTargetKeyDistinguishesWeight(t *testing.T) {
	a := targetModel{ExternalIP: &extIPModel{Address: types.StringValue("203.0.113.10"),
		ZoneID: types.StringValue("z")}, Weight: types.Int64Value(50)}
	b := a
	b.Weight = types.Int64Value(70)
	if targetKey(a) == targetKey(b) {
		t.Fatal("цели с разным весом дали один ключ — смена веса не породила бы ни снятия, ни добавления")
	}
	// Парный положительный: одинаковые цели дают один ключ, иначе каждый план пересобирал бы состав.
	c := a
	if targetKey(a) != targetKey(c) {
		t.Fatal("одинаковые цели дали разные ключи")
	}
}

// Состав приводится СНАЧАЛА снятием, потом добавлением.
//
// Порядок не произволен: добавление на существующую личность — no-op, поэтому обратный
// порядок оставил бы прежний вес и молча разошёлся бы с настройкой.
func TestReconcileRemovesBeforeAdding(t *testing.T) {
	var order []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, ":removeTargets"):
			order = append(order, "remove")
		case strings.HasSuffix(req.URL.Path, ":addTargets"):
			order = append(order, "add")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"op1","done":true,"metadata":{}}`))
	}))
	defer srv.Close()

	r := &targetGroupResource{c: mustProviderClient(t, srv.URL)}
	have := []targetModel{extTarget("203.0.113.10", 50)}
	want := []targetModel{extTarget("203.0.113.10", 70)}
	if err := r.reconcileTargets(context.Background(), "tgr1", have, want); err != nil {
		t.Fatalf("приведение состава: %v", err)
	}
	if len(order) != 2 || order[0] != "remove" || order[1] != "add" {
		t.Fatalf("порядок действий: %v, ожидалось [remove add]", order)
	}
}

// Обратное чтение ждёт, пока состав сойдётся.
//
// Замер на живом крае: операция снятия объявляется завершённой через ~0,2 с, а цель
// исчезает из чтения через ~1,3 с. Без ожидания Terraform останавливается на собственной
// несогласованности — «unexpected new value: .targets length changed».
func TestAwaitTargetsWaitsOutTheStaleRead(t *testing.T) {
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := reads.Add(1)
		targets := `{"externalIp":{"address":"203.0.113.10","zoneId":"z"},"weight":0},` +
			`{"externalIp":{"address":"203.0.113.11","zoneId":"z"},"weight":0}`
		if n > 2 { // с третьего чтения снятие видно
			targets = `{"externalIp":{"address":"203.0.113.10","zoneId":"z"},"weight":0}`
		}
		_, _ = w.Write([]byte(`{"id":"tgr1","name":"tg","healthCheck":{},"targets":[` + targets + `]}`))
	}))
	defer srv.Close()

	r := &targetGroupResource{c: mustProviderClient(t, srv.URL)}
	if _, err := r.awaitTargets(context.Background(), "tgr1",
		[]targetModel{extTarget("203.0.113.10", 0)}); err != nil {
		t.Fatalf("ожидание не сошлось: %v", err)
	}
	if reads.Load() < 3 {
		t.Fatalf("чтений было %d — ожидание не состоялось, проба зеленела бы и без него", reads.Load())
	}
}

// Зеркальная проба: состав, который не сходится НИКОГДА, обязан кончиться отказом с
// числами, а не молчаливой записью в состояние того, чего у края нет.
func TestAwaitTargetsFailsLoudlyWhenItNeverConverges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"tgr1","name":"tg","healthCheck":{},"targets":[
			{"externalIp":{"address":"203.0.113.99","zoneId":"z"},"weight":0}]}`))
	}))
	defer srv.Close()

	r := &targetGroupResource{c: mustProviderClient(t, srv.URL), targetsBudget: 1500 * time.Millisecond}
	_, err := r.awaitTargets(context.Background(), "tgr1", []targetModel{extTarget("203.0.113.10", 0)})
	if err == nil {
		t.Fatal("несошедшийся состав принят за сошедшийся")
	}
	// Утверждается ИМЕННО отказ по сроку с числами: «вернулось не nil» зеленело бы и на
	// обрыве соединения, то есть не проверяло бы сходимость вовсе.
	if !strings.Contains(err.Error(), "не сошёлся") || !strings.Contains(err.Error(), "задано 1") {
		t.Fatalf("отказ не называет предмет: %v", err)
	}
}

func extTarget(addr string, weight int64) targetModel {
	w := types.Int64Null()
	if weight != 0 {
		w = types.Int64Value(weight)
	}
	return targetModel{
		InstanceID: types.StringNull(), NicID: types.StringNull(),
		ExternalIP: &extIPModel{Address: types.StringValue(addr), ZoneID: types.StringValue("z")},
		Weight:     w,
	}
}

func mustProviderClient(t *testing.T, endpoint string) *client.Client {
	t.Helper()
	c, err := client.New(client.Config{Endpoint: endpoint, Token: "t"})
	if err != nil {
		t.Fatalf("клиент: %v", err)
	}
	return c
}
