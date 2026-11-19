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
- Upload is not supported yet.

A Moonraker that requires authentication is not supported yet. Connecting to one fails with `moonraker: authorization required`.

Built on Prusa Connect by Prusa Research a.s. (https://www.prusa3d.com), licensed under OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1. See LICENSE.
