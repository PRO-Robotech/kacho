// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"google.golang.org/protobuf/proto"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// Общая механика ресурсов Kachō живёт функциями ниже, а не интерфейсом-каркасом.
//
// Здесь стоял обобщённый интерфейс вида resourceKind[M] — попытка описать «ресурс вообще»
// до того, как ресурсов стало больше одного. Ни один ресурс его не реализовал: разница
// между ними лежит не в наборе методов, а в форме тела и в том, какими действиями края
// приводится состав, и интерфейс эту разницу не выражал. Мёртвый каркас снят вместе с
// объявлением: он выглядел устройством, которого нет.

// importByID — общая проверка формата при импорте.
//
// Формат проверяется общим каталогом префиксов платформы ДО обращения к краю: заведомо
// негодная строка получает терминальный отказ с внятным текстом, а не уезжает в сеть за
// ответом «не найдено», который для неё ничего не значит.
func importByID(ctx context.Context, kindName, prefix string, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Форма — по общему каталогу платформы; принадлежность префикса каталогу проверяется
	// там же, поэтому опечатка в самой таблице видов провайдера тоже будет поймана.
	if !ids.IsValid(req.ID, prefix) || !ids.HasKnownPrefix(req.ID) {
		resp.Diagnostics.AddError("Негодный идентификатор: "+kindName,
			"Строка "+strconv.Quote(req.ID)+" не является идентификатором этого ресурса: "+
				"он начинается с «"+prefix+"» и состоит из знаков crockford-base32. "+
				"Идентификатор виден в выводе списка и в консоли.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// awaitCreate отправляет создание, дожидается операции и возвращает идентификатор.
//
// Порядок буквальный: операция завершена → УТВЕРЖДЕНИЕ, что отказа нет → метаданные.
// Идентификатор присутствует в метаданных даже у операции, завершившейся отказом.
func awaitCreate(ctx context.Context, c *client.Client, path, idField, resourceType, address string, body proto.Message) (string, error) {
	raw, _ := json.Marshal(map[string]string{"t": resourceType, "a": address})
	hdr := &client.Headers{IdempotencyKey: client.IdempotencyKey(resourceType, address, raw)}

	resp, err := c.Do(ctx, http.MethodPost, path, body, hdr)
	if err != nil {
		return "", err
	}
	if out := client.Classify(resp); out.Kind != client.OutcomeOK {
		return "", fmt.Errorf("%s", out.Message)
	}
	var op client.Operation
	if err := json.Unmarshal(resp.Body, &op); err != nil {
		return "", fmt.Errorf("разбор ответа: %w", err)
	}
	done, err := c.AwaitOperation(ctx, op.ID, client.AwaitOptions{})
	if err != nil {
		return "", err
	}
	id, ok := done.MetadataString(idField)
	if !ok || id == "" {
		return "", fmt.Errorf("операция завершилась успехом, но её метаданные не содержат %s — "+
			"записывать в состояние нечего, а ресурс, возможно, создан", idField)
	}
	return id, nil
}

// awaitMutation отправляет изменение или удаление и дожидается операции.
func awaitMutation(ctx context.Context, c *client.Client, method, path string, body proto.Message) error {
	resp, err := c.Do(ctx, method, path, body, nil)
	if err != nil {
		return err
	}
	if out := client.Classify(resp); out.Kind != client.OutcomeOK {
		return fmt.Errorf("%s", out.Message)
	}
	var op client.Operation
	if err := json.Unmarshal(resp.Body, &op); err == nil && op.ID != "" {
		if _, err := c.AwaitOperation(ctx, op.ID, client.AwaitOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// readByID читает ресурс по идентификатору.
//
// retryAuthz включается ТОЛЬКО на первом обратном чтении после создания: право на
// собственный свежий ресурс материализуется не мгновенно. В обычном чтении такого ретрая
// нет — там он замаскировал бы настоящий отзыв доступа.
func readByID(ctx context.Context, c *client.Client, collection, id string, retryAuthz bool) ([]byte, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := c.Do(ctx, http.MethodGet, collection+"/"+id, nil, nil)
		if err != nil {
			return nil, err
		}
		out := client.Classify(resp)
		switch out.Kind {
		case client.OutcomeOK:
			return resp.Body, nil
		case client.OutcomeNotFound, client.OutcomeDenied:
			if retryAuthz && time.Now().Before(deadline) {
				time.Sleep(time.Second)
				continue
			}
			if out.Kind == client.OutcomeNotFound {
				return nil, &notFoundError{msg: out.Message}
			}
			return nil, fmt.Errorf("доступ к ресурсу %s: %s", id, out.Message)
		default:
			return nil, fmt.Errorf("чтение ресурса %s: %s", id, out.Message)
		}
	}
}

// absenceDiagnostics — что делать с ресурсом, которого нет по идентификатору.
//
// Возвращает: снимать ли из состояния, и текст остановки, если снимать нельзя.
// Одиночное «не найдено» ничего не устанавливает: тот же ответ приходит при отказе в
// доступе, и он побайтово равен настоящему отсутствию.
func absenceDiagnostics(ctx context.Context, c *client.Client, collection, scopeParam, kindName, id, scopeID, name string) (remove bool, title, detail string) {
	verdict, err := c.ConfirmAbsence(ctx, collection, scopeParam, scopeID, name)
	switch verdict {
	case client.VerdictGone:
		return true, "", ""
	case client.VerdictPresent:
		return false, "", ""
	case client.VerdictDenied:
		return false, "Доступ к проекту утрачен",
			kindName + " " + id + " не читается, и список проекта тоже отвечает отказом. " +
				"Это событие ПРАВ, а не удаление ресурса: продолжать нельзя, иначе план предложит " +
				"пересоздать инфраструктуру, которая цела."
	default:
		d := kindName + " " + id + " не найден, и подтвердить отсутствие нечем: в проекте не видно " +
			"ни одного ресурса этого типа. Различить «удалён» и «доступ отозван» край не позволяет — " +
			"ответы совпадают дословно.\n\nЕсли ресурс действительно удалён вне Terraform, уберите его " +
			"из состояния вручную:\n  terraform state rm <адрес ресурса>"
		if err != nil {
			d += "\n\nПодробности: " + err.Error()
		}
		return false, "Отсутствие не подтверждено", d
	}
}
