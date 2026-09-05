// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package catalogderive_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
)

// TestDeriveBuildsTheEdgeCheckingEntry — обычная строка: отношение, тип объекта и
// идентификатор, взятый из названного аннотацией поля запроса.
func TestDeriveBuildsTheEdgeCheckingEntry(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	e, ok := m["/kacho.cloud.storage.v1.VolumeService/Get"]
	require.True(t, ok, "метод домена обязан попасть в выведенную карту")
	assert.Equal(t, "v_get", e.Relation)
	assert.Equal(t, "storage.volumes.get", e.Permission)
	assert.False(t, e.Public)
	assert.False(t, e.ScopeFiltered)

	require.NotNil(t, e.Extract)
	ot, id, xerr := e.Extract(&storagev1.GetVolumeRequest{VolumeId: "vol-1"})
	require.NoError(t, xerr)
	assert.Equal(t, "storage_volume", ot)
	assert.Equal(t, "vol-1", id, "идентификатор берётся из поля, названного аннотацией")
}

// TestDeriveReadsTheParentScopeField — Create якорится на родителе, и поле другое.
func TestDeriveReadsTheParentScopeField(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	e := m["/kacho.cloud.storage.v1.VolumeService/Create"]
	assert.Equal(t, "editor", e.Relation)
	require.NotNil(t, e.Extract)
	ot, id, xerr := e.Extract(&storagev1.CreateVolumeRequest{ProjectId: "prj-7"})
	require.NoError(t, xerr)
	assert.Equal(t, "project", ot)
	assert.Equal(t, "prj-7", id)
}

// TestDeriveSubstitutesTheClusterSingleton — якорь на кластере адресуется
// синглтоном, ровно как это делает край; иначе Check уходит на `cluster:*`,
// который отвергается как unscoped.
func TestDeriveSubstitutesTheClusterSingleton(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	// Пример берётся с АДМИНСКОГО глагола каталога, а не с чтения: чтение
	// каталога типов дисков — project-scope EXEMPT (#892), у него якоря нет
	// by construction. Прежняя редакция стояла на `List`, и когда его полосу
	// исправили, проба покраснела не на своём предмете: она утверждает про
	// подстановку синглтона, а не про то, какие RPC кластерные.
	e := m["/kacho.cloud.storage.v1.InternalDiskTypeService/Create"]
	require.NotNil(t, e.Extract)
	ot, id, xerr := e.Extract(&storagev1.CreateDiskTypeRequest{})
	require.NoError(t, xerr)
	assert.Equal(t, "cluster", ot)
	assert.Equal(t, "cluster_kacho_root", id)
}

// TestDeriveCarriesTheScopeFilteredLane — полоса «сужает владелец» переносится
// как ScopeFiltered, а НЕ как Public: разница в том, требуется ли субъект.
func TestDeriveCarriesTheScopeFilteredLane(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	e := m["/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments"]
	assert.True(t, e.ScopeFiltered, "строка scope_filtered обязана требовать субъекта")
	assert.False(t, e.Public)
	assert.Empty(t, e.Relation)
	assert.Nil(t, e.Extract)
}

// TestDeriveCarriesTheExemptLane — `<exempt>` снимает per-RPC Check целиком.
func TestDeriveCarriesTheExemptLane(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.operation")
	require.NoError(t, err)

	e, ok := m["/kacho.cloud.operation.OperationService/Get"]
	require.True(t, ok)
	assert.True(t, e.Public)
	assert.False(t, e.ScopeFiltered)
	assert.Empty(t, e.Relation)
	assert.Nil(t, e.Extract)
}

// TestDeriveCarriesHideExistence — форма отказа переносится с той же строки.
func TestDeriveCarriesHideExistence(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.registry.v1")
	require.NoError(t, err)

	var hiding int
	for _, e := range m {
		if e.HideExistence {
			hiding++
		}
	}
	assert.NotZero(t, hiding,
		"ни одна выведенная запись не скрывает существование — либо аннотация "+
			"перестала читаться, либо полоса снята с домена")
}

// TestDeriveRefusesAnUnlinkedPackage — предпосылка вывода: названный пакет обязан
// быть в бинаре. Молчаливая пустая карта означала бы, что каждый RPC сервиса
// отвечает fail-closed, и узналось бы это первым запросом.
func TestDeriveRefusesAnUnlinkedPackage(t *testing.T) {
	_, err := catalogderive.Derive("kacho.cloud.storage.v1", "kacho.cloud.nosuch.v1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kacho.cloud.nosuch.v1")
}

