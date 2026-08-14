// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protoconv_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
)

// infraTokens — инфра-чувствительные поля (security.md), которых публичная проекция
// Volume/Image/Snapshot НЕСТИ НЕ ДОЛЖНА (они живут только в internal :9091 проекции).
var infraTokens = []string{
	"backend", "lun", "nvme", "namespace", "storagenode",
	"poolid", "numericinfra", "bloblayout",
	"bucket", "enginenamespace",
}

// assertNoInfraFields — проверка по ИМЕНАМ полей дескриптора.
func assertNoInfraFields(t *testing.T, resource string, fields protoreflect.FieldDescriptors) {
	t.Helper()
	for idx := 0; idx < fields.Len(); idx++ {
		name := strings.ToLower(strings.ReplaceAll(string(fields.Get(idx).Name()), "_", ""))
		for _, tok := range infraTokens {
			require.NotContains(t, name, tok,
				"public %s must not carry infra field %q (token %q)", resource, fields.Get(idx).Name(), tok)
		}
	}
}

// infraValues — значения, которые кладутся в доменную строку и не имеют права
// появиться в публичном сообщении НИ В КАКОМ поле.
//
// Каждое взято из настоящего носителя инфра-подробности, а не выдумано: имя
// объекта у кластера данных, единица изоляции арендатора, ревизия привязки,
// имя пула. Строки нарочно узнаваемы — совпадение с ними случайным быть не может.
var infraValues = map[string]string{
	"имя объекта у бэкенда": "kacho-inst-prefix/vol-LEAKCANARY0000001",
	"пространство имён":     "proj-LEAKCANARY-namespace",
	"ревизия привязки":      "dtb-LEAKCANARY00000001",
	"желаемая ревизия":      "dtb-LEAKCANARY00000002",
}

// assertNoInfraValues — проверка по ЗНАЧЕНИЯМ, и она сильнее проверки по именам.
//
// # Зачем вторая проверка, если первая уже есть
//
// Гейт по именам перечисляет то, КАК поле названо, и потому слеп к тому, ЧТО в
// нём лежит. Этот класс здесь уже случался вживую: ярус класса ехал свободной
// строкой, и значение вида «pool-b-replicated» проходило сквозь именной гейт
// целиком — канал, закрытый по именам, оставался открыт по значениям. Поле тогда
// перевели в закрытый словарь, но сам пробел в проверке от этого не закрылся:
// закрылся ОДИН его экземпляр.
//
// Проверка по значениям слепа к именам by construction, поэтому переживает любое
// добавление поля: новое поле, куда однажды положат координату кластера, покраснеет
// здесь, даже если назовут его «location» или «hint».
// Приёмник отказа — интерфейс, а не *testing.T: только так проверку можно
// подвергнуть инъекции, не пряча настоящее падение.
type failSink interface {
	Errorf(format string, args ...any)
	FailNow()
	Helper()
	Logf(format string, args ...any)
}

func assertNoInfraValues(t failSink, resource string, m proto.Message) {
	t.Helper()
	// Текстовая форма охватывает ВСЕ поля, включая вложенные и повторяющиеся, —
	// в отличие от перечисления верхнего уровня, которое не заглядывает внутрь.
	wire := prototext.Format(m)
	for what, canary := range infraValues {
		require.NotContains(t, wire, canary,
			"публичный %s вынес наружу %s: значение %q найдено в сообщении.\n"+
				"Инфра-подробность закрывается двумя способами сразу — полем, которого нет, "+
				"и значением, которое туда не попадает. Здесь сработал второй.\n%s",
			resource, what, canary, wire)
	}
	t.Logf("проверено значений-канареек: %d; полей верхнего уровня в %s: %d",
		len(infraValues), resource, m.ProtoReflect().Descriptor().Fields().Len())
}

// TestProjectionGateHasItsSubject — предпосылка обеих проверок.
//
// Утверждение «значения не текут» держится на том, что доменная строка их
// НЕСЁТ: не станет носителя — и обе проверки останутся зелёными, ничего не
// проверив. Здесь это утверждается прямо, а не подразумевается.
func TestProjectionGateHasItsSubject(t *testing.T) {
	v := domain.Volume{}
	v.Backend.BackendObject = infraValues["имя объекта у бэкенда"]
	v.Backend.BackendNamespace = infraValues["пространство имён"]
	v.Backend.BindingID = infraValues["ревизия привязки"]
	v.Backend.DesiredBindingID = infraValues["желаемая ревизия"]

	require.NotEmpty(t, v.Backend.BackendObject, "у доменного тома не стало носителя имени объекта")
	require.NotEmpty(t, v.Backend.BackendNamespace, "у доменного тома не стало носителя пространства имён")
	require.NotEmpty(t, v.Backend.BindingID, "у доменного тома не стало носителя ревизии привязки")
	require.NotEmpty(t, v.Backend.DesiredBindingID, "у доменного тома не стало носителя желаемой ревизии")
	require.Len(t, infraValues, 4, "набор канареек изменился — перечень и предпосылка обязаны совпадать")
}

