package privileged

import (
	"bufio"
	"log"
	"os"
)

// Handles messages from [privClient] and issues replies.
type privServer struct {
	in  *os.File
	out *os.File
}

func newServer() *privServer {
	return &privServer{
		in:  os.Stdin,
		out: os.Stdout,
	}
}

// Runs the server and blocks forever.
func (s *privServer) run() {
	r := bufio.NewReader(s.in)
	for {
		msg, err := ReadMessage(r)
		if err != nil {
			log.Fatalf("ReadMessage error: %v", err)
		}
		s.handleMessage(msg)
	}
}

func (s *privServer) handleMessage(msg Message) {
	log.Printf("Received %v", msg)
}
