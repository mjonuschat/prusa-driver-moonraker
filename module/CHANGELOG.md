# Changelog

What an operator sees change when they upgrade this module, newest release
first. `version:` in `module.yaml` is the newest heading below.

Write the entry in the same change as the code. One bullet per behaviour a
person outside this repository can observe: a route, a setting, a screen, a
refusal. Name the route and the setting key, never a function or a file.

## 0.1.1 - unreleased

### Added

- Moonraker: the endpoint option `api_key`, also under Advanced when adding a printer, for a Moonraker that requires authorization. The key is sent as the `X-Api-Key` header on the websocket and on thumbnail and upload requests. A refused connection now says whether to set `api_key` or that Moonraker refused the one set.

## 0.1.0 - 2026-11-20

### Added

- Moonraker: `core.command.set_idle` ("Exit idle" on the printer's page). After a print has finished or been stopped, it clears the result and the machine reads as idle again. It is refused while a print is running or paused, and it needs the machine's `virtual_sdcard` section.

## 0.1.0-rc.1 - 2026-11-20

### Added

- Moonraker: a tab on the module's page under Settings, Extensions.
