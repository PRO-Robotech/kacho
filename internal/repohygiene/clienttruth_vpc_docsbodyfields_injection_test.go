// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_vpc_docsbodyfields_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clienttruth_vpc_docsbodyfields_test.go`) о способности падать не
// говорит ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ, и каждая снимает РОВНО ОДНО свойство у элемента,
// чьи остальные свойства целы.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docsBodyStand — синтетическое дерево: контракт с двумя маршрутами и страница,
// где каждый пример верен. Это ЗАКОННОЕ состояние, и на нём анализатор обязан
// молчать.
type docsBodyStand struct{ root string }

func newDocsBodyStand(t *testing.T) *docsBodyStand {
	t.Helper()
	s := &docsBodyStand{root: t.TempDir()}

	// Контракт. `ProbeDeleteRequest` объявлен в ОДНУ строку: не закрыв такое
	// сообщение сразу, разбор считал бы своими все последующие строки файла — и
	// поля настоящих сообщений терялись бы молча. Именно так анализатор дал шесть
	// ложных находок на настоящем дереве, прежде чем это было починено.
	s.write(t, "proto/kacho/cloud/probevpc/v1/thing_service.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

service ProbeThingService {
  rpc Create(ProbeCreateRequest) returns (Operation) {
    option (google.api.http) = {
      post: "/probevpc/v1/things"
      body: "*"
    };
  }
  rpc AddBlocks(ProbeAddRequest) returns (Operation) {
    option (google.api.http) = {
      post: "/probevpc/v1/things/{thing_id}:add-blocks"
      body: "*"
    };
  }
}

message ProbeDeleteRequest { string thing_id = 1; }

message ProbeCreateRequest {
  // В комментарии стоит фигурная скобка { — и она НЕ код. Анализатор,
  // считающий скобки вместе с прозой, приписал бы поля ниже другому сообщению.
  string project_id = 1;
  string name = 2;
  map<string, string> labels = 3;
  string ipv4_cidr_primary = 4;
  ProbeNested nested = 5;
}

message ProbeNested {
  string inner_key = 1;
}

message ProbeAddRequest {
  string thing_id = 1;
  repeated string v4_cidr_blocks = 2;
}
`)
	s.write(t, "services/probevpc/docs/content/api/thing.mdx", "```\n"+`
    curl -X POST http://localhost:18080/probevpc/v1/things \\
      -H 'Authorization: Bearer <JWT>' \\
      -d '{
        "projectId": "{projectId}",
        "name": "prod-thing",
        "labels": { "env": "prod" },
        "ipv4CidrPrimary": "10.0.0.0/24",
        "nested": { "innerKey": "не судится — принадлежит другому сообщению" }
      }'

    curl -X POST http://localhost:18080/probevpc/v1/things/{thingId}:add-blocks \\
      -d '{ "v4CidrBlocks": ["10.1.0.0/24"] }'
`+"\n```\n")
	return s
}

func (s *docsBodyStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *docsBodyStand) run(t *testing.T) ([]DocsBodyFieldFinding, DocsBodyFieldCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditDocsBodyFields(DocsBodyFieldOptions{
		Tree:      clientTruthSyntheticTree(t, s.root),
		ProtoRoot: "proto",
		DocRoots:  []string{"services/probevpc/docs/content"},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestDocsBodyInjection_CleanStandIsSilent — контроль. Без него всякая
// последующая краснота неотличима от анализатора, краснеющего на всём.
func TestDocsBodyInjection_CleanStandIsSilent(t *testing.T) {
	s := newDocsBodyStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве находок %d, ожидался ноль: %v", len(findings), findings)
	}
	if census.Routes != 2 {
		t.Fatalf("маршрутов %d, ожидалось 2 — таблица маршрутов собрана не вся", census.Routes)
	}
	// Два тела: многострочное и однострочное. Однострочное — отдельный случай:
	// закрывающая кавычка стоит на той же строке, и разбор, читающий следующую,
	// подхватил бы соседний блок страницы.
	if census.Bodies != 2 || census.Routed != 2 || census.Unrouted != 0 {
		t.Fatalf("тел %d (ожидалось 2), сопоставлено %d, без маршрута %d",
			census.Bodies, census.Routed, census.Unrouted)
	}
	// Судятся пять ключей верхнего уровня: projectId, name, labels,
	// ipv4CidrPrimary, nested — плюс один у второго тела. Вложенный `innerKey`
	// НЕ судится: он принадлежит другому сообщению.
	if census.Keys != 6 {
		t.Fatalf("ключей рассужено %d, ожидалось 6 — вложенный ключ попал в счёт "+
			"либо верхний потерян", census.Keys)
	}
}

// TestDocsBodyInjection_RetiredFieldIsFound — ИНЪЕКЦИЯ: у существующего примера
// меняется ОДНО свойство — имя ключа на снятое с контракта. Остальное цело.
func TestDocsBodyInjection_RetiredFieldIsFound(t *testing.T) {
	s := newDocsBodyStand(t)
	s.write(t, "services/probevpc/docs/content/api/thing.mdx", "```\n"+`
    curl -X POST http://localhost:18080/probevpc/v1/things \\
      -d '{
        "projectId": "{projectId}",
        "v4CidrBlocks": ["10.0.0.0/24"]
      }'
`+"\n```\n")
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	f := findings[0]
	if !strings.Contains(f.File, "thing.mdx") || f.Line == 0 {
		t.Fatalf("координата не названа: %+v", f)
	}
	if f.Key != "v4CidrBlocks" || f.Snake != "v4_cidr_blocks" || f.Request != "ProbeCreateRequest" {
		t.Fatalf("находка не называет предмет: %+v", f)
	}
}

// TestDocsBodyInjection_SameKeyLegalOnAnotherRoute — ЗАКОННЫЙ БЛИЗНЕЦ той же
// формы: `v4CidrBlocks` у создания — находка (см. выше), у добавления блоков —
// верное поле. Пара доказывает, что судится ПРЕДМЕТ (поле этого запроса), а не
// имя ключа само по себе.
func TestDocsBodyInjection_SameKeyLegalOnAnotherRoute(t *testing.T) {
	s := newDocsBodyStand(t)
	s.write(t, "services/probevpc/docs/content/api/thing.mdx", "```\n"+`
    curl -X POST http://localhost:18080/probevpc/v1/things/{thingId}:add-blocks \\
      -d '{ "v4CidrBlocks": ["10.1.0.0/24"] }'
`+"\n```\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d на законном близнеце, ожидался ноль: %v", len(findings), findings)
	}
	if census.Keys != 1 {
		t.Fatalf("ключей рассужено %d, ожидался 1 — близнец не рассужен вовсе", census.Keys)
	}
}

// TestDocsBodyInjection_ResponseBlockIsNotJudged — окно поиска тела узкое. Широкий
// поиск уезжает за конец команды и подхватывает следующий блок страницы — обычно
// пример ОТВЕТА; тогда анализатор судит ключи ответа против полей запроса. На
// настоящем дереве это давало три ложные находки (`id`, `done`, `metadata`).
func TestDocsBodyInjection_ResponseBlockIsNotJudged(t *testing.T) {
	s := newDocsBodyStand(t)
	s.write(t, "services/probevpc/docs/content/api/thing.mdx", "```\n"+`
    curl -X POST http://localhost:18080/probevpc/v1/things \\
      -d '{ "projectId": "{projectId}", "name": "prod-thing" }'
`+"\n```\n\nОтвет — операция:\n\n```\n"+`
    {
      "id": "{operationId}",
      "done": false,
      "metadata": { "thingId": "{thingId}" }
    }
`+"\n```\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d, ожидался ноль — разбор ушёл в блок ответа: %v", len(findings), findings)
	}
	if census.Keys != 2 {
		t.Fatalf("ключей рассужено %d, ожидалось 2 — в счёт попал ответ", census.Keys)
	}
}

