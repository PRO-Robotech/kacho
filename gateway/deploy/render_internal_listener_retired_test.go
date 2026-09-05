// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// render_internal_listener_retired_test.go — рендер-страж снятого внутреннего
// листенера края.
//
// ПРЕДМЕТ. Задача #1024 сняла у края единственную службу его cluster-internal
// gRPC-листенера, а с ней и сам листенер: входной поверхности больше нет.
// Вместе с ней снята и ручка чарта `internalListener` со всеми четырьмя
// переменными `KACHO_API_GATEWAY_INTERNAL_GRPC_*`.
//
// Здесь, отдельным файлом, стоял более узкий страж, запрещавший ОДНО имя —
// переменную отзыва отражения схемы (имя файла не воспроизводится: в дереве его
// нет, и запись читалась бы как живая координата). Его предмет поглощён этим
// стражем: отражение подавалось ИМЕННО на внутреннем листенере, и запрет одного
// имени на чарте, где листенера нет вовсе, утверждал бы меньше, чем есть. Страж
// заменён, а не ослаблен — запрещается весь префикс, включая прежнее имя, и
// прежнее значение оверлея выставляется в списке ниже.
//
// СНЯТАЯ РУЧКА ОСТАВЛЯЕТ ЗНАЧЕНИЕ В ЧЬЁМ-ТО ПРОФИЛЕ. «Устаревшие значения
// игнорируются» — утверждение, верное ровно до того дня, когда шаблон обзаведётся
// второй ссылкой. Поэтому оно не оставляется на доверие читателю, а проверяется
// рендером С ВЫСТАВЛЕННЫМИ снятыми значениями.
package deploy_test

import (
	"strings"
	"testing"
)

// retiredInternalListenerEnvPrefix — префикс переменных СВОЕГО внутреннего
// листенера края.
//
// Именно `KACHO_API_GATEWAY_INTERNAL_GRPC_`, а не `INTERNAL_GRPC`: край
// по-прежнему НАБИРАЕТ внутренние листенеры соседей, и их адреса зовутся
// `KACHO_API_GATEWAY_<ДОМЕН>_INTERNAL_GRPC`. Запрет по широкой подстроке снёс бы
// их вместе с предметом — то есть запретил бы краю разговаривать с сервисами.
const retiredInternalListenerEnvPrefix = "KACHO_API_GATEWAY_INTERNAL_GRPC_"

// retiredInternalListenerValues — значения снятой ручки, которые могли остаться
// в чужом оверлее. Выставляются НАРОЧНО: страж обязан утверждать про инертность,
// а не предполагать её.
var retiredInternalListenerValues = []string{
	"internalListener.mtls.enable=true",
	"internalListener.reflection=true",
	"internalListener.allowedSpiffe[0]=spiffe://kacho.cloud/ns/kacho/sa/kaname",
}

// TestRender_RetiredInternalListenerKnobIsInert — снятые значения не доезжают до
// PodSpec ни на одной посадке И не роняют рендер.
//
// Обе половины несущие. Отсутствие переменных без «рендер прошёл» зеленело бы на
// чарте, который перестал рендериться вовсе; «рендер прошёл» без отсутствия
// переменных не сказал бы ничего о предмете.
func TestRender_RetiredInternalListenerKnobIsInert(t *testing.T) {
	for _, tc := range []struct {
		name string
		sets []string
	}{
		{
			name: "умолчания чарта",
			sets: nil,
		},
		{
			name: "снятые значения выставлены оверлеем",
			sets: retiredInternalListenerValues,
		},
		{
			name: "снятые значения при включённом backend-dial mTLS",
			sets: append([]string{"mtls.enable=true"}, retiredInternalListenerValues...),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := helmTemplate(t, tc.sets...)

			// Предпосылка: рендер вообще произвёл PodSpec. Без неё отрицание
			// ниже удовлетворяется пустым выводом и читается как проход.
			mustContain(t, out, "kind: Deployment")
			mustContain(t, out, "KACHO_API_GATEWAY_LISTEN_ADDR")

			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, retiredInternalListenerEnvPrefix) {
					t.Fatalf("переменная снятого внутреннего листенера отрендерена: %q.\n"+
						"У края этого листенера нет (#1024) — процесс переменную не читает, "+
						"поэтому в контейнере она в лучшем случае шум, а в худшем — ручка, "+
						"которую следующий читатель провяжет обратно, решив, что поверхность "+
						"существует", strings.TrimSpace(line))
				}
			}
		})
	}
}

// TestRender_NeighbourInternalAddressesSurvive — обратная сторона запрета выше,
// без которой он был бы маской.
//
// Край СНЯЛ свой внутренний листенер и продолжает НАБИРАТЬ чужие. Их адреса
// отличаются от снятых переменных одним сегментом имени, поэтому страж, взявший
// подстроку пошире, снёс бы связь края с сервисами и остался бы зелёным. Здесь
// утверждается, что этого не произошло.
func TestRender_NeighbourInternalAddressesSurvive(t *testing.T) {
	out := helmTemplate(t)
	for _, env := range []string{
		"KACHO_API_GATEWAY_IAM_INTERNAL_GRPC",
		"KACHO_API_GATEWAY_GEO_INTERNAL_GRPC",
		"KACHO_API_GATEWAY_VPC_INTERNAL_GRPC",
	} {
		mustContain(t, out, env)
		if strings.HasPrefix(env, retiredInternalListenerEnvPrefix) {
			t.Fatalf("предикат запрета накрывает адрес соседа %s — префикс выбран "+
				"слишком широко, и страж запрещал бы связь края с сервисами", env)
		}
	}
}
