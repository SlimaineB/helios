import { readdir, readFile, writeFile, mkdir, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const srcDir = join(root, 'docs');
const outDir = join(root, 'src', 'content', 'docs');
const editBase = 'https://github.com/SlimaineB/helios/edit/main/docs/';

await rm(outDir, { recursive: true, force: true });
await mkdir(outDir, { recursive: true });

const files = (await readdir(srcDir)).filter((file) => file.endsWith('.md')).sort();

for (const file of files) {
  const raw = await readFile(join(srcDir, file), 'utf8');
  const name = file === 'index.md' ? 'overview.md' : file.toLowerCase();
  let body = raw;

  if (!raw.startsWith('---\n')) {
    const heading = raw.match(/^#\s+(.+?)\s*$/m);
    const title = heading ? heading[1] : name.replace(/\.md$/, '');

    if (heading && heading.index === 0) {
      body = raw.slice(heading[0].length).replace(/^\s+/, '');
    }

    body = `---\ntitle: ${JSON.stringify(title)}\neditUrl: ${editBase}${file}\n---\n\n${body}`;
  }

  await writeFile(join(outDir, name), body);
}

console.log(`Synced ${files.length} docs pages into src/content/docs`);
