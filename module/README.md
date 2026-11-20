# Moonraker

A Prusa Connect module that is one browser bundle and nothing else: the
host verifies it against a digest, serves it, and never runs it.
`make new-module` wrote this skeleton.

Its namespace is `com.github.aux.mjonuschat.prusa-driver-moonraker`, derived by the host from the repository
address `github.com/mjonuschat/prusa-driver-moonraker`. You never write the namespace: every address the module
answers on is built from it.

| path | purpose |
| --- | --- |
| `module.yaml` | the manifest: the kind and the version, and the `ui` block with the ceiling this bundle may register into |
| `install.sh` | build, install and see it. The one command below |
| `CHANGELOG.md` | what an operator sees change when they upgrade this module |
| `.vscode/extensions.json` | the editor extensions for the languages in here |
| `ui/src/index.ts` | the bundle's entry: it receives the host object and registers what the manifest declares |
| `ui/src/settings.svelte` | `machines`: a tab on the module's page under Settings, Extensions |
| `ui/src/styles.css` | this module's own Tailwind. Four lines, and only one emits |
| `ui/src/locales/en.json` | every text this module draws. No component writes an English string |

## Installation

```bash
sh install.sh
```

That builds the bundle, and installs the release with
`prusa-connect module add` into the demo's data, under `bin/machines-demo`
in the checkout. `TARGET=app sh install.sh` installs it into the real app's
data, `bin/machines-app`, instead. What the tools print goes to `install.log`
there, and a good run prints one line saying where the module is installed.

To see it, run this in the checkout. `-dir` is this folder, `../machines` when
it sits beside the checkout:

```bash
go run ./cmd/new-module -start demo -dir ../machines
```

It starts the demo on a free port with this module installed, and this
module's dev server in front of it. It prints the address to open and the
account to sign in with. `-start app` starts the real app instead, which asks
for an account on its first run. Ctrl+C stops everything it started.

Paste what the command printed on your machine under this paragraph. A README
that quotes real output is one a reader can check; one that describes the output
is one they have to trust.

Run `sh install.sh` again after a change to `module.yaml`.
`module add` records the bytes it installed and the host serves nothing it
cannot verify against that record, so a rebuilt release is a new install. A
core that is already running read its declarations when it started, so it is
restarted too.

## Iterating

`-start` runs the module's own dev server, `pnpm dev` in `ui/`. Open the
address it prints, not the core's. The page is the core's app and this module
is served from `src/`, so an edit to a component shows up without a reload.
Run by hand, `pnpm dev` reads the core's address from `PCL_CORE_URL`,
`http://localhost:8080` by default.

Install again after changing `module.yaml`: the core reads what a module
contributes when it is installed.

## What the pieces are

The bundle's build writes `ui/dist`, and that directory is already the release
layout: `module.yaml` at its root and the entry at `ui/index.js`. `-from ui/dist` is what the install
reads. A `checksums.txt` is optional and verified when it is there; a release
you just built has nothing to prove against.

```bash
prusa-connect module add github.com/mjonuschat/prusa-driver-moonraker@0.1.0 -from ui/dist
prusa-connect module list
prusa-connect module remove github.com/mjonuschat/prusa-driver-moonraker
```

Without `-approve` the declaration is written switched off, and the command
prints what the module asked for so an operator can read it first.

## The ceiling

`contributes` in `module.yaml` is the whole of what this bundle may register.
A `host.register` call the block does not declare throws, and the host drops
this module whole. The generator wrote both sides from one answer:

- `settings: machines` ← `host.register.settings('machines', Settings)`

Add one to the manifest and the other to the entry, or neither.

## Styling

`ui/src/styles.css` carries four directives and only the third emits anything.
They let this module name any Tailwind utility the library's theme defines,
without shipping Tailwind's preflight into the host page. The page behind them
is `styling-a-module` in the core's documentation.

## Texts

Every text a person reads is a key in `ui/src/locales/en.json`, read with
`host.t('<key>')`. The host merges that file under this module's namespace, so
a key here may be one the core already names. Copy the file to `<tag>.json` to
translate it.

## Troubleshooting

| what you see | what it means |
| --- | --- |
| `module add` refuses the release | `ui/dist` is not the release layout. `module.yaml` sits at its root and the entry at `ui/index.js` |
| the module is listed and switched off | `module add` ran without `-approve`. Read what it printed, then switch the instance on |
| the sidebar entry is missing | the app refused the bundle. Open the browser console: a registration `contributes` does not declare is refused there by name |
| a screen shows a key instead of a sentence | the key is missing from `ui/src/locales/en.json` |
| a change does not show | the install was not run again. Every change is a new install |
