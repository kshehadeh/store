package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// Console reads answers from stdin and writes prompts to stderr, so stdout
// carries only pulled content.
type Console struct {
	Out io.Writer
	// Interactive is true when stdin is a terminal, or when STORE_INTERACTIVE=1.
	Interactive bool
	in          *bufio.Reader
	tty         bool
	input       *os.File
}

func NewConsole(in *os.File, out io.Writer) *Console {
	fd := int(in.Fd())
	tty := term.IsTerminal(fd)
	return &Console{
		Out:         out,
		Interactive: tty || os.Getenv("STORE_INTERACTIVE") == "1",
		in:          bufio.NewReader(in),
		tty:         tty,
		input:       in,
	}
}

// Reader returns stdin for reading piped content.
func (c *Console) Reader() io.Reader { return c.in }

func (c *Console) Info(format string, args ...any) { fmt.Fprintf(c.Out, format+"\n", args...) }

func (c *Console) readLine() (string, error) {
	line, err := c.in.ReadString('\n')
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return "", err
		}
		if line == "" {
			return "", errors.New("no input received")
		}
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Ask prompts for a line of input, returning def if the answer is empty.
func (c *Console) Ask(prompt, def string) (string, error) {
	if !c.Interactive {
		return "", errors.New("input is required but the terminal is not interactive")
	}
	if c.tty {
		return c.promptInput(prompt, def, false)
	}
	if def != "" {
		fmt.Fprintf(c.Out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(c.Out, "%s: ", prompt)
	}
	ans, err := c.readLine()
	if err != nil {
		return "", err
	}
	if ans = strings.TrimSpace(ans); ans == "" {
		ans = def
	}
	return ans, nil
}

// ReadSecret reads one line without echoing it on a terminal.
func (c *Console) ReadSecret(prompt string) (string, error) {
	if c.tty {
		return c.promptInput(prompt, "", true)
	}
	fmt.Fprint(c.Out, prompt)
	return c.readLine()
}

// Choose shows a searchable terminal list (numbered lines for piped input)
// and returns the selected index.
func (c *Console) Choose(prompt string, options []string) (int, error) {
	if !c.Interactive {
		return -1, errors.New("a selection is required but the terminal is not interactive")
	}
	if len(options) == 0 {
		return -1, errors.New("no options to select")
	}
	if c.tty {
		return c.chooseItem(prompt, options)
	}
	fmt.Fprintln(c.Out, prompt)
	for i, o := range options {
		fmt.Fprintf(c.Out, "  %d) %s\n", i+1, o)
	}
	for range 3 {
		fmt.Fprintf(c.Out, "Enter a number [1-%d]: ", len(options))
		ans, err := c.readLine()
		if err != nil {
			return -1, err
		}
		if n, err := strconv.Atoi(strings.TrimSpace(ans)); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		fmt.Fprintln(c.Out, "Invalid selection.")
	}
	return -1, errors.New("no valid selection was made")
}
