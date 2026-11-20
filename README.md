# prusa-driver-moonraker

A Prusa Connect driver for Klipper and Kalico printers that run Moonraker. A Voron, Rat Rig or converted Ender appears in Prusa Connect as a printer.

## What it does

- **Connects from the server.** The printer needs no settings file and no extra software.
- **Adapts to each printer.** The driver finds the chamber, fans, light and filament sensor by their usual Klipper names and offers only the commands the printer can run. If it misses one, you can tell it which section to use.
- **Handles files and filament.** You can browse and upload files, get a prompt when the filament runs out, and load filament with the printer's own macros.
- **Uses Spoolman for filament information.** Prusa Connect gets the active spool's material, colour and weights.

## Requirements

- A Klipper or Kalico printer with Moonraker, reachable from the Prusa Connect server.
- A Prusa Connect server built with this driver. Each release provides one as a Docker image and with an install script.

## Installation

### Docker

Download `compose.yaml` and `config.docker.yaml` from the [latest release](https://github.com/mjonuschat/prusa-driver-moonraker/releases/latest) into one folder, and start it:

```sh
docker compose up -d
```

Open http://<server>:8080 and install Moonraker under Settings, Extensions.

Back up before you update the image. This is not an official Prusa release, so Prusa Connect's built-in upgrade and its automatic backups are off. Stop the container and copy its volumes:

```sh
docker compose down
for volume in config storage data files; do
  docker run --rm -v "prusa-connect_prusa_connect_${volume}:/data:ro" -v "$PWD:/backup" alpine \
    tar -czf "/backup/${volume}.tgz" -C /data .
done
```

To restore, create the volume again and extract the archive into it with `tar -xzf`.

### Binary

On a Linux host with systemd, as your normal user, not root:

```sh
curl -fsSL https://github.com/mjonuschat/prusa-driver-moonraker/releases/latest/download/install.sh | sh
```

This installs the server and the Moonraker page into `~/prusa-connect` and runs it as the `prusa-connect` service. Open http://<server>:8080 and enter the setup code that `sudo journalctl -u prusa-connect | grep setup_code` shows.

Run the same command again to upgrade.

Back up before you upgrade. This is not an official Prusa release, so Prusa Connect's built-in upgrade and its automatic backups are off. Copy `~/prusa-connect/config` and `~/prusa-connect/storage`.

### Remove

Docker: `docker compose down -v` also deletes the volumes, so back them up first.

Binary:

```sh
sudo systemctl disable --now prusa-connect
sudo rm /etc/systemd/system/prusa-connect.service
sudo systemctl daemon-reload
rm -r ~/prusa-connect
```

## Adding a printer

In Settings, under Extensions, open Moonraker and press Add printer. Enter the printer's address, a name, and optionally a model. A host name or IP address works, with a port if it is not 7125.

## Hardware

The pencil on a printer's row opens its hardware. Each field takes a section name from your Klipper configuration, for example `temperature_sensor Chamber_Top`. An empty field lets the driver choose, and its placeholder shows what it found. `none` turns the part off. If a name matches no section in the printer's configuration, the page warns you after you save.

The driver tries these sections, in order:

| Part | Sections |
|---|---|
| Chamber | `heater_generic` named `chamber`, `heater_chamber`, `chamber_heater` or `bed_fans`, then `temperature_sensor Chamber` |
| Bed fans | `fan_generic bed_fans`, or `heater_generic bed_fans` when that is also the chamber heater |
| Part cooling fan | `fan` |
| Heatbreak fan | `heater_fan hotend_fan` |
| Filament sensor | the first `filament_switch_sensor` |
| Chamber light | an `output_pin`, `led`, `neopixel` or `dotstar` named `chamber_light`, `chamber_led`, `chamber_lights`, `caselight` or `case_light` |
| Chamber fan | a `fan_generic` named `chamber_fan`, `chamber_fans`, `enclosure_fan`, `exhaust_fan`, `filter_fan` or `nevermore` |

## Filament loading

Loading and unloading filament require the macros `LOAD_FILAMENT` and `UNLOAD_FILAMENT`, or `M701` and `M702`.

## Known limitations

- Moonraker must not require authorization, or the connection fails with `moonraker: authorization required`.
- Prusa Connect shows only the first toolhead's spool, and it cannot change the active spool.
- The Moonraker page lists printers added under the connector name `moonraker`.

## Development

```
driver/              the driver, built against prusa-connect/sdk only
driver/moonrakersim  a fake Moonraker for the tests
module/              the Moonraker page, a Svelte bundle
prusa-connect/       the Prusa Connect core, a git submodule
```

```sh
make test           # go test -race ./...
make lint           # golangci-lint run ./...
make build          # build bin/server-moonraker
make sim            # run the fake Moonraker on a local port
make update-core    # move the submodule to its latest commit
make test-scripts   # tests of the scripts and workflows, and shellcheck
```

```sh
(cd module/ui && pnpm run dev)
```

## License

Built on Prusa Connect by Prusa Research a.s. (https://www.prusa3d.com), licensed under OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1. See `LICENSE`.
