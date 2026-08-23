// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionserversingularity_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, и того, что он молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`subscriptionserversingularity_test.go`) ничего не говорит о способности
// проверки падать — зелёный получает и та, что не смотрит никуда.
//
// Каждое утверждение стоит ПАРОЙ: внесённый дефект обязан краснеть И НАЗЫВАТЬ
// координату, а законный близнец той же формы — молчать. Без второй половины
// гейт ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stand — синтетическое дерево: контракт с одним потоковым глаголом и один
// сервер в фундаменте. Это ЗАКОННОЕ состояние, и на нём анализатор обязан
// молчать.
type serverStand struct {
	root string
}

func newServerStand(t *testing.T) *serverStand {
	t.Helper()
	root := t.TempDir()
	s := &serverStand{root: root}
	s.write(t, "proto/kacho/cloud/subscription/subscription.proto", `
syntax = "proto3";
package kacho.cloud.subscription;

// Здесь СЛОВО returns (stream — внутри комментария. Оно не объявление, и
// анализатор, считающий текст, покраснел бы на этом объяснении.
service SubscriptionService {
  rpc Subscribe(SubscriptionRequest) returns (stream SubscriptionMessage) {}
}
`)
	// Законный сервер в фундаменте.
	s.write(t, "pkg/subscription/server.go", `package subscription

type Server struct{}

func (s *Server) Subscribe(req *SubscriptionRequest, stream InternalSubscriptionService_SubscribeServer) error {
	return nil
}
`)
	// ЗАКОННЫЙ БЛИЗНЕЦ: метод с тем же именем и другим предметом. Их в дереве
	// много, и анализатор обязан их не замечать.
	s.write(t, "services/mail/internal/handler/handler.go", `package handler

type Mailer struct{}

func (m *Mailer) Subscribe(req *NewsletterRequest, stream Newsletter_SubscribeServer) error {
	return nil
}
`)
	return s
}