// TestDeriveRefusesAnEmptyPackageList — вывод без единого пакета даёт пустую
// карту, а пустая карта — отказ на каждом RPC.
func TestDeriveRefusesAnEmptyPackageList(t *testing.T) {
	_, err := catalogderive.Derive()
	require.Error(t, err)
}

// TestExtractorRejectsAForeignRequest — экстрактор привязан к типу запроса
// СВОЕГО метода. Чужое сообщение не «даёт пустой id», оно отвергается: пустой id
// уехал бы в Check как `type:` и получил бы отказ, неотличимый от отказа по
// правам.
func TestExtractorRejectsAForeignRequest(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	e := m["/kacho.cloud.storage.v1.VolumeService/Get"]
	require.NotNil(t, e.Extract)
	_, _, xerr := e.Extract(&storagev1.CreateVolumeRequest{ProjectId: "prj-1"})
	require.Error(t, xerr)
}

// TestDeriveIsTotalOverThePackage — выведенная карта покрывает КАЖДЫЙ метод
// названного пакета. Метод без записи отвечает fail-closed, и молчаливый пропуск
// был бы отказом, о котором никто не объявлял.
func TestDeriveIsTotalOverThePackage(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	assert.Equal(t, catalogderive.MethodCount("kacho.cloud.storage.v1"), len(m),
		"в карте столько же записей, сколько методов у пакета")
	for k := range m {
		require.True(t, strings.HasPrefix(k, "/kacho.cloud.storage.v1."),
			"в карту попал метод чужого пакета: %s", k)
	}
}

// TestEmptyScopeIdIsRefusedNotAsked — вызов, не назвавший область, отвергается,
// а не спрашивается с пустым идентификатором.
//
// Проба существует ради разобранного расхождения vpc `AddressService/GetByValue`
// (единственный RPC, чья область берётся из необязательного поля): рукописный
// экстрактор возвращал ошибку на пустом поле, выведенный возвращает пустой id.
// Утверждать, что «исход тот же», по прочтению кода — недостаточно; здесь он
// проверен: пустой id не образует объекта, а значит вопрос не задаётся и вызов
// отвергается.
func TestEmptyScopeIdIsRefusedNotAsked(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	e := m["/kacho.cloud.storage.v1.VolumeService/Get"]
	require.NotNil(t, e.Extract)

	ot, id, xerr := e.Extract(&storagev1.GetVolumeRequest{}) // поле не заполнено
	require.NoError(t, xerr)
	require.Equal(t, "storage_volume", ot)
	require.Empty(t, id)

	_, ferr := authz.FormatObject(ot, id)
	require.Error(t, ferr, "пустой идентификатор не образует объекта — интерсептор отказывает")
}

// TestDeriveIsCompatibleWithTheInterceptorLookup — ключ выведенной карты обязан
// совпадать с тем, чем grpc-go зовёт метод.
func TestDeriveIsCompatibleWithTheInterceptorLookup(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.storage.v1")
	require.NoError(t, err)

	var rm authz.RPCMap = m
	_, ok := rm.Lookup("/kacho.cloud.storage.v1.VolumeService/Delete")
	assert.True(t, ok)
}

// TestDeriveResolvesADottedScopePath — область, лежащая внутри вложенного тела
// запроса, читается по составному пути. Единственный такой сайт сегодня —
// `InternalAddressService.CreateOwnedAddress`, где внутренний путь намеренно
// переиспользует ЦЕЛИКОМ тело публичного создания, поэтому проект запроса лежит
// на уровень глубже.
func TestDeriveResolvesADottedScopePath(t *testing.T) {
	m, err := catalogderive.Derive("kacho.cloud.vpc.v1")
	require.NoError(t, err)

	e, ok := m["/kacho.cloud.vpc.v1.InternalAddressService/CreateOwnedAddress"]
	require.True(t, ok)
	assert.Equal(t, "editor", e.Relation)
	require.NotNil(t, e.Extract)

	ot, id, xerr := e.Extract(&vpcv1.CreateOwnedAddressRequest{
		Address: &vpcv1.CreateAddressRequest{ProjectId: "prj-42"},
	})
	require.NoError(t, xerr)
	assert.Equal(t, "project", ot)
	assert.Equal(t, "prj-42", id)

	// Пустое вложенное сообщение не «подставляет» ничего: id пуст, объект не
	// образуется, вызов отвергается.
	_, id2, xerr2 := e.Extract(&vpcv1.CreateOwnedAddressRequest{})
	require.NoError(t, xerr2)
	assert.Empty(t, id2)
}
