// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

// План, состояние и настройка — три РАЗНЫХ типа фреймворка с одинаковым методом чтения.
// Обёртки существуют, чтобы механика плоского ресурса писалась один раз, а не по копии на
// каждый источник значений: три копии одной функции разъезжаются молча.

type diagList = diag.Diagnostics

type planGetter struct{ p tfsdk.Plan }

func (g planGetter) GetAttribute(ctx context.Context, pth path.Path, target any) diagList {
	return g.p.GetAttribute(ctx, pth, target)
}

type stateGetter struct{ s tfsdk.State }

func (g stateGetter) GetAttribute(ctx context.Context, pth path.Path, target any) diagList {
	return g.s.GetAttribute(ctx, pth, target)
}

// setter — сторона записи. Отдельный интерфейс, потому что писать умеет только состояние.
type setter interface {
	SetAttribute(ctx context.Context, p path.Path, val any) diagList
}

type stateSetter struct{ s *tfsdk.State }

func (w stateSetter) SetAttribute(ctx context.Context, pth path.Path, val any) diagList {
	return w.s.SetAttribute(ctx, pth, val)
}
