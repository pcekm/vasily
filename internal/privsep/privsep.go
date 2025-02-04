/*
Package privsep contains code for running some code as root.

This works as a client/server, where the main part of the program is the client,
and the privileged part runs in a separate process as a server. The two are
connected with pipes.

# Rationale

This is necessary because directly sending and receiving ICMP messages requires
root privileges on most systems. There are two notable exceptions to this:

  - Darwin / MacOS: Allows unprivileged pings with enough flexibility that both
    pings and traceroutes can be made without any special privileges.

  - Linux: Allows privileged pings, but only with a specific kernel setting.
    Unfortunately, even with that setting, Go's x/net libraries don't work
    for traceroutes. Receiving ICMP time exceeded messages requires the
    IP_RECVERR socket option, and a specific call to the recvfrom syscall with
    the MSG_ERRQUEUE flag. Neither can be done with x/net/icmp.

A frequent, simpler approach is to open the raw socket and then immediately drop
privileges. However, since this program is interactive, things are more
complicated. As long as new sockets need be opened, it's necessary to maintain
privileges. Privilege separation is the next best thing.

# Rules

The rules for this module are:

  - Keep this package as simple and easy to read as possible.
  - [Postel's law] does not apply. (Also known as the robustness principle.)
    This package should be as finicky as possible, and it should [os.Exit] at
    the first sign of malformed input.
  - Call [Initialize] in main before _everything_ else. It should literally be
    the first line.
  - No 3rd party packages imported here. The amount of scrutiny for the standard
    library is far higher than it is for most 3rd party code. (Including,
    unfortunately, this package, but there's no getting around that one.)
  - No [unsafe] (it should go without saying)

# Protocol

The communication between the unprivileged client and privileged server is a
simple message passing protocol. The client and server send messages to one
another, and those messages contain requests or replies. Each message consists
of a single byte message type, a single byte arg count, and zero or more args.

Messages are formatted as:

	<type><num_args>{<arg>}*

Each arg is a variable-length string with an 8-bit length prefix:

	<len>{<char>}*

The maximum message length is:

	2 + 255 * (1 + 255) = 65282

backend.Packet is formatted as:

	<packet-type><seq><payload-len><payload>

	<packet-type>: 1 byte
	<seq>:         2 byte big endian sequence number
	<payload-len>: 1 byte
	<payload>:     payload-len bytes

Any unrecognized or improperly-formatted messages to the privileged server will
cause it to immediately exit. The unprivileged client can be more forgiving.

[Postel's law]: https://en.wikipedia.org/wiki/Robustness_principle
*/
package privsep

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"syscall"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/privsep/client"
)

const (
	startPrivFlag = "[privileged]"
)

func Initialize() func() {
	if !usePrivsep() {
		return func() {}
	}

	if len(os.Args) == 2 && os.Args[1] == startPrivFlag {
		log.Printf("Starting privileged server.")
		server := newServer()
		server.run()
		os.Exit(0)
	}

	if err := dropPrivileges(); err != nil {
		log.Fatalf("Error dropping privileges: %v", err)
	}

	me, err := os.Executable()
	if err != nil {
		log.Fatalf("Can't determine self executable: %v", err)
	}
	cmd := exec.Command(me, startPrivFlag)
	cmd.Args[0] = "vasily"
	cmd.Env = []string{}

	clientIn, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Error creating pipe: %v", err)
	}
	clientOut, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalf("Error creating pipe: %v", err)
	}
	clientErr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatalf("Error creating pipe: %v", err)
	}
	waited := make(chan any)
	go stderrLogger(clientErr)

	if err := cmd.Start(); err != nil {
		log.Fatalf("Error running privileged server: %v", err)
	}
	go watchdog(cmd, waited)

	client := client.New(clientIn, clientOut)
	backend.UsePrivsep(client)

	return shutdownFunc(cmd, client, waited)
}

func stderrLogger(r io.Reader) {
	rb := bufio.NewReader(r)
	for {
		line, err := rb.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("ReadString error: %v", err)
			}
			return
		}
		log.Printf("privsep: %v", line)
	}
}

func watchdog(cmd *exec.Cmd, waited chan<- any) {
	defer close(waited)
	if err := cmd.Wait(); err != nil {
		log.Fatalf("Privsep server exited with error: %v", err)
	}
}

func shutdownFunc(cmd *exec.Cmd, privsepClient *client.Client, waited <-chan any) func() {
	return func() {
		if err := privsepClient.Shutdown(); err != nil {
			log.Printf("Error shutting down privsep: %v", err)
			if err := cmd.Process.Kill(); err != nil {
				log.Printf("Error killing privsep: %v", err)
			}
		}
		if err := privsepClient.Close(); err != nil {
			log.Printf("Error closing privsep client: %v", err)
		}
		<-waited
	}
}

func dropPrivileges() error {
	uid := syscall.Getuid()
	euid := syscall.Geteuid()
	if uid == euid {
		// This means either we were run as root, or without setuid. We can
		// continue for now, but without privileges something will likely break
		// later.
		log.Printf("Privilege drop impossible: uid (%d) = euid (%d)", uid, euid)
		return nil
	}

	// Give up privileges.
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("setuid: %v", err)
	}

	// Verify privileges have been given up.
	if syscall.Getuid() != syscall.Geteuid() {
		return fmt.Errorf("failed to drop privileges: uid (%d) != euid (%d)", syscall.Getuid(), syscall.Geteuid())
	}

	// Try to regain root and return an error if that was possible.
	if err := syscall.Seteuid(0); err == nil {
		return fmt.Errorf("unexpectedly able to regain root")
	}

	// One last check to make sure privileges are truly gone.
	if syscall.Getuid() != syscall.Geteuid() {
		return fmt.Errorf("failed to drop privileges: uid (%d) != euid (%d)", syscall.Getuid(), syscall.Geteuid())
	}

	return nil
}
