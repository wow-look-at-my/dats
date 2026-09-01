// Builds a sanitized dats argv from the action's typed inputs and runs it.
// The action's only surface is `tests` (files and directories), so nothing a
// caller types can become a flag or a --no-sandbox: every entry is validated
// to be a relative path, and a directory is expanded to its top-level *.dats
// files before anything reaches the binary.

const bin = process.env.DATS_BIN;
if (!bin) throw new Error('DATS_BIN is not set');

const workdir = inputs['working-directory'] || '.';
process.chdir(workdir);

const raw = (inputs.tests ?? '').split(/\s+/).filter(Boolean);
if (raw.length === 0) throw new Error('tests is required: list .dats files and/or directories');

const tests: string[] = [];
for (const entry of raw) {
	if (entry.startsWith('-')) {
		throw new Error(`tests entry "${entry}" looks like a flag; only .dats files and directories are allowed`);
	}
	if (path.isAbsolute(entry)) {
		throw new Error(`tests entry "${entry}" must be relative to working-directory`);
	}
	if (entry.split(/[\\/]/).includes('..')) {
		throw new Error(`tests entry "${entry}" must not contain ".."`);
	}
	if (entry.endsWith('.dats')) {
		tests.push(entry);
		continue;
	}
	const files = fs
		.readdirSync(entry)
		.filter((f) => f.endsWith('.dats'))
		.map((f) => path.join(entry, f));
	if (files.length === 0) throw new Error(`no .dats files in directory "${entry}"`);
	tests.push(...files);
}

// The download is a fat APE, and each host starts one differently. NT finds an
// executable by its extension, so the file needs an .exe name. Darwin refuses
// the file on execve, so a shell must read the header and exec the payload.
// Linux starts the file as it stands. Every form passes argv, never a string a
// shell splits again.
// -v always. It makes a red leg name the test that failed and print its output,
// instead of only reporting that something failed, and a caller who has to ask
// for that asks after the run they needed it on. A root flag, so it precedes
// the subcommand.
const argv = ['-v', 'test', ...tests];
const windows = process.platform === 'win32';

// NT can host no sandbox backend, so install-wsl-backend.sh puts bubblewrap in
// WSL and names the distro here. The download is a fat APE, so the same file
// runs its Linux payload in there and dats sees an ordinary Linux host with a
// backend. wslpath translates the two paths WSL cannot guess.
const distro = windows ? (process.env.DATS_WSL_DISTRO ?? '') : '';
if (distro) {
	// Every wsl.exe call reads empty stdin. An open one attaches an interactive
	// session and waits, and the step then prints nothing until the job's own
	// timeout kills it. The run is bounded inside Linux, where a timeout can
	// still name what it stopped.
	const seconds = Number(process.env.DATS_WSL_TIMEOUT_SECONDS ?? '900');
	// Mapped here rather than by wslpath inside the distribution. wsl.exe joins
	// its arguments into one command line that the Linux side parses again, and
	// that parse eats the backslashes, so `D:\a\dats` arrives as `D:adats`.
	const toWsl = (p: string) => {
		const drive = /^([A-Za-z]):[\\/](.*)$/.exec(p);
		if (!drive) throw new Error(`cannot map "${p}" into WSL: expected a drive-letter path`);
		return `/mnt/${drive[1].toLowerCase()}/${drive[2].replace(/\\/g, '/')}`;
	};
	const cwd = toWsl(process.cwd());
	const linuxBin = toWsl(bin);
	// PATH is set rather than inherited. wsl.exe resolves the command it is given
	// through its own launcher, but the process it starts inherits a PATH without
	// /usr/bin, so dats looked for bwrap and found nothing while bwrap sat
	// installed. Dropping the Windows entries also stops the probe reaching
	// docker.exe on the host, whose daemon serves windows containers.
	// wsl-run.sh does the Linux-side work: it clears the PE binfmt handler, sets
	// PATH, and lets a shell start the APE. It is a checked-in file rather than a
	// script passed inline, because wsl.exe joins its argv into one command line
	// the Linux side parses again, which strips the quoting an inline script
	// needs. Only plain arguments cross this boundary.
	const actionPath = process.env.DATS_ACTION_PATH;
	if (!actionPath) throw new Error('DATS_ACTION_PATH is not set');
	const runner = toWsl(path.join(actionPath, '.github', 'scripts', 'wsl-run.sh'));
	const res = await $`wsl.exe -d ${distro} -u root --cd ${cwd} -- sh ${runner} ${String(seconds)} ${linuxBin} ${argv}`
		.input('')
		.nothrow();
	if (res.exitCode === 124) core.setFailed(`dats did not finish within ${seconds}s inside ${distro}`);
	else if (res.exitCode !== 0) core.setFailed(`dats exited ${res.exitCode}`);
} else {
	// NT finds an executable by its extension. Darwin refuses the APE on execve,
	// so a shell must read the header and exec the payload. Linux starts the file
	// as it stands. Every form passes argv, never a string a shell splits again.
	const exe = windows && !bin.endsWith('.exe') ? `${bin}.exe` : bin;
	if (exe !== bin) fs.copyFileSync(bin, exe);
	const res = await (windows ? $`${exe} ${argv}` : $`bash -c ${'"$0" "$@"'} ${exe} ${argv}`).nothrow();
	if (res.exitCode !== 0) core.setFailed(`dats exited ${res.exitCode}`);
}
