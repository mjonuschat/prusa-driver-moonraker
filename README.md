# prusa-driver-moonraker

A Prusa Connect printer driver for Klipper machines behind Moonraker.

## Verbs

- `start_print` takes a path as `List` returns it, such as `gcodes/models/part.gcode`.
- `send_gcode` takes G, M and T words only.
- `force_gcode` is an emergency stop only (M112).

## Motion

Offered when the machine lists the `toolhead` object. Each verb is one `printer.gcode.script` call.

| Verb | Gcode |
| --- | --- |
| `core.command.move` (`x`, `y`, `feedrate`) | `SAVE_GCODE_STATE NAME=CONNECT_JOG`, `G91`, `G1 X.. Y.. F..`, `RESTORE_GCODE_STATE NAME=CONNECT_JOG` |
| `core.command.move_z` (`distance`, `feedrate`) | the same with `G1 Z.. F..` |
| `core.command.home` (`axis`, optional) | `G28`, or `G28 X Y Z` for the axes named |
| `core.command.disable_steppers` | `M84` |

- Feedrate is in mm/min. Its ceiling is the machine's `max_velocity` (`move`) or `max_z_velocity` (`move_z`) times 60. When the configuration cannot be read the ceilings are 6000 and 600.
- `x`, `y` and `distance` are relative steps in mm, bounded by plus or minus the axis span (plus or minus 1000 when the span is unknown).
- Motion is refused while PRINTING and in ERROR, and accepted in IDLE, PAUSED, FINISHED and STOPPED. It is refused until the first state is known.
- Klipper's own refusals are returned with its message. After a refused jog the driver sends `RESTORE_GCODE_STATE NAME=CONNECT_JOG` so the machine does not stay in relative mode. The state is never `PAUSE_STATE`.
- A jog and its restore run as one unit. A second motion verb waits for the first.
- Z+ is Klipper's Z+. The driver sends `distance` as given and never inverts it.

## Level bed

`core.command.mesh_bed_leveling` is offered when the machine lists `z_tilt` or `quad_gantry_level`. It takes no arguments and runs one `printer.gcode.script` call.

| Object listed | Gcode |
| --- | --- |
| `z_tilt` | `Z_TILT_ADJUST` |
| `quad_gantry_level` | `QUAD_GANTRY_LEVEL` |

- A machine that lists both is levelled with `Z_TILT_ADJUST`, and the driver logs a warning when it connects.
- Accepted in IDLE, FINISHED and STOPPED. Refused in PRINTING, PAUSED and ERROR, and until the first state is known.
- Klipper's own refusals, such as an unhomed machine, are returned with its message. The driver does not check homing.
- The driver waits up to 5 minutes for the answer, and `home` up to 60 seconds, where other calls wait for the timeout option (10 seconds by default). A timeout longer than that wait is kept.
- It takes the same lock as the motion verbs, so it does not interleave with a jog.

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

## Spoolman

When Moonraker runs the `spoolman` component, the driver publishes the active spool under head 0, so Connect shows the loaded material instead of "No filament". The driver asks Moonraker, never Spoolman, and has no option for it.

| Path member | Value |
| --- | --- |
| `MaterialAbbreviation`, `MaterialType` | the spool's `filament.material` |
| `MaterialColor` | `#` plus `filament.color_hex`, only when it is a valid colour; a spool with several colours has none |
| `MaterialFullWeight` | `initial_weight`, in grams |
| `ConsumedWeight` | `used_weight`, in grams |

A value Spoolman does not report, or reports in a form the driver cannot use, has its path removed and is never written as zero or empty. No active spool, a failed or unanswered call, and a Spoolman that is down also remove all five paths, so a stale spool is not left behind.

The spool is read at three moments:

1. At session start.
2. When the active spool changes (`notify_active_spool_set`).
3. When Moonraker's link to Spoolman comes back (`notify_spoolman_status_changed`).

It is also read after a print ends. Moonraker reports extrusion to Spoolman every `sync_rate` seconds (read from `server.config`, 5 by default), so the driver waits one `sync_rate` plus one second. It then asks `server.spoolman.status` until `pending_reports` is empty, for at most 10 seconds, and reads the spool. A print ends when `/state` moves from PRINTING or PAUSED to FINISHED, STOPPED or ERROR. A newer trigger cancels a pending refresh.

A machine whose Moonraker has no `spoolman` section, or answers method-not-found, gets no Spoolman call for the rest of the session.

Limits: head 0 only, one spool, no live weight during a print (the weight updates after the print), and no writes to Spoolman, including setting the active spool.

Built on Prusa Connect by Prusa Research a.s. (https://www.prusa3d.com), licensed under OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1. See LICENSE.
