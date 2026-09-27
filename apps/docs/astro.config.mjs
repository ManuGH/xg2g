// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { scalarStarlight } from '@scalar/starlight';

export default defineConfig({
  site: 'https://manugh.github.io',
  base: '/xg2g',
  redirects: {
    '/tutorials/how-to': '/xg2g/how-to/',
    '/tutorials/how-to/readme': '/xg2g/how-to/',
    '/tutorials/reference': '/xg2g/reference/',
    '/tutorials/reference/readme': '/xg2g/reference/',
    '/tutorials/explanation': '/xg2g/explanation/',
    '/tutorials/explanation/readme': '/xg2g/explanation/',
    '/how-to/tutorials': '/xg2g/tutorials/',
    '/how-to/tutorials/readme': '/xg2g/tutorials/',
    '/how-to/reference': '/xg2g/reference/',
    '/how-to/reference/readme': '/xg2g/reference/',
    '/how-to/explanation': '/xg2g/explanation/',
    '/how-to/explanation/readme': '/xg2g/explanation/',
    '/reference/tutorials': '/xg2g/tutorials/',
    '/reference/tutorials/readme': '/xg2g/tutorials/',
    '/reference/how-to': '/xg2g/how-to/',
    '/reference/how-to/readme': '/xg2g/how-to/',
    '/reference/explanation': '/xg2g/explanation/',
    '/reference/explanation/readme': '/xg2g/explanation/',
    '/explanation/tutorials': '/xg2g/tutorials/',
    '/explanation/tutorials/readme': '/xg2g/tutorials/',
    '/explanation/how-to': '/xg2g/how-to/',
    '/explanation/how-to/readme': '/xg2g/how-to/',
    '/explanation/reference': '/xg2g/reference/',
    '/explanation/reference/readme': '/xg2g/reference/',
  },
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
            { label: 'Tutorials', slug: 'tutorials' },
            { label: 'How-To Guides', slug: 'how-to' },
            { label: 'Reference', slug: 'reference' },
            { label: 'Explanation', slug: 'explanation' },
          ],
        },
        {
          label: 'Guides',
          items: [{ autogenerate: { directory: 'guides' } }],
        },
        {
          label: 'Developer Documentation',
          items: [{ autogenerate: { directory: 'dev' } }],
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
          label: 'WebUI Architecture',
          items: [{ autogenerate: { directory: 'webui' } }],
        },
        {
          label: 'Release Notes',
          items: [{ autogenerate: { directory: 'release' } }],
        },
      ],
    }),
  ],
});
