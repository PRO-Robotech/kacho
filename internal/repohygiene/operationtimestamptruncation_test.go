// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOperationTimestampsAreTruncatedEverywhere — свойство дерева, а не двух
// исправленных файлов.
func TestOperationTimestampsAreTruncatedEverywhere(t *testing.T) {
	rep, err := auditOperationTimestampTruncation(repoRoot(t))
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	t.Logf("перепись: не-тестовых файлов прочитано %d; сборок Operation %d; "+
		"полей времени рассмотрено %d (из них засчитано через посредника %d); находок %d",
		rep.FilesRead, rep.Constructors, rep.Fields, rep.ViaHelper, len(rep.Findings))

	if rep.FilesRead == 0 {
		t.Fatal("не прочитано ни одного файла — «усечено везде» здесь означало бы " +
			"«ничего не читал»")
	}
	if rep.Constructors == 0 {
		t.Fatal("сборок operationpb.Operation{…} не найдено ни одной — предмет гейта исчез " +
			"из дерева либо перестал опознаваться; «ноль находок» в таком дереве ничего " +
			"не утверждает")
	}
	if rep.Fields == 0 {
		t.Fatal("ни одна сборка не заполняет CreatedAt/ModifiedAt — имена полей сменились, " +
			"и гейт молча проверяет пустоту")
	}

	for _, f := range rep.Findings {
		t.Errorf("%s: %s = %s — %s.\n"+
			"Конвенция продукта требует усечения меток времени до секунды в КАЖДОМ "+
			"proto-ответе: БД хранит микросекунды, клиент их не видит. Operation — ответ "+
			"каждой мутации, то есть самая частая поверхность утечки. Пишите "+
			"`timestamppb.New(x.Truncate(time.Second))` либо зовите посредника, который "+
			"усекает сам", f.Where, f.Field, f.Expr, f.Why)
	}
}

// --- инъекция: обе стороны гоняют ТУ ЖЕ функцию, что и гейт по дереву ---

func synthTruncationTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range truncationScanRoots {
		seed := filepath.Join(root, sub, "seed.go")
		if err := os.MkdirAll(filepath.Dir(seed), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		if err := os.WriteFile(seed, []byte("package seed\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", sub, err)
		}
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

const truncSrcBare = `package m

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

var _ = time.Second

func toProto(op struct{ CreatedAt, ModifiedAt time.Time }) *operationpb.Operation {
	return &operationpb.Operation{
		CreatedAt:  timestamppb.New(op.CreatedAt),
		ModifiedAt: timestamppb.New(op.ModifiedAt),
	}
}
`

const truncSrcDirect = `package m

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

func toProto(op struct{ CreatedAt, ModifiedAt time.Time }) *operationpb.Operation {
	return &operationpb.Operation{
		CreatedAt:  timestamppb.New(op.CreatedAt.Truncate(time.Second)),
		ModifiedAt: timestamppb.New(op.ModifiedAt.Truncate(time.Second)),
	}
}
`

const truncSrcViaHelper = `package m

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t.Truncate(time.Second)) }

func toProto(op struct{ CreatedAt, ModifiedAt time.Time }) *operationpb.Operation {
	return &operationpb.Operation{
		CreatedAt:  ts(op.CreatedAt),
		ModifiedAt: ts(op.ModifiedAt),
	}
}
`

// Посредник, который НЕ усекает, — тот самый случай, ради которого гейт идёт за
// вызов, а не останавливается на «там есть вызов, наверное усекает».
const truncSrcHollowHelper = `package m

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func toProto(op struct{ CreatedAt, ModifiedAt time.Time }) *operationpb.Operation {
	return &operationpb.Operation{
		CreatedAt:  ts(op.CreatedAt),
		ModifiedAt: ts(op.ModifiedAt),
	}
}
`

// Сторона дефекта: неусечённое значение роняет гейт И называет координату.
func TestTruncationGateRedOnBareTimestamp(t *testing.T) {
	root := synthTruncationTree(t, map[string]string{"services/x/mapping.go": truncSrcBare})
	rep, err := auditOperationTimestampTruncation(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("ожидались две находки (created+modified), получено %d: %+v",
			len(rep.Findings), rep.Findings)
	}
	for _, f := range rep.Findings {
		if !strings.Contains(f.Where, "services/x/mapping.go") {
			t.Errorf("находка без координаты файла: %+v", f)
		}
	}
}

// Законный близнец I: прямое усечение — гейт молчит.
func TestTruncationGateSilentOnDirectTruncation(t *testing.T) {
	root := synthTruncationTree(t, map[string]string{"services/x/mapping.go": truncSrcDirect})
	rep, err := auditOperationTimestampTruncation(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("гейт нашёл дефект в законном дереве: %+v", rep.Findings)
	}
	if rep.Fields != 2 {
		t.Fatalf("осмотрено полей %d, ожидалось 2 — молчание относится не к тому, "+
			"что проверяли", rep.Fields)
	}
}

// Законный близнец II: усечение ЧЕРЕЗ ПОСРЕДНИКА — гейт молчит.
//
// Без этой половины гейт ловил бы форму записи, а не свойство: compute и iam
// усекают именно так, и первый же прогон покрасил бы два исправных сервиса.
func TestTruncationGateSilentOnHelperThatTruncates(t *testing.T) {
	root := synthTruncationTree(t, map[string]string{"services/x/mapping.go": truncSrcViaHelper})
	rep, err := auditOperationTimestampTruncation(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("усечение через посредника не засчитано: %+v", rep.Findings)
	}
	if rep.ViaHelper != 2 {
		t.Fatalf("через посредника засчитано %d полей, ожидалось 2 — молчание получено "+
			"не тем путём, которым заявлено", rep.ViaHelper)
	}
}

// Посредник-пустышка: вызов есть, усечения нет — находка.
func TestTruncationGateRedOnHollowHelper(t *testing.T) {
	root := synthTruncationTree(t, map[string]string{"services/x/mapping.go": truncSrcHollowHelper})
	rep, err := auditOperationTimestampTruncation(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("посредник без усечения принят за усечение: %+v", rep.Findings)
	}
	for _, f := range rep.Findings {
		if !strings.Contains(f.Why, "НЕ усекает") {
			t.Errorf("находка не называет причину: %+v", f)
		}
	}
}
