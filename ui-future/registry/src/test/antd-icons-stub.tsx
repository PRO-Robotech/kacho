import type { HTMLAttributes } from "react";
import { createElement } from "react";

// Тест-стаб @ant-design/icons (kacho#7). Заменяет jest.unstable_mockModule Proxy-мок
// из setup.ts: Proxy НЕ даёт статических named-экспортов → под --experimental-vm-modules
// ESM-линкер `import { XOutlined } from "@ant-design/icons"` виснет вечно (доказано на host).
// Реальные статические named-экспорты (<span>) → линкер резолвит. Список = все
// named-импорты @ant-design/icons в src пакета. Новую иконку → добавить сюда.
const Icon = (props: HTMLAttributes<HTMLSpanElement>) => createElement("span", props);

export const ApartmentOutlined = Icon;
export const ApiOutlined = Icon;
export const AppstoreOutlined = Icon;
export const ArrowRightOutlined = Icon;
export const CameraOutlined = Icon;
export const CheckCircleFilled = Icon;
export const ClockCircleOutlined = Icon;
export const CloseCircleFilled = Icon;
export const CloseOutlined = Icon;
export const ClusterOutlined = Icon;
export const CodeOutlined = Icon;
export const ContainerOutlined = Icon;
export const CopyOutlined = Icon;
export const DatabaseOutlined = Icon;
export const DeleteOutlined = Icon;
export const DesktopOutlined = Icon;
export const DownloadOutlined = Icon;
export const DragOutlined = Icon;
export const EditOutlined = Icon;
export const EyeOutlined = Icon;
export const FileImageOutlined = Icon;
export const FormOutlined = Icon;
export const GatewayOutlined = Icon;
export const GlobalOutlined = Icon;
export const HddOutlined = Icon;
export const InfoCircleOutlined = Icon;
export const LoadingOutlined = Icon;
export const LockOutlined = Icon;
export const MinusCircleFilled = Icon;
export const MoreOutlined = Icon;
export const NodeIndexOutlined = Icon;
export const PlusOutlined = Icon;
export const QuestionCircleOutlined = Icon;
export const ReadOutlined = Icon;
export const ReloadOutlined = Icon;
export const RightOutlined = Icon;
export const SafetyCertificateOutlined = Icon;
export const SafetyOutlined = Icon;
export const SearchOutlined = Icon;
export const SettingOutlined = Icon;
export const TagsOutlined = Icon;
export const UserOutlined = Icon;
export const WarningFilled = Icon;

export default Icon;
