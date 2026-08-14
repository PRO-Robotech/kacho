// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Полоса ссылки NIC → Address: что видит вызывающий, назвавший чужой,
// отсутствующий или негодный идентификатор адреса.
//
// Предмет проб — ЧТО УТВЕРЖДАЕТ ОТКАЗ, а не факт отказа. Ссылка приходит массивом
// от вызывающего, адрес — собственный ресурс vpc (own-owned, полоса direct-read),
// поэтому:
//
//   - идентификатор чужого проекта и отсутствующий идентификатор обязаны отвечать
//     ОДНИМ сообщением, побайтово равным настоящему промаху владельца
//     (`NotFound "Address <id> not found"`, тон контракта; форму пишет
//     `internal/repo/kacho/pg/address.go`). Различие текстов здесь — оракул
//     существования: по нему устанавливают, что чужой адрес есть, какого он
//     семейства, в какой подсети лежит и занят ли он;
//   - у адреса СВОЕГО проекта различие законно и полезно: это диагностика по
//     собственному объекту, а не сведения о чужом. Поэтому пробы ниже требуют
//     обе стороны — и схлопывание чужого, и сохранение своего. Отрицание без
//     положительного зеленело бы на функции, которая всегда отвечает промахом.
//
// Порядок проверок утверждается входами, на которых более поздняя проверка дала
// бы ДРУГОЙ текст: мусорный идентификатор, при этом ЛЕЖАЩИЙ в хранилище (иначе
// «формат раньше существования» неотличимо от «оба отвечают одинаково»), и
// чужой адрес с негодным состоянием (иначе «проект раньше состояния»
// неотличимо от порядка наоборот).

const (
	// nicRefProject / nicRefForeign — проект интерфейса и чужой проект.
	nicRefProject = "f1"
	nicRefForeign = "other"
	// nicRefSubnet — подсеть интерфейса; nicRefOtherSubnet — подсеть, которую
	// сообщение НЕ вправе называть, когда адрес чужой.
	nicRefSubnet      = "e9bsub1"
	nicRefOtherSubnet = "e9bsub-foreign-777"
	// nicRefAddrID — ОДИН идентификатор на все схлопнутые исходы: побайтовое
	// равенство сообщений проверяется на одном id, иначе сравнение сводилось бы
	// к сравнению шаблонов, а не текстов.
	nicRefAddrID = "adr00000000000000001"
)

// nicRefMiss — канонический промах владельца адреса, выписанный здесь по
// `internal/repo/kacho/pg/address.go`, а не считанный из проверяемого кода:
// ожидание, скопированное у предмета проверки, не утверждает ничего.
func nicRefMiss(id string) string { return fmt.Sprintf("Address %s not found", id) }

// nicRefStand — стенд под одну пробу: подсеть интерфейса в проекте вызывающего
// плюс произвольные заранее посеянные адреса.
func nicRefStand(t *testing.T, seed ...*kachorepo.AddressRecord) (*kachomock.Repository, *repomock.OpsRepo) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	kr.SeedSubnet(&kachorepo.SubnetRecord{
		Subnet: domain.Subnet{ID: nicRefSubnet, ProjectID: nicRefProject, Name: domain.RcNameVPC("sn")},
	})
	for _, a := range seed {
		kr.SeedAddress(a)
	}
	return kr, or
}

// nicRefInternalV4 — заранее зарезервированный внутренний IPv4-адрес.
func nicRefInternalV4(id, project, subnet string, used bool) *kachorepo.AddressRecord {
	return &kachorepo.AddressRecord{Address: domain.Address{
		ID: id, ProjectID: project, Type: domain.AddressTypeInternal,
		IpVersion: domain.IpVersionIPv4, Used: used,
		InternalIpv4: &domain.InternalIpv4Spec{SubnetID: subnet, Address: "10.0.0.5"},
	}}
}

// nicRefExternalV4 — внешний адрес (в подсети не лежит вовсе).
func nicRefExternalV4(id, project string) *kachorepo.AddressRecord {
	return &kachorepo.AddressRecord{Address: domain.Address{
		ID: id, ProjectID: project, Type: domain.AddressTypeExternal,
		IpVersion: domain.IpVersionIPv4, Used: false,
		ExternalIpv4: &domain.ExternalIpv4Spec{Address: "203.0.113.7", ZoneID: "zone-a"},
	}}
}

