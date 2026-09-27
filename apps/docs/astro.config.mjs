// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { scalarStarlight } from '@scalar/starlight';

export default defineConfig({
  site: 'https://manugh.github.io',
  base: '/xg2g',
  integrations: [
    starlight({
      title: 'xg2g',
      logo: {
        src: './src/assets/logo.svg',
      },
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/ManuGH/xg2g' },
      ],
      customCss: ['./src/styles/custom.css'],
      plugins: [
        scalarStarlight({
          pathname: '/api-reference',
          label: 'API Reference',
          title: 'xg2g OpenAPI v3 Reference',
          configuration: {
            url: '/xg2g/openapi.yaml',
          },
        }),
      ],
      sidebar: [
        {
          label: 'Getting Started',
          items: [
            { label: 'Overview & Quickstart', slug: 'guides/getting_started' },
            { label: 'Installation Guide', slug: 'guides/installation' },
            { label: 'Configuration Reference', slug: 'guides/configuration' },
            { label: 'Troubleshooting', slug: 'guides/troubleshooting' },
          ],
        },
        {
          label: 'Diátaxis Quadrants',
          items: [
            { label: 'Tutorials', slug: 'tutorials/readme' },
            { label: 'How-To Guides', slug: 'how-to/readme' },
            { label: 'Reference', slug: 'reference/readme' },
            { label: 'Explanation', slug: 'explanation/readme' },
          ],
        },
        {
          label: 'Guides',
          items: [{ autogenerate: { directory: 'guides' } }],
        },
        {
          label: 'Architecture',
          items: [{ autogenerate: { directory: 'arch' } }],
        },
        {
          label: 'Operations & Runbooks',
          items: [{ autogenerate: { directory: 'ops' } }],
        },
        {
          label: 'Architecture Decision Records (ADR)',
          items: [{ autogenerate: { directory: 'adr' } }],
        },
        {
          label: 'Release Notes',
          items: [{ autogenerate: { directory: 'release' } }],
        },
      ],
    }),
  ],
});
