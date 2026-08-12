// Типы полей — для колонки «Тип» в таблицах запроса/ответа.
export const TYPES = {
  string: 'string',
  bool: 'bool',
  int32: 'int32',
  int64: 'int64',
  // Необязательное число: отсутствие значения представимо ОТДЕЛЬНО от нуля.
  // Отдельный тип заведён потому, что на месте `int64` ноль читается как
  // утверждение о величине, а нам нужно уметь не утверждать ничего.
  int64Optional: 'optional int64',
  double: 'double',
  mapStringString: 'map<string,string>',
  stringArray: 'string[]',
  timestamp: 'google.protobuf.Timestamp',
  fieldMask: 'google.protobuf.FieldMask',
  operation: 'operation.Operation',
  reference: 'Reference[]',
  attachments: 'VolumeAttachment[]',
  attachMode: 'VolumeAttachment.Mode',
  volumeStatus: 'Volume.Status',
  snapshotStatus: 'Snapshot.Status',
  imageStatus: 'Image.Status',
  imageFormat: 'Image.Format',
  statusReason: 'StatusReason',
  performanceTier: 'DiskType.PerformanceTier',
  lifecycle: 'DiskType.Lifecycle',
  capabilities: 'DiskType.Capabilities',
  sizeLimits: 'DiskType.SizeLimits',
  backendKind: 'StorageBackend.BackendKind',
  backendStatus: 'StorageBackend.Status',
  bindingStatus: 'DiskTypeBinding.Status',
  backendLocator: 'BackendLocator',
  bindingCapabilities: 'BindingCapabilities',
  bindingQos: 'BindingQoS',
  observedState: 'ImageInternal.ObservedState',
} as const

export type TypeKey = keyof typeof TYPES
