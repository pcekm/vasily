// TODO: This belongs in a separate package since it doesn't run as root.

package privileged

import (
	"io"
	"os/exec"
)

type Client struct {
	privCmd *exec.Cmd
	in      io.ReadCloser
	out     io.WriteCloser
}

func newClient(privCmd *exec.Cmd, in io.ReadCloser, out io.WriteCloser) *Client {
	return &Client{
		privCmd: privCmd,
		in:      in,
		out:     out,
	}
}

func (c *Client) Close() error {
	// TODO: Nicer exit? This may not even work. Since it's root and we're not.
	c.privCmd.Process.Kill()
	return c.privCmd.Wait()
}
