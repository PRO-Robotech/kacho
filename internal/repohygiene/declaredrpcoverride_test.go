// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEveryDeclaredRPCIsOverridden — свойство дерева: у каждого объявленного
// RPC есть написанный рукой ответ.
func TestEveryDeclaredRPCIsOverridden(t *testing.T) {
	rep, err := auditDeclaredRPCOverride(repoRoot(t))
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	t.Logf("перепись: proto-файлов %d, сервисов в них %d, go-файлов (не тест) %d, "+
		"типов-реализаций %d, методов сверено %d; чужих контрактов пропущено %d %v; находок %d",
		rep.ProtoFiles, rep.Services, rep.GoFiles, rep.ImplTypes, rep.RPCsChecked,
		len(rep.Foreign), rep.Foreign, len(rep.Missing))

	if rep.ProtoFiles == 0 || rep.GoFiles == 0 {
		t.Fatal("дерево не прочитано — «все методы переопределены» здесь означало бы " +
			"«ничего не читал»")
	}
	if rep.ImplTypes == 0 {
		t.Fatal("не найдено ни одного типа, встраивающего заглушку сервера — предмет гейта " +
			"исчез либо перестал опознаваться")
	}
	if rep.RPCsChecked == 0 {
		t.Fatal("ни одного метода не сверено — гейт молча проверяет пустоту")
	}

	for _, m := range rep.Missing {
		t.Errorf("%s.%s не переопределён типом %s (%s) — вызывающий получает "+
			"`UNIMPLEMENTED: method %s not implemented`, то есть отказ БЕЗ причины и без адреса "+
			"возможности.\n"+
			"Гейт не требует реализации: метод вправе отказывать. Но отказ пишется рукой — с "+
			"причиной, с указанием владельца возможности и под пробой, утверждающей СООБЩЕНИЕ "+
			"(у названного и унаследованного отказа код один и тот же). Образцы: "+
			"services/compute/internal/handler/declared_but_absent.go и хвост "+
			"services/vpc/internal/apps/kacho/api/routetable/handler.go",
			m.Service, m.Method, m.Type, m.Where, m.Method)
	}
}

// --- инъекция: обе стороны гоняют ТУ ЖЕ функцию, что и гейт по дереву ---

func synthOverrideTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		// Минимальный контракт: сервис с двумя методами.
		"proto/kacho/cloud/x/v1/x_service.proto": `syntax = "proto3";
package kacho.cloud.x.v1;

service XService {
  rpc Alpha (AlphaRequest) returns (AlphaResponse) {}
  rpc Beta (BetaRequest) returns (BetaResponse) {}
}

message AlphaRequest {}
message AlphaResponse {}
message BetaRequest {}
message BetaResponse {}
`,
	}
	for rel, body := range base {
		writeSynthFile(t, root, rel, body)
	}
	for rel, body := range files {
		writeSynthFile(t, root, rel, body)
	}
	// gateway обязан быть непустым: обход требует состав у индекса и отказывает
	// на пустом — «смотреть не на что» есть отказ, а не успех.
	writeSynthFile(t, root, "gateway/seed.go", "package seed\n")
	synthTrack(t, root)
	return root
}

func writeSynthFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

const overrideSrcPartial = `package h

type XHandler struct {
	UnimplementedXServiceServer
}

func (h *XHandler) Alpha(req int) int { return 0 }
`

const overrideSrcFull = `package h

type XHandler struct {
	UnimplementedXServiceServer
}

func (h *XHandler) Alpha(req int) int { return 0 }

func (h *XHandler) Beta(req int) int { return 0 }
`

// Сторона дефекта: непереопределённый метод — находка С КООРДИНАТОЙ.
func TestOverrideGateRedOnInheritedMethod(t *testing.T) {
	root := synthOverrideTree(t, map[string]string{"services/x/handler.go": overrideSrcPartial})
	rep, err := auditDeclaredRPCOverride(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(rep.Missing) != 1 {
		t.Fatalf("ожидалась одна находка (Beta), получено %d: %+v", len(rep.Missing), rep.Missing)
	}
	if rep.Missing[0].Method != "Beta" {
		t.Fatalf("гейт назвал не тот метод: %+v", rep.Missing[0])
	}
	if rep.Missing[0].Where != "services/x/handler.go" {
		t.Fatalf("находка без координаты типа: %+v", rep.Missing[0])
	}
}

// Законный близнец: оба метода переопределены — гейт молчит.
//
// Без этой половины гейт ловил бы факт встраивания заглушки, а не отсутствие
// метода, и краснел бы на КАЖДОМ обработчике дерева.
func TestOverrideGateSilentWhenAllOverridden(t *testing.T) {
	root := synthOverrideTree(t, map[string]string{"services/x/handler.go": overrideSrcFull})
	rep, err := auditDeclaredRPCOverride(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(rep.Missing) != 0 {
		t.Fatalf("гейт нашёл дефект в законном дереве: %+v", rep.Missing)
	}
	if rep.RPCsChecked != 2 {
		t.Fatalf("сверено методов %d, ожидалось 2 — молчание относится не к тому, "+
			"что проверяли", rep.RPCsChecked)
	}
}
