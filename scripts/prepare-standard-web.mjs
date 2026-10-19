// Private UI carriers belong in the standalone image as well as shared-host
// packages. Copy the existing generated page byte-for-byte; do not fork UI.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
const root=path.resolve(process.argv[2]||'web/dist');
const manifestPath=path.join(root,'asset-manifest.json');
const manifest=JSON.parse(fs.readFileSync(manifestPath,'utf8'));
for(const [source,target] of [['admin/wecom-tags.html','admin/tags.html']]) {
 const bytes=fs.readFileSync(path.join(root,source));
 const metadata=manifest.release_files?.[source];
 const sha256=crypto.createHash('sha256').update(bytes).digest('hex');
 if(!metadata || metadata.sha256!==sha256 || metadata.bytes!==bytes.length) throw new Error('Private UI source integrity mismatch: '+source);
 fs.writeFileSync(path.join(root,target),bytes);
 manifest.release_files[target]={...metadata};
}
fs.writeFileSync(manifestPath,JSON.stringify(manifest,null,2)+'\n');
console.log('Standalone private UI carriers verified and prepared');
