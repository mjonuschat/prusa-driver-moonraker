// The Moonraker dialect as a module of its own, outside the core module.
//
// Moonraker is the API server that sits in front of Klipper on a Voron, a Rat
// Rig and most converted Enders, and this is the connector that speaks to it.
// It used to live at internal/connector/moonraker, which put it inside the
// application module and therefore inside every binary. Nothing about the code
// needed that: its non-test imports are sdk/connectorapi, sdk/event,
// sdk/extension, sdk/fault and one websocket library, which is exactly the
// budget ADR-20260825-n55 rule 2 gives an adapter. So it is out here, admitted to a
// binary by being named in a build configuration (ADR-20260901-7t4) and by nothing
// else.
//
// That matters more for a connector than for a storage provider. A dialect is
// the thing a third party most wants to write and least wants to upstream: a
// machine somebody bought, with an API somebody else published, on a schedule
// neither shares with this project. If the shipped Klipper dialect can live out
// here then so can theirs, and this module is the proof rather than the promise.
//
// This directory carries its own go.mod, so `go build ./...` in the core skips
// it and Go itself refuses an `internal/` import from here. A published adapter
// lives in its author's own repository and is named in a build configuration by
// version; this one is named by path, which is what ADR-20260901-7t4 rule 15 calls
// path mode.
//
// The replace is what a development build does before the sdk is tagged. A
// released adapter requires a published sdk version and carries no replace,
// and the composition module is where sdk resolution lives either way, because
// a replace binds only in the main module.
//
// Unlike the other worked examples this module has third-party requirements,
// and that is the honest case rather than an embarrassment: a dialect talks a
// wire protocol and something has to frame it. gorilla/websocket is the framing
// and testify is the test dependency the cases arrived with. Both are pinned at
// the version the core already carries, so a composed build resolves one copy
// of each and not two. Nothing checked that on the author's behalf — module
// resolution did, because the composition module requires both sides, and an
// adapter that had asked for a different major version would have found out
// there rather than here.
module github.com/mjonuschat/prusa-driver-moonraker

go 1.26.11

require (
	github.com/prusa3d/prusa-connect/sdk v0.0.0
	github.com/gorilla/websocket v1.5.3
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace github.com/prusa3d/prusa-connect/sdk => ./prusa-connect/sdk
