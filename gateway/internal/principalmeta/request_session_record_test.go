// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

// request_session_record_test.go — строитель исходящих метаданных и ссылка на
// запись сессии (kacho#1280, приёмка kaname
// docs/engineering/acceptance/assurance-level-is-declared-by-our-session.md,
// Ф11-45 «в обеих формах»).
//
// ПРЕДМЕТ. Полоса нашей сессии ставит ссылку в ОБЕИХ поверхностных формах —
// голой и мостовой, как уровень подтверждения. Служба принимает ссылку, только
// когда значение одно; поэтому единственный производитель ключа за мостом —
// SessionRecordMetadata: он читает обе формы и кладёт ровно одно значение, а
// мост этот ключ не пропускает. Общий строитель MetadataFromRequest ссылку НЕ
// кладёт: он строит метаданные для любого адресата, а ссылка — только службе
// доступа (kacho#3136).
//
// БЛИЗНЕЦЫ. Три входа отличаются ровно одним фактом — какая форма выставлена,
// — и исход у всех трёх один: [R_S]. Четвёртый — заголовка нет ни в одной
// форме — пара к «кладёт»: ключа в метаданных нет вовсе.

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

const builderRecordS = "hss-00000000000001280s"

func TestSessionRecordMetadata_OneValueWhicheverFormIsSet(t *testing.T) {
	cases := []struct {
		name  string
		forms []string
	}{
		{"both forms", []string{principalmeta.HeaderTokenSessionID, principalmeta.HeaderGRPCMetaTokenSessionID}},
		{"bare form only", []string{principalmeta.HeaderTokenSessionID}},
		{"bridge form only", []string{principalmeta.HeaderGRPCMetaTokenSessionID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/iam/v1/projects", nil)
			for _, h := range tc.forms {
				r.Header.Set(h, builderRecordS)
			}
			got := principalmeta.SessionRecordMetadata(r).Get(principalmeta.MetaTokenSessionID)
			if !reflect.DeepEqual(got, []string{builderRecordS}) {
				t.Fatalf("строитель положил %s = %q, want [%q] — одно значение при любой выставленной форме",
					principalmeta.MetaTokenSessionID, got, builderRecordS)
			}
		})
	}
}

func TestSessionRecordMetadata_AbsentHeaderAddsNoKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/projects", nil)
	md := principalmeta.SessionRecordMetadata(r)
	if _, ok := md[principalmeta.MetaTokenSessionID]; ok {
		t.Fatalf("строитель положил ключ %s без заголовка: %q — пустое значение служба прочла бы как названное",
			principalmeta.MetaTokenSessionID, md[principalmeta.MetaTokenSessionID])
	}
}

// Общий строитель ссылку не кладёт ни при какой форме: его метаданные уходят
// любому адресату. Близнец — личность того же запроса он кладёт.
func TestMetadataFromRequest_CarriesNoSessionRecord(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	r.Header.Set(principalmeta.HeaderTokenSessionID, builderRecordS)
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenSessionID, builderRecordS)
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr-3136")
	md := principalmeta.MetadataFromRequest(r)
	if got := md.Get(principalmeta.MetaPrincipalID); !reflect.DeepEqual(got, []string{"usr-3136"}) {
		t.Fatalf("контроль: личность %q, want [usr-3136]", got)
	}
	if got, ok := md[principalmeta.MetaTokenSessionID]; ok {
		t.Fatalf("общий строитель положил %s = %q — ссылка уехала бы любому адресату",
			principalmeta.MetaTokenSessionID, got)
	}
}

// Мост ключ ссылки не пропускает: его единственный производитель — строитель.
// Пара: ключ того же подсемейства, который строитель не кладёт, мост пропускает.
func TestSessionRecordKey_IsAnnotatorProduced(t *testing.T) {
	if !principalmeta.IsAnnotatorProducedKey(principalmeta.MetaTokenSessionID) {
		t.Errorf("%s не внесён в ключи аннотатора — мост пропустил бы обе формы, и служба увидела бы два значения",
			principalmeta.MetaTokenSessionID)
	}
	if principalmeta.IsAnnotatorProducedKey(principalmeta.MetaTokenJti) {
		t.Errorf("контроль: %s строитель не кладёт, его производитель — мост", principalmeta.MetaTokenJti)
	}
}
