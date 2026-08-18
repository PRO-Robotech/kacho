// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

// Инъекция для гейта отображения отказа учёта — в ОБЕ стороны.
//
// Без законного близнеца гейт ловил бы форму, а не существо: первое же
// срабатывание на исправном владельце его отключит. Поэтому здесь стоят рядом
// три синтетических владельца — полный, неполный и такой, у которого нужные
// имена есть ТОЛЬКО в комментариях.
//
// Третий — не педантизм, а причина, по которой гейт разбирает AST: слова
// `KQ001` и `QUOTA_EXCEEDED` встречаются в прозе этого дерева десятками, в том
// числе в шапке самого гейта. Предикат по подстроке зеленел бы на объяснении
// самого себя.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const injFullOwner = `package pg

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errExceeded = errors.New("resource count quota exceeded")
var errNotProvisioned = errors.New("resource count quota not provisioned")

func classify(code string) error {
	switch code {
	case "KQ001":
		return errExceeded
	case "KQ002":
		return errNotProvisioned
	}
	return nil
}

func refuse(err error) error {
	if errors.Is(err, errExceeded) {
		st := status.New(codes.ResourceExhausted, "full")
		out, _ := st.WithDetails(&errdetails.ErrorInfo{Reason: "QUOTA_EXCEEDED"})
		return out.Err()
	}
	st := status.New(codes.FailedPrecondition, "full")
	out, _ := st.WithDetails(&errdetails.ErrorInfo{Reason: "QUOTA_NOT_PROVISIONED"})
	return out.Err()
}
`

// injPartialOwner — SQLSTATE опознаёт, наружу отказ НЕ производит. Ровно то
// состояние, в котором был найден шестой владелец.
const injPartialOwner = `package pg

import "errors"

var errExceeded = errors.New("resource count quota exceeded")

func classify(code string) error {
	switch code {
	case "KQ001", "KQ002":
		return errExceeded
	}
	return nil
}
`

// injCommentOnlyOwner — все нужные имена присутствуют, и ни одно не исполняется.
const injCommentOnlyOwner = `package pg

// Отказ учёта поднимается кодами KQ001 и KQ002; наружу он уходит признаками
// QUOTA_EXCEEDED и QUOTA_NOT_PROVISIONED поверх codes.ResourceExhausted и
// codes.FailedPrecondition.
func nothing() {}
`

func writeInjOwner(t *testing.T, root, svc, body string) string {
	t.Helper()
	dir := filepath.Join(root, "services", svc, "internal", "repo")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "errmap.go")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestQuotaRefusalMappingGate_FailsOnTheGapAndIsSilentOnItsLegalTwin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := []string{
		writeInjOwner(t, root, "full", injFullOwner),
		writeInjOwner(t, root, "partial", injPartialOwner),
		writeInjOwner(t, root, "commentonly", injCommentOnlyOwner),
	}

	serviceOf := func(path string) string {
		p := filepath.ToSlash(path)
		i := strings.Index(p, "/services/")
		if i < 0 {
			return ""
		}
		return strings.Split(p[i+len("/services/"):], "/")[0]
	}

	facts, parsed := scanQuotaRefusalMapping(t, files, serviceOf)
	require.Equal(t, 3, parsed, "разобрано не то число файлов, что подано")

	t.Run("законный близнец — гейт молчит", func(t *testing.T) {
		require.Empty(t, quotaRefusalFindings([]string{"full"}, facts),
			"полный владелец объявлен находкой: гейт ловит форму, а не существо, "+
				"и первое же ложное срабатывание его отключит")
	})

	t.Run("пропуск — гейт краснеет и называет владельца", func(t *testing.T) {
		findings := quotaRefusalFindings([]string{"partial"}, facts)
		require.Len(t, findings, 2,
			"у владельца без производителя отказа обязаны быть названы ОБА исхода")
		joined := strings.Join(findings, "\n")
		require.Contains(t, joined, "partial", "находка не называет владельца")
		require.Contains(t, joined, "QUOTA_EXCEEDED")
		require.Contains(t, joined, "QUOTA_NOT_PROVISIONED")
		require.NotContains(t, joined, "KQ001",
			"SQLSTATE у этого владельца опознаётся — находка о нём была бы ложной")
	})

	t.Run("имена только в комментариях — это НЕ производитель", func(t *testing.T) {
		findings := quotaRefusalFindings([]string{"commentonly"}, facts)
		require.Len(t, findings, 3,
			"проза, называющая все нужные имена, зачтена за реализацию: предикат "+
				"читает текст, а не код")
	})

	t.Run("читать нечего — это находка, а не успех", func(t *testing.T) {
		empty, n := scanQuotaRefusalMapping(t, nil, serviceOf)
		require.Zero(t, n, "перепись обязана назвать ноль прочитанного")
		require.Len(t, quotaRefusalFindings([]string{"full"}, empty), 3,
			"на пустом корпусе гейт обязан отчитаться находками, а не тишиной: "+
				"иначе «ноль находок» неотличимо от «ноль прочитанного»")
	})
}
