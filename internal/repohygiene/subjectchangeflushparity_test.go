// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
)

// methodExistsInContract — разрешима ли пара «служба/метод» в реестре контрактов.
// Реестр наполняется импортом сгенерированного пакета выше: имя судится по
// ДЕСКРИПТОРУ, а не по совпадению строк.
func methodExistsInContract(service, method string) bool {
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return false
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return false
	}
	return sd.Methods().ByName(protoreflect.Name(method)) != nil
}

func subjectChangeFlushParityOptions(t *testing.T) SubjectChangeFlushParityOptions {
	t.Helper()
	return SubjectChangeFlushParityOptions{
		Root:          repoRoot(t),
		ProducerRoot:  "services/iam/internal/apps/kaname/api",
		SelfFlushFile: "gateway/internal/middleware/authz.go",
		MethodExists:  methodExistsInContract,
	}
}

// TestSelfFlushCoversEveryProducerOfTheSubjectChangeQueue — вердикт о НАСТОЯЩЕМ
// дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`subjectchangeflushparity_injection_test.go`).
func TestSelfFlushCoversEveryProducerOfTheSubjectChangeQueue(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditSubjectChangeFlushParity(subjectChangeFlushParityOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: обе полосы непусты. Ноль производителей означал бы, что очередь
	// не пишется вовсе, и совпадение чисел было бы совпадением пустот.
	if census.GoFiles < 100 || len(census.Producers) == 0 || len(census.SelfFlushSet) == 0 {
		t.Fatalf("файлов %d, производителей %d, самосброс %d — обход пуст, вердикт беспредметен",
			census.GoFiles, len(census.Producers), len(census.SelfFlushSet))
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  " + f.What)
	}
	t.Fatalf("полосы одного механизма разошлись — находок %d:%s", len(findings), b.String())
}
