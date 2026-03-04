package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
)

func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func ColorizeStatus(status string, enable bool) string {
	if !enable {
		return status
	}

	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "running"):
		return colorize(status, ansiGreen)
	case strings.Contains(s, "crashloopbackoff"), strings.Contains(s, "error"), strings.Contains(s, "failed"):
		return colorize(status, ansiRed)
	case strings.Contains(s, "pending"):
		return colorize(status, ansiYellow)
	default:
		return status
	}
}

func colorize(s, c string) string {
	return fmt.Sprintf("%s%s%s", c, s, ansiReset)
}
