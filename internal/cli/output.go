package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

const (
	ExitOK       = 0
	ExitError    = 1
	ExitUsage    = 2
	ExitNotFound = 3
)

type CLIError struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions,omitempty"`
	ExitCode    int      `json:"exit_code"`
	Retryable   bool     `json:"retryable,omitempty"`
}

func (e *CLIError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	for _, s := range e.Suggestions {
		b.WriteString("\n  hint: ")
		b.WriteString(s)
	}
	return b.String()
}

func Err(code, message string) *CLIError {
	return &CLIError{Code: code, Message: message, ExitCode: ExitError}
}

func ErrWithExit(code, message string, exitCode int) *CLIError {
	return &CLIError{Code: code, Message: message, ExitCode: exitCode}
}

type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

func ResolveFormat(flag string, jsonFlag bool) Format {
	if flag != "" {
		return Format(strings.ToLower(flag))
	}
	if jsonFlag {
		return FormatJSON
	}
	if !IsTerminal() {
		return FormatJSON
	}
	return FormatTable
}

func IsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

func EncodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func WriteError(w io.Writer, format Format, e *CLIError) {
	if format == FormatJSON {
		envelope := struct {
			Error *CLIError `json:"error"`
		}{Error: e}
		json.NewEncoder(w).Encode(envelope)
		return
	}
	fmt.Fprintln(w, "error:", e.Error())
}
