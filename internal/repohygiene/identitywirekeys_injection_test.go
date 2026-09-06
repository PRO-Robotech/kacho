// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identitywirekeys_injection_test.go — доказательство того, что
// TestIdentityWireNamespaceIsDeclaredOnce СПОСОБЕН упасть, и падает он на
// существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanIdentityWireDeclarations), что и
// гейт: доказательство, проверяющее вторую реализацию, доказывает вторую
// реализацию.
//
// Прогонов ЧЕТЫРЕ, и каждый снимает своё сомнение:
//
//	control    — чистый файл: находок ноль, а перепись НЕ ноль;
//	injection  — внесено второе объявление имени: находка с координатой;
//	legitimate — законные соседи: обращение к пакету-объявлению, чужой ключ той
//	             же приставки, употребление и проза — молчание;
//	binding    — привязка константы фундамента опознаётся, и по ней сверяется
//	             согласие каталога с фундаментом.
package repohygiene

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/principalwire"
)

// identityWireInjectedDefect — ВТОРОЕ объявление имени, тремя формами сразу:
// одиночная константа, элемент составного значения и приставка подсемейства.
// Третья форма стоит здесь не для полноты: на приставке ключуется вычистка
// чужого ввода, и её второе объявление стоит ровно столько же, сколько второе
// объявление ключа.
const identityWireInjectedDefect = `package restfront

const mdKeyPrincipalID = "x-kacho-principal-id"

var principalKeys = []string{
	"x-kacho-principal-type",
	"x-kaname-principal-display-name",
}

const forgeablePrefix = "x-kacho-principal-"
`

// identityWireInjectedLegitimate — то же место БЕЗ второго объявления, вместе с
// законными соседями. Каждый сосед — свой способ обмануть разбор:
//
//   - имя, взятое у пакета-объявления: объявление одно, здесь только ссылка;
//   - `x-kacho-hook-token` — законное имя ПОД той же приставкой, но вне
//     контракта личности: запрети его — и запретишь соседнюю подсистему;
//   - употребление ключа в выражении: чужой, клиентом подделываемый ключ надо
//     ПРОЧИТАТЬ, чтобы снять;
//   - проза отказа, называющая приставку: без неё отказ непонятен.
const identityWireInjectedLegitimate = `package restfront

import (
	"fmt"

	pw "github.com/PRO-Robotech/kacho/pkg/principalwire"
)

const mdKeyPrincipalID = pw.MetaPrincipalID

const hookAuthHeader = "X-Kacho-Hook-Token"

func strip(md map[string][]string) error {
	delete(md, "x-kacho-admin")
	if _, ok := md["x-kacho-principal-id"]; ok {
		return fmt.Errorf("x-kacho-principal-* прислан клиентом и снят до проверки доступа")
	}
	return nil
}
`

// identityWireInjectedBinding — файл ФУНДАМЕНТА: константы связаны с каталогом
// обращениями к пакету-объявлению, в том числе под псевдонимом. Псевдоним здесь
// не украшение: разбор по ИМЕНИ пакета обойти можно было бы одной буквой в
// объявлении импорта.
const identityWireInjectedBinding = `package grpcsrv

import pw "github.com/PRO-Robotech/kacho/pkg/principalwire"

const (
	MDKeyPrincipalType = pw.MetaPrincipalType
	MDKeyPrincipalID   = pw.MetaPrincipalID
)
`

// TestIdentityWireScannerFindsASecondDeclaration — сторона (а): внесённое
// второе объявление становится находкой, и находка несёт координату и вид.
func TestIdentityWireScannerFindsASecondDeclaration(t *testing.T) {
	decls, _, census, err := ScanIdentityWireDeclarations(
		"services/iam/internal/restfront/headermatcher_test.go", []byte(identityWireInjectedDefect))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Specs == 0 || census.Literals == 0 {
		t.Fatalf("осмотрено объявлений %d, литералов %d — разбирается не то дерево",
			census.Specs, census.Literals)
	}
	if len(decls) != 4 {
		t.Fatalf("находок %d, ожидалось 4 (константа, два элемента составного значения "+
			"и приставка): %+v", len(decls), decls)
	}
	kinds := map[principalwire.WireNameKind]int{}
	lines := map[int]bool{}
	for _, d := range decls {
		if d.File == "" || d.Line == 0 {
			t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", d)
		}
		if d.Value == "" {
			t.Errorf("находка без значения — читателю нечего искать: %+v", d)
		}
		kinds[d.Kind]++
		lines[d.Line] = true
	}
	if len(lines) != len(decls) {
		t.Errorf("находки склеились по строкам — разбор считает не узлы: %+v", decls)
	}
	if kinds[principalwire.WireNameKey] != 3 {
		t.Errorf("ключей опознано %d, ожидалось 3 (в том числе ЧУЖОГО пространства: "+
			"признак структурный, иначе переименование обошло бы гейт): %+v",
			kinds[principalwire.WireNameKey], decls)
	}
	if kinds[principalwire.WireNameFamily] != 1 {
		t.Errorf("приставок подсемейства опознано %d, ожидалась 1 — на ней ключуется "+
			"вычистка чужого ввода: %+v", kinds[principalwire.WireNameFamily], decls)
	}
}

