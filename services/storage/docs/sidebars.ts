import type { SidebarsConfig } from '@docusaurus/plugin-content-docs'

const sidebars: SidebarsConfig = {
  storageSidebar: [
    'intro',
    'getting-started',
    {
      type: 'category',
      label: 'Архитектура',
      collapsed: false,
      items: [
        'architecture/overview',
        'architecture/data-model',
        'architecture/attachments',
        'architecture/placement-coherence',
        'architecture/authz',
        'architecture/operations',
      ],
    },
    {
      type: 'category',
      label: 'Установка',
      collapsed: true,
      items: ['install/deploy', 'install/configuration'],
    },
    {
      type: 'category',
      label: 'API',
      collapsed: false,
      items: [
        'api/overview',
        'api/volume',
        'api/snapshot',
        'api/image',
        'api/disk-type',
        'api/internal',
        'api/operations',
        'api/quotas',
      ],
    },
    {
      type: 'category',
      label: 'Дополнительно',
      collapsed: true,
      items: ['advanced/design-decisions', 'advanced/observability'],
    },
    {
      type: 'category',
      label: 'Terraform',
      collapsed: false,
      items: ['terraform/provider', 'terraform/module-storage-set'],
    },
  ],
}

export default sidebars
