package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bastian110/herald/internal/client"
	"github.com/bastian110/herald/internal/protocol"
)

func recvCmd(args []string) int {
	fs := flag.NewFlagSet("recv", flag.ContinueOnError)
	sock := fs.String("socket", socketPath(), "herald socket path")
	follow := fs.Bool("follow", false, "stream every message instead of one")
	asJSON := fs.Bool("json", false, "print the raw JSON envelope")
	timeoutS := fs.Int("timeout", 0, "give up after N seconds (0 = no timeout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	emit := func(e protocol.Envelope) error {
		if *asJSON {
			b, err := protocol.Encode(e)
			if err != nil {
				return err
			}
			os.Stdout.Write(b)
		} else {
			fmt.Println(e.Text)
		}
		return nil
	}

	err := client.Recv(*sock, *follow, time.Duration(*timeoutS)*time.Second, emit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "herald recv: %v\n", err)
		return 1
	}
	return 0
}
