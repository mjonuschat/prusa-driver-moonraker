# prusa-driver-moonraker

A Prusa Connect printer driver for Klipper machines behind Moonraker.

## Verbs

- `start_print` takes a path relative to Moonraker's `gcodes` root.
- `send_gcode` takes G, M and T words only.
- `force_gcode` is an emergency stop only (M112).

A Moonraker that requires authentication is not supported yet. Connecting to one fails with `moonraker: authorization required`.

Built on Prusa Connect by Prusa Research a.s. (https://www.prusa3d.com), licensed under OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1. See LICENSE.
