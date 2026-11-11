// Command apigen writes the TypeScript HTTP contract for the web client.
//
//	go run ./cmd/apigen ../web/src/shared/api/contract.gen.ts
package main

import (
	"fmt"
	"os"

	"github.com/nic/paper-collect/backend/internal/server"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: apigen <output.ts>")
		os.Exit(2)
	}
	out, err := server.TypeScriptContract()
	if err != nil {
		fmt.Fprintln(os.Stderr, "apigen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "apigen:", err)
		os.Exit(1)
	}
}
