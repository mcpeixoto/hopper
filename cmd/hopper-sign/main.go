// Command hopper-sign signs and verifies release artifacts with Ed25519. It is a
// build/release helper, not part of the runtime.
//
//	hopper-sign keygen                     # print a new public/private keypair
//	HOPPER_SIGNING_KEY=<priv> hopper-sign sign FILE   # write FILE.sig
//	hopper-sign verify PUBHEX FILE FILE.sig           # verify (exit 0 = ok)
package main

import (
	"fmt"
	"os"

	"github.com/mcpeixoto/hopper/internal/sign"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keygen":
		pub, priv, err := sign.GenerateKey()
		check(err)
		fmt.Printf("public  (embed / commit):     %s\n", pub)
		fmt.Printf("private (CI secret HOPPER_SIGNING_KEY): %s\n", priv)

	case "sign":
		if len(os.Args) != 3 {
			usage()
		}
		key := os.Getenv("HOPPER_SIGNING_KEY")
		if key == "" {
			fatal("HOPPER_SIGNING_KEY not set")
		}
		data, err := os.ReadFile(os.Args[2])
		check(err)
		sig, err := sign.Sign(key, data)
		check(err)
		out := os.Args[2] + ".sig"
		check(os.WriteFile(out, []byte(sig), 0o644))
		fmt.Printf("wrote %s\n", out)

	case "verify":
		if len(os.Args) != 5 {
			usage()
		}
		data, err := os.ReadFile(os.Args[3])
		check(err)
		sig, err := os.ReadFile(os.Args[4])
		check(err)
		if !sign.Verify(os.Args[2], data, string(sig)) {
			fatal("signature INVALID")
		}
		fmt.Println("signature OK")

	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hopper-sign keygen | sign FILE | verify PUBHEX FILE SIGFILE")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "hopper-sign: "+msg)
	os.Exit(1)
}
