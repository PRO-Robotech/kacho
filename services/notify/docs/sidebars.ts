import type { SidebarsConfig } from '@docusaurus/plugin-content-docs'

const sidebars: SidebarsConfig = {
  notifySidebar: [
    'intro',
    {
      type: 'category',
      label: 'Установка',
      collapsed: false,
      items: ['install/dns-requirements'],
    },
  ],
}

export default sidebars
