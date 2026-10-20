package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func (a *App) apiCommand() *cobra.Command {

	var method, data, contentType string
	var include bool
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Call any API route with your credential",
		Long: `Call any route of the public API, signed in as you, and print what it answers.
It is the way to reach something octopaas has no command for yet; the routes
are described at /openapi.yaml.

The method is GET, or POST when you send data. --data takes a string, @file,
or @- for stdin. A JSON answer is pretty-printed; anything else is printed as
it came. A failure status still prints the answer, then exits 1.`,
		Example: `  octopaas api /v1/me
  octopaas api /v1/deployments | jq '.[].status'
  octopaas api -X PATCH /v1/deployments/$ID -d '{"replicas": 2}'
  octopaas api /openapi.yaml > openapi.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			path := args[0]
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			var body []byte
			if data != "" {
				var err error
				if body, err = a.dataArg(data); err != nil {
					return err
				}
				if method == "" {
					method = http.MethodPost
				}
			}
			if method == "" {
				method = http.MethodGet
			}

			api, err := a.client()
			if err != nil {
				return err
			}
			resp, err := api.Raw(cmd.Context(), strings.ToUpper(method), path, body, contentType)
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()

			if include {
				_, _ = fmt.Fprintf(a.Stdout, "%s %s\n", resp.Proto, resp.Status)
				_ = resp.Header.Write(a.Stdout)
				_, _ = fmt.Fprintln(a.Stdout)
			}
			answer, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			var pretty bytes.Buffer
			if strings.Contains(resp.Header.Get("Content-Type"), "json") && json.Indent(&pretty, answer, "", "  ") == nil {
				pretty.WriteByte('\n')
				answer = pretty.Bytes()
			}
			if _, err := a.Stdout.Write(answer); err != nil {
				return err
			}
			if resp.StatusCode >= 400 {
				return apiStatus(resp.StatusCode)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&method, "method", "X", "", "the HTTP method (default GET, or POST with --data)")
	cmd.Flags().StringVarP(&data, "data", "d", "", "the request body: a string, @file, or @- for stdin")
	cmd.Flags().StringVar(&contentType, "content-type", "application/json", "the body's Content-Type")
	cmd.Flags().BoolVarP(&include, "include", "i", false, "print the status line and headers too")
	return cmd
}

func (a *App) dataArg(data string) ([]byte, error) {

	if name, ok := strings.CutPrefix(data, "@"); ok {
		return a.readInput(name)
	}
	return []byte(data), nil
}

// apiStatus is a failed raw call: the answer is already on stdout, so all that
// is left to say is the status.
type apiStatus int

func (s apiStatus) Error() string {
	return fmt.Sprintf("the API answered %d %s", int(s), http.StatusText(int(s)))
}
