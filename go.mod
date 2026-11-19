// A printer driver for Klipper machines behind Moonraker, the API server of
// Voron, Rat Rig and most converted Enders.
//
// The driver is a module of its own, outside the Prusa Connect core, which is
// the prusa-connect/ submodule. Its non-test imports are sdk/connectorapi,
// sdk/event, sdk/extension, sdk/fault and one websocket library. A server
// binary gets the driver by naming it in build.yaml, and by nothing else.
//
// The replace points at the sdk directory of the submodule, because the sdk has
// no published version. Once it has one, replace the line with a require at that
// version. A replace binds only in the main module, and compose-build copies
// the core's own replaces into the module it generates.
//
// gorilla/websocket frames the wire and testify is the test dependency. Both are
// pinned at the version the core carries, so a composed build resolves one copy
// of each.
module github.com/mjonuschat/prusa-driver-moonraker

go 1.26.11

require (
	github.com/prusa3d/prusa-connect/sdk v0.0.0
	github.com/gorilla/websocket v1.5.3
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace github.com/prusa3d/prusa-connect/sdk => ./prusa-connect/sdk
