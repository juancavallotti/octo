package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// emit prints v as -o asked: JSON or YAML for machines, or whatever human draws
// for people.
func (a *App) emit(v any, human func()) error {

	switch a.output {
	case outputJSON:
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case outputYAML:
		//we go through JSON so YAML uses the API's field names, not Go's.
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		out, err := yaml.Marshal(doc)
		if err != nil {
			return err
		}
		_, err = a.Stdout.Write(out)
		return err
	default:
		human()
		return nil
	}
}

// ago says how long ago t was, the way a person would.
func ago(t time.Time) string {

	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Local().Format("2 Jan 2006")
	}
}

// readInput reads a file, or stdin when path is "-".
func (a *App) readInput(path string) ([]byte, error) {

	if path == "-" {
		return io.ReadAll(a.Stdin)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// confirm asks a yes/no question on the terminal. Without one there is nobody
// to ask, so the command must have been given --yes.
func (a *App) confirm(question string, yes bool) error {

	if yes {
		return nil
	}
	if !a.ui.Fancy {
		return usageError{fmt.Errorf("%s: add --yes to do it without asking", question)}
	}
	_, _ = fmt.Fprintf(a.Stderr, "%s %s ", a.ui.Accent("?"), question+" [y/N]")
	answer, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return errCancelled
}

var errCancelled = fmt.Errorf("left as it was")

func short(id string) string {

	if len(id) > 8 {
		return id[:8]
	}
	return id
}
