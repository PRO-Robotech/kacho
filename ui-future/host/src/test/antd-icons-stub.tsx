import type { HTMLAttributes } from "react";
import { createElement } from "react";

// Тест-стаб @ant-design/icons для host-jest (kacho#7). Заменяет прежний
// jest.unstable_mockModule Proxy-мок в setup.ts: Proxy НЕ даёт СТАТИЧЕСКИХ
// named-экспортов, поэтому под --experimental-vm-modules ESM-линкер
// `import { XOutlined } from "@ant-design/icons"` висел ВЕЧНО, ожидая binding
// (доказано DIAG6: import antd-иконки виснет на link ДАЖЕ без рендера; lucide — ok).
// Здесь каждый используемый glyph — реальный статический named-export (<span>) →
// линкер резолвит. Список = все named-импорты @ant-design/icons в host src
// (единственный импортёр — HostRail, 20 иконок). Новую иконку в проде → добавить
// сюда; CI host-hang guard ловит регресс этого класса.
const Icon = (props: HTMLAttributes<HTMLSpanElement>) => createElement("span", props);

export const ApartmentOutlined = Icon;
export const ApiOutlined = Icon;
export const AppstoreOutlined = Icon;
export const BankOutlined = Icon;
export const CameraOutlined = Icon;
export const ClusterOutlined = Icon;
export const DesktopOutlined = Icon;
export const FileImageOutlined = Icon;
export const GatewayOutlined = Icon;
export const GlobalOutlined = Icon;
export const HddOutlined = Icon;
export const HistoryOutlined = Icon;
export const KeyOutlined = Icon;
export const NodeIndexOutlined = Icon;
export const ProductOutlined = Icon;
export const ProjectOutlined = Icon;
export const RobotOutlined = Icon;
export const SafetyCertificateOutlined = Icon;
export const SafetyOutlined = Icon;
export const TeamOutlined = Icon;
export const UserOutlined = Icon;

export default Icon;