// nicRefCreate — создание интерфейса со ссылкой на названный v4-адрес; возвращает
// завершённую Operation.
func nicRefCreate(t *testing.T, kr *kachomock.Repository, or *repomock.OpsRepo, v4IDs []string) *operationResult {
	t.Helper()
	uc := NewCreateNetworkInterfaceUseCase(kr, &repomock.ProjectClient{OK: true}, or)
	op, err := uc.Execute(context.Background(), CreateInput{NetworkInterface: domain.NetworkInterface{
		ProjectID:    nicRefProject,
		Name:         "nic",
		SubnetID:     nicRefSubnet,
		V4AddressIDs: v4IDs,
	}})
	if err != nil {
		return &operationResult{syncErr: err}
	}
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	return &operationResult{opErr: saved.Error}
}

// operationResult — исход создания, каким его видит вызывающий: либо синхронный
// отказ, либо ошибка Operation. Обе формы несут код и сообщение, поэтому
// сравниваются одинаково.
type operationResult struct {
	syncErr error
	opErr   *rpcstatus.Status
}

func (r *operationResult) code() codes.Code {
	if r.syncErr != nil {
		st, _ := status.FromError(r.syncErr)
		return st.Code()
	}
	if r.opErr == nil {
		return codes.OK
	}
	return codes.Code(r.opErr.Code)
}

func (r *operationResult) message() string {
	if r.syncErr != nil {
		st, _ := status.FromError(r.syncErr)
		return st.Message()
	}
	if r.opErr == nil {
		return ""
	}
	return r.opErr.Message
}

// TestNICAddressRef_ForeignAndAbsentAreOneMessage — схлопывание: чужой адрес в
// любом состоянии и отсутствующий адрес отвечают ОДНИМ сообщением, побайтово
// равным настоящему промаху владельца. Все исходы названы на ОДНОМ id, поэтому
// сравниваются именно тексты.
func TestNICAddressRef_ForeignAndAbsentAreOneMessage(t *testing.T) {
	cases := []struct {
		name string
		seed *kachorepo.AddressRecord
	}{
		{"absent", nil},
		{"foreign_project_otherwise_valid", nicRefInternalV4(nicRefAddrID, nicRefForeign, nicRefSubnet, false)},
		{"foreign_project_other_subnet", nicRefInternalV4(nicRefAddrID, nicRefForeign, nicRefOtherSubnet, false)},
		{"foreign_project_not_internal_v4", nicRefExternalV4(nicRefAddrID, nicRefForeign)},
		{"foreign_project_already_used", nicRefInternalV4(nicRefAddrID, nicRefForeign, nicRefSubnet, true)},
	}

	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seed []*kachorepo.AddressRecord
			if tc.seed != nil {
				seed = append(seed, tc.seed)
			}
			kr, or := nicRefStand(t, seed...)
			got := nicRefCreate(t, kr, or, []string{nicRefAddrID})

			assert.Equal(t, codes.NotFound, got.code(),
				"чужой/отсутствующий адрес — полоса direct-read владельца: NotFound")
			assert.Equal(t, nicRefMiss(nicRefAddrID), got.message(),
				"сообщение обязано быть побайтово равно настоящему промаху владельца")
			assert.NotContains(t, got.message(), nicRefOtherSubnet,
				"подсеть чужого адреса не может попасть в текст отказа")
			assert.Empty(t, kr.NetworkInterfaces(), "интерфейс не создаётся")

			seen[tc.name] = got.message()
		})
	}

	// Побайтовое равенство между исходами — отдельное утверждение: без него
	// каждый кейс проверял бы лишь свой шаблон.
	var first string
	for name, msg := range seen {
		if first == "" {
			first = msg
			continue
		}
		assert.Equal(t, first, msg, "исход %q обязан быть неотличим от остальных", name)
	}
}