// TestDocsBodyInjection_SingleLineMessageDoesNotSwallowTheFile — сообщение в одну
// строку закрывается сразу. Инъекция снимает свойство у РАЗБОРА: если бы оно
// пропало, поля объявленных ниже сообщений терялись бы и каждый верный ключ
// становился находкой.
func TestDocsBodyInjection_SingleLineMessageDoesNotSwallowTheFile(t *testing.T) {
	s := newDocsBodyStand(t)
	// Однострочное сообщение поставлено ПЕРЕД тем, чьи поля судятся: без
	// закрытия оно поглотило бы их все.
	s.write(t, "proto/kacho/cloud/probevpc/v1/thing_service.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

service ProbeThingService {
  rpc Create(ProbeCreateRequest) returns (Operation) {
    option (google.api.http) = { post: "/probevpc/v1/things" body: "*" };
  }
}

message ProbeSwallow { string a = 1; }
message ProbeEmpty {}

message ProbeCreateRequest {
  string project_id = 1;
  string name = 2;
}
`)
	s.write(t, "services/probevpc/docs/content/api/thing.mdx", "```\n"+`
    curl -X POST http://localhost:18080/probevpc/v1/things \\
      -d '{ "projectId": "p", "name": "n" }'
`+"\n```\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d, ожидался ноль — однострочное сообщение поглотило файл: %v",
			len(findings), findings)
	}
	if census.Keys != 2 {
		t.Fatalf("ключей рассужено %d, ожидалось 2", census.Keys)
	}
}

// TestDocsBodyInjection_UnroutedPathIsCounted — путь, не совпавший ни с одним
// шаблоном, не судится, и число таких печатается. Молчание без счёта было бы
// неотличимо от «судили и не нашли».
func TestDocsBodyInjection_UnroutedPathIsCounted(t *testing.T) {
	s := newDocsBodyStand(t)
	s.write(t, "services/probevpc/docs/content/api/thing.mdx", "```\n"+`
    curl -X POST http://localhost:18080/probevpc/v1/ghosts \\
      -d '{ "whateverKey": "x" }'
`+"\n```\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d, ожидался ноль", len(findings))
	}
	if census.Bodies != 1 || census.Unrouted != 1 || census.Routed != 0 {
		t.Fatalf("тел %d, без маршрута %d, сопоставлено %d — счёт несопоставленных не ведётся",
			census.Bodies, census.Unrouted, census.Routed)
	}
}

// TestDocsBodyInjection_EmptyWalkIsVisible — «ноль находок» обязано быть отличимо
// от «ноль прочитанного».
func TestDocsBodyInjection_EmptyWalkIsVisible(t *testing.T) {
	s := &docsBodyStand{root: t.TempDir()}
	s.write(t, "proto/.keep", "")
	s.write(t, "services/probevpc/docs/content/.keep", "")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d на пустом дереве, ожидался ноль", len(findings))
	}
	if census.Routes != 0 || census.DocFiles != 0 || census.Bodies != 0 {
		t.Fatalf("перепись не показала пустоту: %+v", census)
	}
}
