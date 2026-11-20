# Comparison with the xBuddy connector

Last updated: 2026-11-20

This compares the commands this driver offers with the commands the xBuddy
connector (Prusa's own printers) offers. It was made by reading the code: the
core at commit `2e8c62bd` and Prusa-Firmware-Buddy at `b1043d68`. Only
`set_idle` was also tried on a real Klipper machine. Nothing was tested on a
Prusa printer.

## Supported

| Area | Commands |
|---|---|
| State | `send_state_info`, `send_info`, `send_file_info` |
| Print control | `start_print`, `pause_print`, `resume_print`, `stop_print`, `reset`, `reset_printer`, `set_idle` |
| Temperatures and tuning | nozzle (per tool), bed and chamber targets, flow, speed |
| Motion and filament | home, move, Z move, extruder move, disable steppers, load and unload filament, mesh bed leveling |
| G-code, dialogs, objects | `send_gcode`, `force_gcode` (raw G-code), dialog actions, cancel and uncancel object |
| Chamber hardware | light, fan, target temperature |
| Files | browse, delete file, create and delete folder, upload (`file_upload`) |

## Not supported

| Area | Commands | Reason |
|---|---|---|
| Add-on power | `set_addon_power` | The command names no device, and the core has no add-on power state to show. A switch would work but Connect could not display its state. |
| Printer-ready | `set_printer_ready`, `cancel_printer_ready` | READY is a flag set by a person or by Connect. The core treats it the same as IDLE, and nothing on a Klipper machine would set it. |
| Maintenance | `upgrade`, `flash`, `beep` | Not built. `upgrade` could use Moonraker's update manager, `flash` could use aldis, and `beep` could send `M300`. |
| Tool flags | `set_tool_nozzle_diameter`, `set_tool_hardened`, `set_tool_high_flow` | Klipper has nothing to set. |
| Enclosure | `set_enclosure_enabled`, `_postprint`, `_printing_filtration`, `_postprint_filtration_time` | Prusa enclosure hardware only. |

## Not needed

| Commands | Why this driver does not need them |
|---|---|
| `start_inline_download`, `start_connect_download`, `start_encrypted_download` | Only the xBuddy connector's own transfer code uses them. The core sends files through `file_upload`, which this driver supports. |
| `stop_transfer` | The core sends it when a transfer is abandoned. Without it the core logs one line and carries on. This driver keeps no transfer slot to release. |
| `send_job_info`, `send_transfer_info`, `get_status` | Only the xBuddy connector uses them, or they appear as an example in the test documentation. This driver reads state from Moonraker directly. |
| `set_token`, `set_hostname` | Setup commands for Prusa firmware. The core never sends them. |
| `z_power_move` | Raises the Z motor current. Specific to Prusa hardware. |
| `recursive_delete` (feature) | The core does not read it. See "Deleting folders" below. |
| `state_stream` (feature) | Only the example echo adapter uses it. |

## Differences in behavior

1. `delete_folder` asks Moonraker to delete the folder without `force`, so
   Moonraker refuses a folder that still holds files. A Prusa printer deletes a
   non-empty folder. Sending `force=true` would match it, but the delete would
   then destroy files.
2. Like a Prusa printer, this driver keeps showing FINISHED or STOPPED until
   `set_idle` clears it. Deleting the printed file does not clear it.
3. `set_idle` is accepted only in FINISHED and STOPPED, because the reset also
   stops a print that is still running.
