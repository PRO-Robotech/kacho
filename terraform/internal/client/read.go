// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Verdict — что провайдер УСТАНОВИЛ о судьбе ресурса, которого нет по идентификатору.
//
// Ответ «не найдено» при отказе в доступе побайтово равен настоящему отсутствию — это
// намеренное свойство края (сокрытие существования). Поэтому одиночный такой ответ ничего
// не устанавливает, и различение выносится в отдельный шаг.
type Verdict int

const (
	// VerdictGone — ресурса действительно нет: контрольная страница проекта непуста,
	// значит пообъектная выдача работает, и отсутствие нашего — настоящее удаление.
	VerdictGone Verdict = iota

	// VerdictPresent — ресурс есть, расхождения нет. Первый ответ был окном прав.
	VerdictPresent

	// VerdictDenied — список отвечает отказом: доступ к проекту утрачен.
	VerdictDenied

	// VerdictAmbiguous — различить нечем: контрольная страница пуста целиком, или обход
	// не завершён. Провайдер останавливается, а не решает за оператора.
	VerdictAmbiguous
)

// String — для текстов ошибок.
func (v Verdict) String() string {
	switch v {
	case VerdictGone:
		return "Gone"
	case VerdictPresent:
		return "Present"
	case VerdictDenied:
		return "Denied"
	case VerdictAmbiguous:
		return "Ambiguous"
	}
	return fmt.Sprintf("Verdict(%d)", int(v))
}

// ListPage — то, что провайдеру нужно от списочного ответа: идентификаторы и курсор.
type ListPage struct {
	IDs           []string
	NextPageToken string
}

// ConfirmAbsence устанавливает, что означает «ресурса нет по идентификатору».
//
// Порядок и его цена:
//
//  1. список по ИМЕНИ в пределах проекта. Нашли — ресурс жив (первый ответ был окном
//     прав), расхождения нет;
//  2. не нашли — КОНТРОЛЬНАЯ страница проекта без фильтра. Есть хоть один ресурс того же
//     типа ⇒ пообъектная выдача работает ⇒ наш действительно удалён;
//  3. контрольная страница пуста целиком ⇒ различить «в проекте больше ничего нет» и
//     «пообъектные права ушли» НЕЧЕМ ⇒ остановка.
//
// Цена шага 3 названа честно: единственный ресурс проекта, удалённый вручную, даст ложную
// остановку. Это осознанный размен — ложная остановка стоит одной команды оператора,
// ложное пересоздание уничтожает инфраструктуру.
//
// Слепая зона, которая ОСТАЁТСЯ: контрольная страница непуста, а наш объект невидим
// пообъектно (точечно отозванная выдача). Такой случай будет принят за удаление. Защита от
// него — обязательное имя: пересоздание упрётся в громкий конфликт имени, а не создаст
// молчаливый дубль.
// scopeParam — имя параметра области в списочном запросе.
//
// У ресурсов vpc и nlb это проект, у ресурсов iam — аккаунт. Параметр назван явно, потому
// что подстановка «всегда projectId» дала бы у iam пустой список на КАЖДОМ подтверждении:
// край отверг бы неизвестный параметр молча, а провайдер прочитал бы это как «в области
// ничего нет» и остановился бы на ровном месте.
const (
	ScopeProject = "projectId"
	ScopeAccount = "accountId"
)

// ConfirmAbsence — см. описание исходов у типа Verdict.
func (c *Client) ConfirmAbsence(ctx context.Context, collectionPath, scopeParam, scopeID, name string) (Verdict, error) {
	byName, err := c.listPage(ctx, collectionPath, scopeParam, scopeID, name)
	if err != nil {
		return VerdictAmbiguous, err
	}
	switch byName.verdict {
	case VerdictDenied:
		return VerdictDenied, nil
	case VerdictAmbiguous:
		return VerdictAmbiguous, byName.err
	}
	if len(byName.page.IDs) > 0 {
		return VerdictPresent, nil
	}

	control, err := c.listPage(ctx, collectionPath, scopeParam, scopeID, "")
	if err != nil {
		return VerdictAmbiguous, err
	}
	switch control.verdict {
	case VerdictDenied:
		return VerdictDenied, nil
	case VerdictAmbiguous:
		return VerdictAmbiguous, control.err
	}
	if len(control.page.IDs) == 0 {
		return VerdictAmbiguous, nil
	}
	return VerdictGone, nil
}

type listResult struct {
	page    ListPage
	verdict Verdict
	err     error
}

// listPage читает ОДНУ страницу коллекции. Курсор не проходится целиком намеренно:
// контрольный вопрос — «есть ли в проекте хоть что-нибудь этого типа», и для него первой
// страницы достаточно. Поиск по имени сужен фильтром, поэтому его ответ помещается в
// страницу by construction.
func (c *Client) listPage(ctx context.Context, collectionPath, scopeParam, scopeID, name string) (listResult, error) {
	q := url.Values{}
	q.Set(scopeParam, scopeID)
	if name != "" {
		q.Set("filter", fmt.Sprintf("name=%q", name))
	}
	q.Set("pageSize", "100")

	resp, err := c.Do(ctx, "GET", collectionPath+"?"+q.Encode(), nil, nil)
	if err != nil {
		return listResult{verdict: VerdictAmbiguous, err: err}, err
	}

	switch out := Classify(resp); out.Kind {
	case OutcomeOK:
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(resp.Body, &raw); err != nil {
			return listResult{verdict: VerdictAmbiguous,
				err: fmt.Errorf("разбор списка %s: %w", collectionPath, err)}, nil
		}
		return listResult{page: extractIDs(raw), verdict: VerdictPresent}, nil
	case OutcomeDenied, OutcomeUnauthenticated:
		return listResult{verdict: VerdictDenied}, nil
	default:
		return listResult{verdict: VerdictAmbiguous,
			err: fmt.Errorf("список %s: %s", collectionPath, out.Message)}, nil
	}
}

// extractIDs достаёт идентификаторы из списочного ответа, не зная имени коллекции.
//
// Форма ответа — объект с ОДНИМ массивом объектов (networks, subnets, …) и курсором.
// Перебирать здесь имена коллекций значило бы завести второй список ресурсов, который
// разъедется с первым молча.
func extractIDs(raw map[string]json.RawMessage) ListPage {
	page := ListPage{}
	if tok, ok := raw["nextPageToken"]; ok {
		_ = json.Unmarshal(tok, &page.NextPageToken)
	}
	for key, val := range raw {
		if key == "nextPageToken" {
			continue
		}
		var items []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(val, &items); err != nil {
			continue
		}
		for _, it := range items {
			if it.ID != "" {
				page.IDs = append(page.IDs, it.ID)
			}
		}
	}
	return page
}