// TestValueGateGoesRedOnALeak — инъекция: проверка обязана уметь краснеть.
//
// Зелёный сам по себе не доказывает ничего: ровно так же выглядела бы проверка,
// которая не умеет падать. Здесь ей подсовывают сообщение, где координата
// действительно вынесена наружу (в поле с невинным именем — то есть именно тот
// случай, который именной гейт пропускает), и требуют красного.
func TestValueGateGoesRedOnALeak(t *testing.T) {
	leaked := protoconv.Volume(&domain.Volume{
		ID: "vol00000000000000000", ProjectID: "prj-1",
		// Имя тома — обычное публичное поле, к инфра-токенам не относится.
		// Значение в нём — координата кластера данных.
		Name:   infraValues["имя объекта у бэкенда"],
		ZoneID: "region-1-a", DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
	})
	fake := &testingT{}
	assertNoInfraValues(fake, "Volume", leaked)
	require.True(t, fake.failed,
		"проверка по значениям НЕ покраснела на вынесенной наружу координате — "+
			"значит она не проверяет то, ради чего написана")

	// Обратная сторона инъекции: законное сообщение той же формы обязано пройти.
	// Без этого «краснеет» означало бы «краснеет всегда».
	clean := protoconv.Volume(&domain.Volume{
		ID: "vol00000000000000000", ProjectID: "prj-1", Name: "data",
		ZoneID: "region-1-a", DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
	})
	ok := &testingT{}
	assertNoInfraValues(ok, "Volume", clean)
	require.False(t, ok.failed, "законное сообщение не должно ронять проверку")
}

// testingT — минимальный приёмник отказа для инъекции.
type testingT struct{ failed bool }

func (f *testingT) Errorf(string, ...any) { f.failed = true }
func (f *testingT) FailNow()              { f.failed = true }
func (f *testingT) Helper()               {}
func (f *testingT) Logf(string, ...any)   {}

// TestImagePublicProjectionNoInfra — STOR-1-25 / STOR-P-69: public Image не несёт
// инфра-полей ПО ИМЕНИ и не выносит инфра-координат ПО ЗНАЧЕНИЮ.
func TestImagePublicProjectionNoInfra(t *testing.T) {
	src := &domain.Image{
		ID: "img00000000000000000", ProjectID: "prj-1", Name: "ubuntu",
		RegionID: "ru-central1", Placement: domain.ImagePlacementRegional,
		SourceSnapshot: "snp00000000000000000", SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
		Format: domain.ImageFormatStandard, Status: domain.ImageStatusReady,
	}
	src.Backend.BackendObject = infraValues["имя объекта у бэкенда"]
	src.Backend.BackendNamespace = infraValues["пространство имён"]
	src.Backend.BindingID = infraValues["ревизия привязки"]
	src.Backend.DesiredBindingID = infraValues["желаемая ревизия"]

	i := protoconv.Image(src)
	assertNoInfraFields(t, "Image", i.ProtoReflect().Descriptor().Fields())
	assertNoInfraValues(t, "Image", i)
	require.Equal(t, "ru-central1", i.GetRegionId())
}

// TestVolumePublicProjectionNoInfra — STOR-1-16 / STOR-P-69.
func TestVolumePublicProjectionNoInfra(t *testing.T) {
	src := &domain.Volume{
		ID: "vol00000000000000000", ProjectID: "prj-1", Name: "data",
		ZoneID: "region-1-a", DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
		SourceImage: "img00000000000000000", Status: domain.VolumeStatusAvailable,
	}
	src.Backend.BackendObject = infraValues["имя объекта у бэкенда"]
	src.Backend.BackendNamespace = infraValues["пространство имён"]
	src.Backend.BindingID = infraValues["ревизия привязки"]
	src.Backend.DesiredBindingID = infraValues["желаемая ревизия"]

	v := protoconv.Volume(src)
	assertNoInfraFields(t, "Volume", v.ProtoReflect().Descriptor().Fields())
	assertNoInfraValues(t, "Volume", v)
	require.Equal(t, "img00000000000000000", v.GetSourceImageId())
}

// TestSnapshotPublicProjectionNoInfra — снимок нёс те же координаты и той же
// проверки не имел вовсе. Третий ресурс двухпроекционности проверяется наравне
// с двумя другими: «покрыто» не должно читаться шире, чем есть.
func TestSnapshotPublicProjectionNoInfra(t *testing.T) {
	src := &domain.Snapshot{
		ID: "snp00000000000000000", ProjectID: "prj-1", Name: "nightly",
		ZoneID: "region-1-a", SizeBytes: 1 << 30, Status: domain.SnapshotStatusReady,
	}
	src.Backend.BackendObject = infraValues["имя объекта у бэкенда"]
	src.Backend.BackendNamespace = infraValues["пространство имён"]
	src.Backend.BindingID = infraValues["ревизия привязки"]
	src.Backend.DesiredBindingID = infraValues["желаемая ревизия"]

	s := protoconv.Snapshot(src)
	assertNoInfraFields(t, "Snapshot", s.ProtoReflect().Descriptor().Fields())
	assertNoInfraValues(t, "Snapshot", s)
}
