// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
)

// Формат страницы судится ДО решения о доступе.
//
// Предмет. `ListRepositories` и `ListTags` спрашивали права первой строкой, а
// page_size/page_token уезжали дальше и проверялись уже за гейтом. Следствие: один
// и тот же мусорный курсор отвечал по-разному в зависимости от того, что вызывающему
// выдано — тому, у кого грант есть, INVALID_ARGUMENT, а тому, у кого нет, отказ или
// пустая страница. Вызывающий не отличает «твой курсор сломан» от «тебе сюда
// нельзя», и оператор в логах — тоже.
//
// Проба берёт вызывающего БЕЗ прав (пустой контекст — субъект не назван вовсе) и
// требует, чтобы ответ был про ФОРМАТ. Это и есть предмет: ответ на формат не
// зависит от прав.
//
// Пара, а не одиночное отрицание. Рядом стоит контроль сохранности гейта: при
// ЗАКОННОЙ пагинации тот же вызывающий по-прежнему получает отказ по правам, а не
// проходит внутрь. Без него «стало InvalidArgument на всё» читалось бы как успех.
func TestRegistryListsJudgePageFormatBeforeAccess(t *testing.T) {
	const registryID = "regprobe000000000"

	type probe struct {
		name string
		call func(h *RegistryHandler, ctx context.Context, size int32, token string) error
	}
	probes := []probe{
		{"ListRepositories", func(h *RegistryHandler, ctx context.Context, size int32, token string) error {
			_, err := h.ListRepositories(ctx, &registryv1.ListRepositoriesRequest{
				RegistryId: registryID, PageSize: size, PageToken: token})
			return err
		}},
		{"ListTags", func(h *RegistryHandler, ctx context.Context, size int32, token string) error {
			_, err := h.ListTags(ctx, &registryv1.ListTagsRequest{
				RegistryId: registryID, Repository: "app", PageSize: size, PageToken: token})
			return err
		}},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			t.Run("page_size вне диапазона — InvalidArgument, а не отказ по правам", func(t *testing.T) {
				err := regCallNoPanic(t, func() error {
					return p.call(&RegistryHandler{}, context.Background(), 1001, "")
				})
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err),
					"ответ на формат зависит от прав: гейт доступа отработал раньше проверки формата")
				assert.True(t, regNamesField(err, "page_size"), "отказ обязан назвать поле: %v", err)
			})

			t.Run("отрицательный page_size — InvalidArgument", func(t *testing.T) {
				err := regCallNoPanic(t, func() error {
					return p.call(&RegistryHandler{}, context.Background(), -1, "")
				})
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
				assert.True(t, regNamesField(err, "page_size"), "отказ обязан назвать поле: %v", err)
			})

			t.Run("мусорный page_token — InvalidArgument, а не отказ по правам", func(t *testing.T) {
				err := regCallNoPanic(t, func() error {
					return p.call(&RegistryHandler{}, context.Background(), 10, "%%%not-a-cursor%%%")
				})
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err),
					"ответ на формат зависит от прав: гейт доступа отработал раньше проверки формата")
			})

			t.Run("гейт доступа уцелел: законная страница по-прежнему НЕ проходит без прав", func(t *testing.T) {
				err := regCallNoPanic(t, func() error {
					return p.call(&RegistryHandler{}, context.Background(), 10, "")
				})
				require.Error(t, err, "вызывающий без прав прошёл внутрь: гейт снят, а не переставлен")
				assert.NotEqual(t, codes.InvalidArgument, status.Code(err),
					"законная страница отвергнута по формату: проба отвергает всё подряд")
			})
		})
	}
}

// regNamesField — отказ по формату обязан НАЗВАТЬ поле. Имя живёт либо в
// google.rpc.BadRequest.field_violations (туда его кладёт платформенный
// validate.PageSize), либо в тексте — принимаем оба: предмет утверждения «ответ про
// это поле», а не «ответ такой-то формы».
func regNamesField(err error, field string) bool {
	st := status.Convert(err)
	if strings.Contains(st.Message(), field) {
		return true
	}
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			if v.GetField() == field || strings.Contains(v.GetDescription(), field) {
				return true
			}
		}
	}
	return false
}

// regCallNoPanic — паника означала бы, что запрос дошёл до неподключённого
// сотрудника; предмет пробы — код и причина ответа, а не крах.
func regCallNoPanic(t *testing.T, call func() error) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("запрос дошёл до неподключённого сотрудника (%v)", r)
		}
	}()
	return call()
}
