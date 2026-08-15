import type { FC } from "react";
import { Button, Result } from "antd";

export interface ModuleUnavailablePanelProps {
  /** Имя раздела так, как его видит пользователь в меню. Экран обязан называть
   *  раздел ПО ИМЕНИ: «что-то пошло не так» не говорит, ЧТО именно недоступно. */
  moduleLabel: string;
  /** Техническая причина — вспомогательная строка, не заголовок. */
  detail?: string;
  onRetry?: () => void;
}

/**
 * Экран «раздел недоступен» — единственная форма отказа модуля в консоли.
 *
 * Роль `alert` и `data-testid` живут на собственной обёртке, а не на `Result`:
 * заменитель antd в пробах пропускает в DOM только `data-*`/`aria-*`/`id`/`title`,
 * поэтому утверждение, повешенное на `role` самого `Result`, было бы верно лишь
 * в одном из двух харнессов (host держит настоящий antd, остальные — заменитель).
 */
export const ModuleUnavailablePanel: FC<ModuleUnavailablePanelProps> = ({ moduleLabel, detail, onRetry }) => (
  <div role="alert" data-testid="module-unavailable" data-module-label={moduleLabel}>
    <Result
      status="warning"
      title={`Раздел «${moduleLabel}» недоступен`}
      subTitle={
        detail
          ? `Модуль не загрузился: ${detail}. Остальные разделы консоли работают.`
          : "Модуль не загрузился. Остальные разделы консоли работают."
      }
      extra={
        onRetry ? (
          <Button type="primary" onClick={onRetry}>
            Повторить
          </Button>
        ) : undefined
      }
    />
  </div>
);