// TestNICAddressRef_MissMatchesTheOwnersMapperOutput — байт-идентичность
// утверждается против ВЫХОДА общего mapper'а на продуктовой форме ошибки владельца,
// а не только против литерала в этом файле. Так проба ловит два разных сдвига: смену
// тона у владельца адреса и смену политики `stripSentinel` в общем mapper'е —
// каждый из них сделал бы отказ отличимым от промаха, оставив литеральное сравнение
// зелёным.
//
// Продуктовую форму (`"%w: Address %s not found"` под `ErrNotFound`) приходится
// собирать здесь руками: `kachomock` отдаёт ГОЛЫЙ sentinel, то есть его собственный
// промах беднее продуктового. Сверять отказ с промахом фикстуры значило бы требовать
// от прод-кода этой бедности.
func TestNICAddressRef_MissMatchesTheOwnersMapperOutput(t *testing.T) {
	wrapped := fmt.Errorf("%w: Address %s not found", repo.ErrNotFound, nicRefAddrID)
	st, ok := status.FromError(serviceerr.MapRepoErr(wrapped))
	require.True(t, ok, "MapRepoErr обязан вернуть gRPC-status")
	require.Equal(t, codes.NotFound, st.Code(), "промах владельца — NotFound")

	kr, or := nicRefStand(t, nicRefInternalV4(nicRefAddrID, nicRefForeign, nicRefSubnet, false))
	got := nicRefCreate(t, kr, or, []string{nicRefAddrID})

	assert.Equal(t, st.Code(), got.code(), "код отказа обязан совпасть с кодом промаха")
	assert.Equal(t, st.Message(), got.message(), "текст отказа обязан совпасть с текстом промаха")
}

// TestNICAddressRef_FormatPrecedesEverything — формат идентификатора проверяется
// ПЕРВЫМ. Вход выбран так, что любая более поздняя проверка дала бы ДРУГОЙ текст:
// мусорный id ЛЕЖИТ в хранилище и годен по состоянию, поэтому «существование
// раньше формата» ответило бы успехом, а «проект раньше формата» — промахом.
func TestNICAddressRef_FormatPrecedesEverything(t *testing.T) {
	const garbage = "zzz"

	t.Run("garbage_id_present_in_store", func(t *testing.T) {
		kr, or := nicRefStand(t, nicRefInternalV4(garbage, nicRefProject, nicRefSubnet, false))
		got := nicRefCreate(t, kr, or, []string{garbage})

		assert.Equal(t, codes.InvalidArgument, got.code(),
			"мусорный идентификатор — отказ формата, а не утверждение об отсутствии ресурса")
		assert.Equal(t, fmt.Sprintf("invalid address id '%s'", garbage), got.message())
		assert.NotEqual(t, nicRefMiss(garbage), got.message(),
			"мусор не вправе получать контракт-тон отсутствия объекта")
		assert.NotNil(t, got.syncErr,
			"мусор не оплачивается ни Operation, ни чтением: отказ синхронный")
		assert.Empty(t, kr.NetworkInterfaces())
	})

	t.Run("garbage_id_absent", func(t *testing.T) {
		kr, or := nicRefStand(t)
		got := nicRefCreate(t, kr, or, []string{garbage})
		assert.Equal(t, codes.InvalidArgument, got.code())
		assert.Equal(t, fmt.Sprintf("invalid address id '%s'", garbage), got.message())
		assert.NotNil(t, got.syncErr, "отказ синхронный")
	})

	// Правка интерфейса отвергает форму так же синхронно: у Update своя точка входа,
	// и проверка, поставленная только у Create, второго вызывающего не защищает.
	t.Run("update_path_refuses_garbage_synchronously", func(t *testing.T) {
		kr, or := nicRefStand(t)
		nicID := "nic00000000000000002"
		preloadNIC(t, kr, &kachorepo.NetworkInterfaceRecord{
			NetworkInterface: domain.NetworkInterface{
				ID: nicID, ProjectID: nicRefProject, Name: domain.RcNameVPC("nic"),
				SubnetID: nicRefSubnet, Status: domain.NIStatusAvailable,
			},
		})

		uc := NewUpdateNetworkInterfaceUseCase(kr, or)
		_, err := uc.Execute(context.Background(), UpdateInput{
			NetworkInterfaceID: nicID,
			NetworkInterface:   domain.NetworkInterface{V4AddressIDs: []string{garbage}},
			UpdateMask:         []string{"v4_address_ids"},
		})
		require.Error(t, err, "мусорная ссылка обязана быть отвергнута синхронно")
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, fmt.Sprintf("invalid address id '%s'", garbage), st.Message())
	})
}

