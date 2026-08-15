import { Component } from "react";
import type { ComponentType, ErrorInfo, ReactNode } from "react";
import { ModuleUnavailablePanel } from "./ModuleUnavailablePanel";

export interface ModuleErrorBoundaryProps {
  /** Имя раздела для экрана отказа. Обязательно: экран называет раздел по имени. */
  moduleLabel: string;
  /**
   * Вызывается по кнопке «Повторить» ПОСЛЕ сброса состояния границы. Ленивый
   * модуль обязан здесь пересоздать свой `lazy()`: React кэширует ОТКЛОНЁННЫЙ
   * промис навсегда, поэтому один только сброс границы даст тот же отказ сразу.
   */
  onRetry?: () => void;
  children?: ReactNode;
}

interface ModuleErrorBoundaryState {
  error: Error | null;
}

/**
 * Граница отказа модуля консоли (#371).
 *
 * Консоль собрана на Module Federation: девять микрофронтендов приезжают по сети
 * во время работы. Без границы отказ ОДНОГО из них снимает с экрана всё дерево —
 * React 16+ размонтирует корень, если непойманная ошибка дошла до него. `Suspense`
 * это не закрывает: он ловит ОЖИДАНИЕ, а не отказ.
 *
 * Граница ставится и на корне приложения, и на каждом удалённом модуле: корневая
 * одна оставила бы пользователя без имени отказавшего раздела и без работающего
 * остатка консоли.
 */
export class ModuleErrorBoundary extends Component<ModuleErrorBoundaryProps, ModuleErrorBoundaryState> {
  state: ModuleErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: unknown): ModuleErrorBoundaryState {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // Отказ обязан быть виден в журнале браузера: экран называет раздел, журнал —
    // место. Без этого «раздел недоступен» неотличим от «раздел пуст».
    console.error(`[kacho] раздел «${this.props.moduleLabel}» отказал`, error, info.componentStack);
  }

  private handleRetry = (): void => {
    this.setState({ error: null });
    this.props.onRetry?.();
  };

  render(): ReactNode {
    const { error } = this.state;
    if (error) {
      return (
        <ModuleUnavailablePanel
          moduleLabel={this.props.moduleLabel}
          detail={error.message || undefined}
          onRetry={this.handleRetry}
        />
      );
    }
    return this.props.children;
  }
}

/**
 * Обёртка страницы, выставленной модулем наружу через федерацию.
 *
 * Живёт в самом модуле, а не только в host: у модуля есть и собственная точка
 * входа (standalone-разработка), где host-границы нет вовсе, — а именно из-за
 * неё «граница только в host» оставляла бы половину путей без защиты.
 */
export function withModuleBoundary<P extends object>(Page: ComponentType<P>, moduleLabel: string): ComponentType<P> {
  const Guarded = (props: P) => (
    <ModuleErrorBoundary moduleLabel={moduleLabel}>
      <Page {...props} />
    </ModuleErrorBoundary>
  );
  Guarded.displayName = `withModuleBoundary(${Page.displayName ?? Page.name ?? "Page"})`;
  return Guarded;
}
