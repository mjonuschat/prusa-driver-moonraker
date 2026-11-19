// Command moonraker-sim runs the fake Moonraker host as a process.
//
// It lets a server built with this driver dial a machine that answers, with no
// printer attached. The core cannot import this module's packages, so the
// machine is offered as a program and not as a library.
//
// The contract with that caller is one line on stdout and nothing else:
//
//	moonraker-sim listening on 127.0.0.1:41235
//
// The address after "listening on " is what the listener actually bound, so a
// caller that passed no -addr reads the ephemeral port from it. The line is
// written and flushed before the first connection can be accepted, and stdout
// carries nothing afterwards, so a caller may read one line and stop reading.
// Anything this program has to say about a failure goes to stderr.
//
// Run it with:
//
//	go run ./cmd/moonraker-sim            # ephemeral loopback port
//	go run ./cmd/moonraker-sim -addr 127.0.0.1:7125
//
// It serves until it is killed. There is no shutdown flag: the caller owns the
// process and ending it is how the machine is switched off.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mjonuschat/prusa-driver-moonraker/driver/moonrakersim"
)

func main() {
	addr := flag.String("addr", "",
		"host:port to bind; empty binds an ephemeral loopback port, which is the usual case")
	hostname := flag.String("hostname", "",
		"what printer.info reports as the Klipper host's name; empty takes the simulator's default")
	flag.Parse()

	// The context bounds the bind and nothing after it. Start says so, and this
	// program has nothing to add: it serves until the caller ends it.
	host, err := moonrakersim.Start(context.Background(), moonrakersim.Options{
		Addr:     *addr,
		Hostname: *hostname,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "moonraker-sim: %v\n", err)
		os.Exit(1)
	}

	// The one line of the contract. It is written straight to an unbuffered
	// os.Stdout, so a caller blocked on a read is released here rather than
	// whenever a buffer happened to fill.
	fmt.Fprintln(os.Stdout, "moonraker-sim listening on "+host.Address())

	// Block for as long as the process lives. The host serves on its own
	// goroutines; there is nothing left for this one to do, and select on no
	// case is how a program says that without burning a core on it.
	select {}
}