func (s *serverStand) write(t *testing.T, rel, body string) {
	t.Helper()
	path := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s *serverStand) audit(t *testing.T, allow ...SubscriptionStreamAllowance) []SubscriptionServerFinding {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditSubscriptionServerSingularity(SubscriptionServerOptions{
		Root:      s.root,
		ProtoRoot: "proto",
		GoRoots:   []string{"pkg", "services"},
		Allow:     allow,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v\n%s", err, log.String())
	}
	if census.ProtoFiles == 0 || census.GoFiles == 0 {
		t.Fatalf("стенд пуст: %s", log.String())
	}
	return findings
}

func requireQuietStand(t *testing.T, findings []SubscriptionServerFinding, why string) {
	t.Helper()
	if len(findings) != 0 {
		t.Fatalf("%s: анализатор нашёл %v", why, findings)
	}
}

func requireFinding(t *testing.T, findings []SubscriptionServerFinding, kind, mustName string) {
	t.Helper()
	for _, f := range findings {
		if f.Kind != kind {
			continue
		}
		if mustName == "" || strings.Contains(f.Where, mustName) || strings.Contains(f.What, mustName) {
			return
		}
	}
	t.Fatalf("находки %q с упоминанием %q нет; найдено: %v", kind, mustName, findings)
}

// A. Законное дерево — анализатор МОЛЧИТ.
//
// Первое утверждение и самое важное: без него всякое отрицание ниже зеленело бы
// на анализаторе, находящем всё подряд.
func TestServerStandIsQuietOnALegitimateTree(t *testing.T) {
	s := newServerStand(t)
	requireQuietStand(t, s.audit(t), "законное дерево: один глагол, один сервер в фундаменте, чужой Subscribe рядом")
}

// B. Второй сервер — В СЕРВИСЕ. Ровно то, ради чего гейт написан.
func TestServerStandRedsOnASecondServerInAService(t *testing.T) {
	s := newServerStand(t)
	s.write(t, "services/vpc/internal/handler/watch.go", `package handler

type Watch struct{}

func (w *Watch) Subscribe(req *SubscriptionRequest, stream InternalSubscriptionService_SubscribeServer) error {
	return nil
}
`)
	f := s.audit(t)
	requireFinding(t, f, "СЕРВЕР-НЕ-В-ФУНДАМЕНТЕ", "services/vpc/internal/handler/watch.go")
	requireFinding(t, f, "ВТОРОЙ-СЕРВЕР", "services/vpc")
}

// C. Второй сервер В САМОМ ФУНДАМЕНТЕ — тоже находка.
//
// Пара к B, и она несущая: гейт, проверяющий только «лежит ли в pkg», молчал бы
// на двух серверах рядом друг с другом — то есть на том же расхождении, только
// ближе.
func TestServerStandRedsOnASecondServerInsideTheFoundation(t *testing.T) {
	s := newServerStand(t)
	s.write(t, "pkg/subscription2/server.go", `package subscription2

type Other struct{}

func (o *Other) Subscribe(req *SubscriptionRequest, stream InternalSubscriptionService_SubscribeServer) error {
	return nil
}
`)
	f := s.audit(t)
	requireFinding(t, f, "ВТОРОЙ-СЕРВЕР", "pkg/subscription2")
	for _, x := range f {
		if x.Kind == "СЕРВЕР-НЕ-В-ФУНДАМЕНТЕ" {
			t.Fatalf("сервер в фундаменте назван лежащим не там: %v", x)
		}
	}
}

// D. Второй потоковый глагол в контракте — второй язык подписки.
func TestServerStandRedsOnASecondStreamingVerb(t *testing.T) {
	s := newServerStand(t)
	s.write(t, "proto/kacho/cloud/vpc/v1/watch.proto", `
syntax = "proto3";
package kacho.cloud.vpc.v1;
service InternalWatchService {
  rpc Watch(WatchRequest) returns (stream Event) {}
}
`)
	requireFinding(t, s.audit(t), "ВТОРОЙ-ГЛАГОЛ", "Watch")
}

// E. Тот же второй глагол С ЗАПИСЬЮ В ВЕДОМОСТИ — анализатор молчит.
//
// Пара к D. Без неё гейт нельзя было бы носить через фазу перевода доменов: он
// краснел бы на состоянии, которое сам же и предписывает пройти.
func TestServerStandIsQuietOnAnAllowedSecondVerb(t *testing.T) {
	s := newServerStand(t)
	s.write(t, "proto/kacho/cloud/vpc/v1/watch.proto", `
syntax = "proto3";
package kacho.cloud.vpc.v1;
service InternalWatchService {
  rpc Watch(WatchRequest) returns (stream Event) {}
}
`)
	requireQuietStand(t, s.audit(t, SubscriptionStreamAllowance{
		Method:  "Watch",
		File:    "proto/kacho/cloud/vpc/v1/watch.proto",
		Because: "домен переводится на общую форму задачей kacho#1019; истекает снятием глагола",
	}), "глагол, стоящий в ведомости")
}

// F. Запись ведомости, которой НЕЧЕГО исключать, — сама находка.
func TestServerStandRedsOnAnExpiredAllowance(t *testing.T) {
	s := newServerStand(t)
	requireFinding(t, s.audit(t, SubscriptionStreamAllowance{
		Method:  "Watch",
		File:    "proto/kacho/cloud/vpc/v1/watch.proto",
		Because: "домен переводится задачей kacho#1019",
	}), "ПОСЛАБЛЕНИЕ-ИСТЕКЛО", "Watch")
}

// G. Запись ведомости БЕЗ ПРИЧИНЫ объявлением не является.
func TestServerStandRedsOnAnAllowanceWithoutAReason(t *testing.T) {
	s := newServerStand(t)
	s.write(t, "proto/kacho/cloud/vpc/v1/watch.proto", `
syntax = "proto3";
package kacho.cloud.vpc.v1;
service InternalWatchService {
  rpc Watch(WatchRequest) returns (stream Event) {}
}
`)
	requireFinding(t, s.audit(t, SubscriptionStreamAllowance{Method: "Watch"}),
		"ПОСЛАБЛЕНИЕ-БЕЗ-ПРИЧИНЫ", "Watch")
}

// H. Глагол объявлен, СЕРВЕРА НЕТ — состояние между фазами, и оно обязано быть
// названо, а не молчаливо зелено.
func TestServerStandRedsWhenTheVerbHasNoServer(t *testing.T) {
	s := newServerStand(t)
	if err := os.Remove(filepath.Join(s.root, "pkg/subscription/server.go")); err != nil {
		t.Fatal(err)
	}
	requireFinding(t, s.audit(t), "СЕРВЕРА-НЕТ", "")
}

// I. Сервер есть, ГЛАГОЛА НЕТ — обратное состояние, и оно тоже названо.
func TestServerStandRedsWhenTheServerHasNoVerb(t *testing.T) {
	s := newServerStand(t)
	if err := os.Remove(filepath.Join(s.root,
		"proto/kacho/cloud/subscription/subscription.proto")); err != nil {
		t.Fatal(err)
	}
	s.write(t, "proto/kacho/cloud/geo/v1/geo.proto", "syntax = \"proto3\";\npackage kacho.cloud.geo.v1;\n")
	requireFinding(t, s.audit(t), "ГЛАГОЛА-НЕТ", "")
}

// J. Объявление В КОММЕНТАРИИ объявлением не является.
//
// Отдельным утверждением, потому что соседний анализатор этой же фазы на этом
// уже краснел — на собственном объяснении.
func TestServerStandIgnoresAStreamingVerbInAComment(t *testing.T) {
	s := newServerStand(t)
	s.write(t, "proto/kacho/cloud/vpc/v1/doc.proto", `
syntax = "proto3";
package kacho.cloud.vpc.v1;
// Прежде здесь стояло:
//   rpc Watch(WatchRequest) returns (stream Event) {}
/* и в блочном комментарии тоже:
   rpc Watch2(WatchRequest) returns (stream Event) {}
*/
`)
	requireQuietStand(t, s.audit(t), "потоковый глагол, стоящий в комментарии")
}

// K. Пустой обход — ОТКАЗ, а не «ноль находок».
func TestServerStandRefusesAnEmptyWalk(t *testing.T) {
	root := t.TempDir()
	var log strings.Builder
	_, census, err := AuditSubscriptionServerSingularity(SubscriptionServerOptions{
		Root:      root,
		ProtoRoot: "proto",
		GoRoots:   []string{"pkg"},
	}, &log)
	if err == nil {
		t.Fatalf("пустой обход прошёл: %+v", census)
	}
	if !strings.Contains(err.Error(), "неотличимо") {
		t.Fatalf("отказ не называет предмета: %v", err)
	}
}
