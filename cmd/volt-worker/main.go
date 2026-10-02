// Command volt-worker serves the IDE worker protocol (see package worker)
// on stdin/stdout.
//
// Usage: volt-worker [devices-dir]. The optional argument is the device
// library (otherwise $VOLT_DEVICES, then ~/.volt/devices).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/PhilemonChiro/volt-compiler"
	"github.com/PhilemonChiro/volt-compiler/worker"
)

func main() {
	if len(os.Args) > 1 {
		compiler.DevicesDir, _ = filepath.Abs(os.Args[1])
	}
	if err := worker.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "volt-worker:", err)
	}
}
