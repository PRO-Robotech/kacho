import type { HTMLAttributes } from "react";
import { createElement } from "react";

// Тест-стаб @ant-design/icons для jest-конфигов, гоняющих shared/src
// (system / vpc / iam). Тот же корень, что уже вылечен в host: Proxy-мок через
// jest.unstable_mockModule НЕ даёт СТАТИЧЕСКИХ named-экспортов, поэтому под
// --experimental-vm-modules ESM-линкер на `import { XOutlined } from
// "@ant-design/icons"` не находит binding — и процесс jest уходит молча, с кодом
// 0 и БЕЗ отчёта. Молчание читается как успех: так весь набор shared-суит
// (включая resource-registry.vpc1) числился зелёным, ни разу не выполнившись.
//
// Здесь каждый используемый glyph — реальный статический named-export, линкер
// резолвит. Список = все named-импорты @ant-design/icons в shared/system/vpc/iam;
// его полноту стережёт antd-icons-stub.test.ts (новая иконка в проде без строки
// здесь роняет тест, а не тихо убивает прогон).
const Icon = (props: HTMLAttributes<HTMLSpanElement>) => createElement("span", props);

export const ApartmentOutlined = Icon;
export const ApiOutlined = Icon;
export const AppstoreOutlined = Icon;
export const ArrowLeftOutlined = Icon;
export const ArrowRightOutlined = Icon;
export const BankOutlined = Icon;
export const CameraOutlined = Icon;
export const CaretRightOutlined = Icon;
export const CheckCircleFilled = Icon;
export const CheckCircleOutlined = Icon;
export const ClockCircleOutlined = Icon;
export const CloseCircleFilled = Icon;
export const CloseOutlined = Icon;
export const CloudServerOutlined = Icon;
export const ClusterOutlined = Icon;
export const CodeOutlined = Icon;
export const ContainerOutlined = Icon;
export const CopyOutlined = Icon;
export const DatabaseOutlined = Icon;
export const DeleteOutlined = Icon;
export const DeploymentUnitOutlined = Icon;
export const DesktopOutlined = Icon;
export const DownOutlined = Icon;
export const DownloadOutlined = Icon;
export const DragOutlined = Icon;
export const EditOutlined = Icon;
export const ExclamationCircleFilled = Icon;
export const ExclamationCircleOutlined = Icon;
export const EyeOutlined = Icon;
export const FileImageOutlined = Icon;
export const FilterOutlined = Icon;
export const FolderOpenOutlined = Icon;
export const FormOutlined = Icon;
export const GatewayOutlined = Icon;
export const GlobalOutlined = Icon;
export const HddOutlined = Icon;
export const HistoryOutlined = Icon;
export const HomeOutlined = Icon;
export const InfoCircleOutlined = Icon;
export const KeyOutlined = Icon;
export const LinkOutlined = Icon;
export const LoadingOutlined = Icon;
export const LockOutlined = Icon;
export const LoginOutlined = Icon;
export const LogoutOutlined = Icon;
export const MailOutlined = Icon;
export const MinusCircleFilled = Icon;
export const MinusCircleOutlined = Icon;
export const MinusOutlined = Icon;
export const MoreOutlined = Icon;
export const NodeIndexOutlined = Icon;
export const PauseCircleOutlined = Icon;
export const PlayCircleOutlined = Icon;
export const PlusOutlined = Icon;
export const PoweroffOutlined = Icon;
export const ProductOutlined = Icon;
export const ProjectOutlined = Icon;
export const QuestionCircleOutlined = Icon;
export const ReadOutlined = Icon;
export const ReloadOutlined = Icon;
export const RightOutlined = Icon;
export const RobotOutlined = Icon;
export const SafetyCertificateOutlined = Icon;
export const SafetyOutlined = Icon;
export const SearchOutlined = Icon;
export const SettingOutlined = Icon;
export const StopOutlined = Icon;
export const TagsOutlined = Icon;
export const TeamOutlined = Icon;
export const UnlockOutlined = Icon;
export const UserAddOutlined = Icon;
export const UserOutlined = Icon;
export const WarningFilled = Icon;
export const WarningOutlined = Icon;

export default Icon;
