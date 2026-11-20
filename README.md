# prusa-driver-moonraker

A Prusa Connect printer driver for Klipper machines behind Moonraker.

## Verbs

- `start_print` takes a path as `List` returns it, such as `gcodes/models/part.gcode`.
- `send_gcode` takes G, M and T words only.
- `force_gcode` is an emergency stop only (M112).

## Files

- One storage, `gcodes`, is shown. There is no other root.
- Listing includes a preview of each file.
- Folders can be created.
- Files and folders can be deleted.
- Changes made in Mainsail or Fluidd appear as file changes.
- Files can be sent to the printer from Prusa Connect.

## Telemetry

The driver reads Klipper objects by name and publishes them under fixed paths in Connect's state tree. Each role has default object names, matched case-insensitively, and an option in the driver's options that names another object instead.

| Role | Default object names | State paths | Override option |
| --- | --- | --- | --- |
| Chamber | `heater_generic chamber`, `heater_generic heater_chamber`, `heater_generic chamber_heater`, then `heater_generic bed_fans`, then `temperature_sensor Chamber` | `/temperature/chamber`, `/temperature/chamber_target` (heaters only) | `chamber_object` |
| Bed fans | `fan_generic bed_fans`, else `heater_generic bed_fans` when that is the chamber heater | `/fan/bed/speed`, `/fan/bed/rpm` | `bed_fans_object` |
| Part fan | `fan` | `/fan/print/speed`, `/fan/print/rpm` | `part_fan_object` |
| Heatbreak fan | `heater_fan hotend_fan` | `/fan/heatbreak/speed`, `/fan/heatbreak/rpm` | `heatbreak_fan_object` |
| Filament sensor | the first `filament_switch_sensor` | none (a runout raises a dialog) | `filament_object` |
| Layers | `print_stats.info` (current and total layer) | `/job/layer`, `/job/layer_count` | none |
| Extra sensors | none | `/sensors/<name>/temperature` | `extra_sensors` |
| Extra fans | none | `/fans/<name>/speed`, `/fans/<name>/rpm` | `extra_fans` |

`<name>` is the object name without its Klipper type, for example `chamber_top` for `temperature_sensor chamber_top`.

For the chamber, the first heater from the list wins, so a machine with both a `chamber` heater and a `bed_fans` heater uses the `chamber` heater and reports `bed_fans` as the bed fans. A `bed_fans` heater is used as the bed fans only when it is also the chamber heater and no `fan_generic bed_fans` exists. An object is never both the chamber and the bed fans. A heater that stands in for a fan reports its `power` as the fan speed.

Options take an object name, either in full (`heater_generic chamber`) or without its type when only one object has that name. `none` turns a role off. A name that matches no object is ignored and logged. `extra_sensors` and `extra_fans` take a comma-separated list.

A fan `rpm` is published only when Klipper reports one. The rpm of the part and heatbreak fans is also mirrored to the active head.

A filament runout raises a dialog only while a print is running.

The chamber verb sets the target of the resolved chamber heater only. On a machine whose only chamber object is `heater_generic bed_fans`, that is the heater it sets.

Temperature ceilings are bed 250, chamber 250 and nozzle 500, each narrowed to the heater's `max_temp`.

Not done yet: several Moonraker instances, publishing every sensor automatically, and fan speed setters.

Limit: an extra sensor or fan removed from the options disappears from Connect's tree on the next redial. It does not disappear after a driver restart, because the driver remembers the names of its extras only in memory.

A Moonraker that requires authentication is not supported yet. Connecting to one fails with `moonraker: authorization required`.

Built on Prusa Connect by Prusa Research a.s. (https://www.prusa3d.com), licensed under OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1. See LICENSE.