// TestNICAddressRef_EmptyIDNamesTheField — пустая строка в массиве ссылок.
// `corevalidate.ResourceID` пустой вход ПРОПУСКАЕТ (это её контракт), поэтому
// обязательность — ответственность вызывающего. Без неё пустая строка уезжает в
// чтение и возвращается контракт-тоном промаха с вырезанным id — утверждение об
// отсутствии ресурса, которого вызывающий не называл.
func TestNICAddressRef_EmptyIDNamesTheField(t *testing.T) {
	kr, or := nicRefStand(t)
	got := nicRefCreate(t, kr, or, []string{""})

	assert.Equal(t, codes.InvalidArgument, got.code())
	assert.NotContains(t, got.message(), "not found",
		"пустой вход не может отвечать тоном отсутствия ресурса")

	require.NotNil(t, got.syncErr, "отказ обязан быть синхронным — величина от вызывающего")
	st, _ := status.FromError(got.syncErr)
	assert.Contains(t, fieldsNamedBy(st), "v4_address_ids",
		"отказ обязан назвать поле запроса, куда пришло значение")
}

// TestNICAddressRef_ReadingFunctionGuardsItsOwnInput — проверка формы стоит и В ТОЙ
// ЖЕ функции, которая читает адрес, а не только у синхронного вызывающего.
//
// # Что эта проба утверждает, а что — нет
//
// Она зовёт `validateNICAddressRef` НАПРЯМУЮ, без вызывающего, поэтому о ПОРЯДКЕ и о
// синхронности не говорит ничего — это утверждают пробы уровня use-case выше
// (`TestNICAddressRef_FormatPrecedesEverything`, `..._EmptyIDNamesTheField`). Её
// предмет другой: что читающая функция отвергает негодный вход САМА, а значит
// защищает и того вызывающего, которого напишут после нас и который синхронную
// ветку не позовёт. Без этой пробы у ветки формы внутри функции не было бы
// предмета — и её сняли бы как дублирующую.
func TestNICAddressRef_ReadingFunctionGuardsItsOwnInput(t *testing.T) {
	kr, _ := nicRefStand(t, nicRefInternalV4(nicRefAddrID, nicRefProject, nicRefSubnet, false))
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	ar := w.Addresses()

	t.Run("garbage_refused", func(t *testing.T) {
		err := validateNICAddressRef(context.Background(), ar, "zzz", nicRefProject, nicRefSubnet, domain.IpVersionIPv4)
		require.Error(t, err)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "invalid address id 'zzz'", st.Message())
	})

	t.Run("empty_refused_naming_the_field", func(t *testing.T) {
		err := validateNICAddressRef(context.Background(), ar, "", nicRefProject, nicRefSubnet, domain.IpVersionIPv6)
		require.Error(t, err)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.NotContains(t, st.Message(), "not found")
		assert.Contains(t, fieldsNamedBy(st), "v6_address_ids",
			"на v6-входе отказ обязан называть v6-поле, а не v4")
	})

	// Положительный контроль: законная ссылка проходит ту же функцию. Без него оба
	// отрицания зеленели бы на функции, отвергающей всё.
	t.Run("legal_ref_passes", func(t *testing.T) {
		require.NoError(t, validateNICAddressRef(context.Background(), ar, nicRefAddrID,
			nicRefProject, nicRefSubnet, domain.IpVersionIPv4))
	})
}

// fieldsNamedBy — имена полей из BadRequest-details отказа.
func fieldsNamedBy(st *status.Status) []string {
	var out []string
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, fv := range br.FieldViolations {
			out = append(out, fv.Field)
		}
	}
	return out
}