// TestIdentityWireScannerIsSilentOnALegitimateTwin — сторона (б): законный
// близнец молчание не теряет.
func TestIdentityWireScannerIsSilentOnALegitimateTwin(t *testing.T) {
	decls, bindings, census, err := ScanIdentityWireDeclarations(
		"services/iam/internal/restfront/front.go", []byte(identityWireInjectedLegitimate))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Specs == 0 || census.Literals == 0 {
		t.Fatalf("осмотрено объявлений %d, литералов %d — молчание сказано ни о чём",
			census.Specs, census.Literals)
	}
	if len(decls) != 0 {
		t.Fatalf("законный близнец объявлен находкой (%+v) — гейт запрещал бы ссылку на "+
			"единственное объявление, соседнюю подсистему под той же приставкой, чтение "+
			"чужого ключа и прозу отказа, то есть ровно то, ради чего он заведён", decls)
	}
	if len(bindings) != 1 || bindings[0].Ident != "MetaPrincipalID" {
		t.Fatalf("ссылка на единственное объявление не опознана как привязка: %+v", bindings)
	}
}

// TestIdentityWireScannerReadsTheFundamentBinding — третий прогон: привязка
// константы фундамента к каталогу читается, и по ней сверяется согласие.
//
// Без него молчание первого утверждения было бы неотличимо от молчания мёртвого
// разбора: файл, где имя ВЗЯТО у объявления, находок не даёт по построению.
func TestIdentityWireScannerReadsTheFundamentBinding(t *testing.T) {
	decls, bindings, _, err := ScanIdentityWireDeclarations(
		"pkg/grpcsrv/principal_extract.go", []byte(identityWireInjectedBinding))
	if err != nil {
		t.Fatalf("разбор синтетики фундамента: %v", err)
	}
	if len(decls) != 0 {
		t.Fatalf("файл, берущий имена у объявления, дал находки (%+v) — гейт краснел бы "+
			"на том состоянии, к которому он ведёт", decls)
	}
	if len(bindings) != 2 {
		t.Fatalf("привязок опознано %d, ожидалось 2 — псевдоним пакета-объявления обошёл "+
			"разбор: %+v", len(bindings), bindings)
	}
	byIdent := map[string]IdentityWireBinding{}
	for _, b := range bindings {
		byIdent[b.Ident] = b
		if b.Const == "" || b.Line == 0 {
			t.Errorf("привязка без имени константы или координаты: %+v", b)
		}
	}
	for _, want := range []string{"MetaPrincipalType", "MetaPrincipalID"} {
		if _, ok := byIdent[want]; !ok {
			t.Errorf("привязка к %q не прочитана — согласие каталога с фундаментом сверять "+
				"было бы нечем: %+v", want, bindings)
		}
	}
}

// TestIdentityWireCatalogueNamesOnlyRealFundamentKeys — четвёртый прогон:
// пометка `Fundament` каталога и привязки фундамента сверяются в ОБЕ стороны на
// синтетике, где связан РОВНО ОДИН ключ.
//
// Он утверждает то, что на дереве утверждать нечем: сегодня согласие полное, и
// «сошлось» неотличимо от «не сверялось».
func TestIdentityWireCatalogueNamesOnlyRealFundamentKeys(t *testing.T) {
	_, bindings, _, err := ScanIdentityWireDeclarations(
		"pkg/grpcsrv/acr.go", []byte("package grpcsrv\n\nimport pw \"github.com/PRO-Robotech/kacho/pkg/principalwire\"\n\nconst MDKeyTokenACR = pw.MetaTokenACR\n"))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	bound := map[string]bool{}
	for _, b := range bindings {
		bound[b.Ident] = true
	}

	var missing []string
	for _, k := range principalwire.Keys() {
		if k.Fundament && !bound[k.Ident] {
			missing = append(missing, k.Name)
		}
	}
	if len(missing) == 0 {
		t.Fatalf("на синтетике, где связан ОДИН ключ, расхождения не найдено — сверка "+
			"каталога с фундаментом ничего не утверждает: %v", bound)
	}
	if bound["MetaTokenACR"] != true {
		t.Fatalf("связанный ключ не опознан — тогда «расхождение» было бы у всех сразу "+
			"и означало бы поломку разбора, а не находку: %v", bound)
	}
	t.Logf("осмотрено: записей каталога %d, связано синтетикой %d, названо несвязанными %d",
		len(principalwire.Keys()), len(bound), len(missing))
}
