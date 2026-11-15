import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { unified } from '@astrojs/markdown-remark';

const base = '/helios';

/**
 * MkDocs wrote links as `page.md` and `page.md#section`. Astro/Starlight
 * routes are extensionless, lowercased and base-prefixed, so rewrite local
 * Markdown links to the generated routes.
 */
function remarkDocsLinks() {
  return (tree) => {
    const walk = (node) => {
      if (node.type === 'link' && typeof node.url === 'string') {
        const href = node.url;
        if (!/^(https?:|mailto:|#|\/)/.test(href)) {
          const [target, hash] = href.split('#');
          if (target.endsWith('.md')) {
            let slug = target.replace(/^\.\//, '').replace(/\.md$/, '').toLowerCase();
            if (slug === 'index') slug = 'overview';
            node.url = `${base}/${slug}/${hash ? `#${hash}` : ''}`;
          }
        }
      }
      if (Array.isArray(node.children)) node.children.forEach(walk);
    };
    walk(tree);
  };
}

export default defineConfig({
  site: 'https://slimaineb.github.io',
  base,
  trailingSlash: 'always',
  outDir: './site',
  markdown: {
    processor: unified({ remarkPlugins: [remarkDocsLinks] }),
  },
  integrations: [
    starlight({
      title: 'Helios docs',
      disable404Route: true,
      description:
        'GitOps-style drift detection and self-healing for Ansible. Keep every Linux host converged to the playbook in Git.',
      logo: {
        src: './src/assets/logo.svg',
        alt: 'Helios',
      },
      favicon: '/favicon.svg',
      customCss: ['./src/styles/docs.css'],
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/SlimaineB/helios',
        },
      ],
      editLink: {
        baseUrl: 'https://github.com/SlimaineB/helios/edit/main/docs/',
      },
      tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
      sidebar: [
        {
          label: 'Start here',
          items: [
            { label: 'Overview', slug: 'overview' },
            { label: 'Quick start', slug: 'quickstart' },
          ],
        },
        {
          label: 'Install',
          items: [
            { label: 'Hub and agents', slug: 'install' },
            { label: 'Airgap / offline', slug: 'airgap' },
            { label: 'Kubernetes (k3s)', slug: 'k3s' },
          ],
        },
        {
          label: 'Operate',
          items: [
            { label: 'Optional GitOps', slug: 'gitops' },
            { label: 'High availability', slug: 'ha' },
            { label: 'Authentication and RBAC', slug: 'auth' },
            { label: 'Database schema', slug: 'database' },
            { label: 'Releases and packages', slug: 'releases' },
            { label: 'Capacity', slug: 'capacity' },
          ],
        },
        {
          label: 'Develop',
          items: [
            { label: 'Build, demo and tests', slug: 'development' },
            { label: 'Testing', slug: 'testing' },
          ],
        },
      ],
    }),
  ],
});
