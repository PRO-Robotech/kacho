// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"
	"sync"

	"google.golang.org/genproto/googleapis/api/annotations"

	apiv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// rest_bindings.go — полная таблица REST-биндингов Kachō, собранная из
// proto-дескрипторов, слинкованных в бинарь gateway.
//
// internal_routes.go классифицирует «этот запрос — внутренний» и берёт из
// биндинга только пару (метод, шаблон). Контрактному разбору тела запроса нужно
// больше: какому RPC принадлежит биндинг, какое у него входное сообщение и
// КАКАЯ ЕГО ЧАСТЬ приходит телом (`body: "*"` — всё сообщение, `body: "<поле>"`
// — только это поле, отсутствие `body` — тела нет вовсе). Поэтому таблица
// строится один раз здесь и переиспользуется обоими потребителями: разойтись
// они не могут by construction.

// httpBinding — один REST-биндинг одного RPC.
type httpBinding struct {
	method   string
	template string
	segs     []internalRouteSegment
	// fqn — канонический gRPC-FQN вида `kacho.cloud.vpc.v1.NetworkService/Create`
	// (та же форма, что в permission-каталоге и таблице authz-middleware).
	fqn string
	// input — дескриптор сообщения запроса RPC.
	input protoreflect.MessageDescriptor
	// output — дескриптор сообщения ОТВЕТА RPC.
	//
	// Заведено рядом с `input`, а не выведено потребителем из `fqn`: второй путь
	// от FQN к дескриптору разошёлся бы с этим молча на первом же биндинге,
	// который таблица собирает иначе, чем ожидает потребитель. Читает его гейт
	// примеров страниц арендатора: пример ответа сверяется с тем сообщением,
	// которое этот биндинг и отдаёт.
	output protoreflect.MessageDescriptor
	// operationResponse — сообщение, которое RPC кладёт в `Operation.response`,
	// по СВОЕЙ аннотации `(kacho.cloud.api.operation).response`. nil у RPC,
	// который операции не заводит.
	//
	// Собирается здесь, рядом с `output`, по той же причине: страница арендатора
	// у мутирующего глагола показывает не конверт операции, а её ПОЛЕЗНУЮ
	// НАГРУЗКУ, и сверять эту нагрузку надо с тем, что назвал сам контракт.
	// Вывести её потребителю из `fqn` значило бы завести второй путь к тому же
	// дескриптору, который разошёлся бы с этим молча.
	operationResponse protoreflect.MessageDescriptor
	// body — значение `body` из google.api.http: "" (тела нет), "*" (тело —
	// всё сообщение запроса) либо имя поля, в которое разбирается тело.
	body string
	// internal — биндинг принадлежит Internal*-сервису.
	internal bool
}

var (
	httpBindingsOnce sync.Once
	httpBindings     []httpBinding
)

// loadedHTTPBindings возвращает таблицу REST-биндингов, собирая её при первом
// обращении.
func loadedHTTPBindings() []httpBinding {
	httpBindingsOnce.Do(func() { httpBindings = buildHTTPBindings() })
	return httpBindings
}

// buildHTTPBindings обходит глобальный proto-реестр и собирает REST-биндинги
// (включая additional_bindings) всех сервисов домена kacho.
func buildHTTPBindings() []httpBinding {
	var out []httpBinding
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !underDeclaredRoot(string(fd.Package())) {
			return true
		}
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			internal := isInternalServiceName(string(svc.Name()))
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule)
				if rule == nil {
					continue
				}
				fqn := string(svc.FullName()) + "/" + string(m.Name())
				opResp := declaredOperationResponse(fd, m)
				for _, r := range append([]*annotations.HttpRule{rule}, rule.GetAdditionalBindings()...) {
					verb, tmpl := httpRuleVerbAndPath(r)
					if verb == "" || tmpl == "" {
						continue
					}
					out = append(out, httpBinding{
						method:            verb,
						template:          tmpl,
						segs:              parsePathTemplate(tmpl),
						fqn:               fqn,
						input:             m.Input(),
						output:            m.Output(),
						operationResponse: opResp,
						body:              r.GetBody(),
						internal:          internal,
					})
				}
			}
		}
		return true
	})
	return out
}

// declaredOperationResponse — сообщение, названное аннотацией
// `(kacho.cloud.api.operation).response` этого RPC.
//
// Имя в аннотации короткое, в пакете СВОЕГО файла (`response: "Zone"`), и
// изредка полное (`"google.protobuf.Empty"`). Различаются они наличием точки —
// то же соглашение, что читает гейт ведомости носителей секрета в iam; второго
// правила здесь не заводится.
//
// nil означает «RPC операции не объявляет», и это отличимо от «объявляет
// неразрешимый тип»: последнее тоже даёт nil, но такой RPC не собрался бы —
// тип его ответа лежит в том же дереве контрактов.
func declaredOperationResponse(fd protoreflect.FileDescriptor, m protoreflect.MethodDescriptor) protoreflect.MessageDescriptor {
	ext := proto.GetExtension(m.Options(), apiv1.E_Operation)
	op, ok := ext.(*apiv1.Operation)
	if !ok || op == nil || op.GetResponse() == "" {
		return nil
	}
	name := op.GetResponse()
	if !strings.Contains(name, ".") {
		name = string(fd.Package()) + "." + name
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return nil
	}
	return md
}
