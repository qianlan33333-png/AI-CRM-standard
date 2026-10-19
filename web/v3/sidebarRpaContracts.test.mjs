import {execFileSync} from 'node:child_process';

// The frontend lane owns npm preparation in both clean local and stage runners.
// Generate the actual production Host/overlay before exercising its consumers.
execFileSync('npm',['run','build','--silent'],{stdio:'inherit'});
execFileSync(process.execPath,['scripts/build-v3-host-adapters.mjs'],{stdio:'inherit'});
await import('./sidebarGroupNavigation.test.mjs');
await import('./sidebarGroupBindings.test.mjs');
execFileSync('npm',['run','typecheck','--silent'],{stdio:'inherit'});
