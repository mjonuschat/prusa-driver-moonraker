// Package root exists so the module path itself names the driver.
// A composed build blank-imports the module path, and the implementation
// lives in the driver package below it.
package root

import _ "github.com/mjonuschat/prusa-driver-moonraker/driver"