// TestNICAddressRef_OwnProjectStateStillDiagnosed — вторая сторона схлопывания:
// у адреса СВОЕГО проекта исходы по-прежнему различимы. Без этих проб утверждения
// выше зеленели бы на функции, которая на любой вход отвечает промахом.
func TestNICAddressRef_OwnProjectStateStillDiagnosed(t *testing.T) {
	cases := []struct {
		name     string
		seed     *kachorepo.AddressRecord
		wantCode codes.Code
		wantMsg  string
	}{
		{
			"not_internal_v4",
			nicRefExternalV4(nicRefAddrID, nicRefProject),
			codes.InvalidArgument,
			fmt.Sprintf("address %s is not an internal IPv4 address", nicRefAddrID),
		},
		{
			"other_subnet_of_same_project",
			nicRefInternalV4(nicRefAddrID, nicRefProject, nicRefOtherSubnet, false),
			codes.InvalidArgument,
			fmt.Sprintf("address %s belongs to subnet %s, not %s", nicRefAddrID, nicRefOtherSubnet, nicRefSubnet),
		},
		{
			"already_in_use",
			nicRefInternalV4(nicRefAddrID, nicRefProject, nicRefSubnet, true),
			codes.FailedPrecondition,
			fmt.Sprintf("address %s is already in use", nicRefAddrID),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kr, or := nicRefStand(t, tc.seed)
			got := nicRefCreate(t, kr, or, []string{nicRefAddrID})

			assert.Equal(t, tc.wantCode, got.code(),
				"собственный адрес: диагностика состояния законна")
			assert.Equal(t, tc.wantMsg, got.message())
			assert.NotEqual(t, nicRefMiss(nicRefAddrID), got.message(),
				"свой объект не прячется за промахом — иначе владелец не узнает причину")
			assert.Empty(t, kr.NetworkInterfaces())
		})
	}
}

// TestNICAddressRef_LegalRefPasses — положительный контроль: законная ссылка
// проходит и адрес помечается занятым. Без него всякое отрицание выше зеленело
// бы на функции, отвергающей всё.
func TestNICAddressRef_LegalRefPasses(t *testing.T) {
	kr, or := nicRefStand(t, nicRefInternalV4(nicRefAddrID, nicRefProject, nicRefSubnet, false))
	got := nicRefCreate(t, kr, or, []string{nicRefAddrID})

	require.Nil(t, got.syncErr, "законная ссылка не отвергается синхронно")
	require.Nil(t, got.opErr, "законная ссылка обязана пройти: %v", got.message())
	require.Len(t, kr.NetworkInterfaces(), 1, "интерфейс создан")

	rd, err := kr.Reader(context.Background())
	require.NoError(t, err)
	a, err := rd.Addresses().Get(context.Background(), nicRefAddrID)
	_ = rd.Close()
	require.NoError(t, err)
	assert.True(t, a.Used, "приаттаченный адрес помечен занятым")
}

// TestNICAddressRef_UpdatePathSharesTheLane — правка интерфейса ходит той же
// полосой: чужой адрес, добавленный через Update, отвергается тем же
// сообщением. Иначе фикс закрывал бы один вызывающий из двух — привязка
// валидируется общей функцией, но проверка, поставленная у одного вызывающего,
// второго не защищает.
func TestNICAddressRef_UpdatePathSharesTheLane(t *testing.T) {
	kr, or := nicRefStand(t, nicRefInternalV4(nicRefAddrID, nicRefForeign, nicRefSubnet, false))
	nicID := "nic00000000000000001"
	preloadNIC(t, kr, &kachorepo.NetworkInterfaceRecord{
		NetworkInterface: domain.NetworkInterface{
			ID:        nicID,
			ProjectID: nicRefProject,
			Name:      domain.RcNameVPC("nic"),
			SubnetID:  nicRefSubnet,
			Status:    domain.NIStatusAvailable,
		},
	})

	uc := NewUpdateNetworkInterfaceUseCase(kr, or)
	op, err := uc.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: nicID,
		NetworkInterface:   domain.NetworkInterface{V4AddressIDs: []string{nicRefAddrID}},
		UpdateMask:         []string{"v4_address_ids"},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.NotNil(t, saved.Error, "чужой адрес не может быть привязан правкой")
	assert.Equal(t, int32(codes.NotFound), saved.Error.Code)
	assert.Equal(t, nicRefMiss(nicRefAddrID), saved.Error.Message)

	rd, rerr := kr.Reader(context.Background())
	require.NoError(t, rerr)
	a, gerr := rd.Addresses().Get(context.Background(), nicRefAddrID)
	_ = rd.Close()
	require.NoError(t, gerr)
	assert.False(t, a.Used, "чужой адрес не помечается занятым")
}
