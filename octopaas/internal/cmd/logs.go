package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/juancavallotti/octo/octopaas/internal/client"
	"github.com/juancavallotti/octo/octopaas/internal/ui"
)

// reconnectAfter is how long a follow waits before reconnecting a stream that
// ended with an error.
const reconnectAfter = 2 * time.Second

func (a *App) logsCommand() *cobra.Command {

	var follow bool
	var pod, level, grep string
	var tail, limit int
	var since time.Duration
	var all bool

	cmd := &cobra.Command{
		Use:   "logs <deployment>",
		Short: "Read or follow what a deployment logs",
		Long: `Read a deployment's logs three ways:

  - By default, its stored history: newest last, across every pod it has had,
    filtered with --level, --grep and --since. --all pages through everything.
  - With --tail, the last lines its pods wrote, straight from the cluster.
  - With --follow, its pods' logs live, line by line, until you press Ctrl-C.
    The platform ends a stream after thirty minutes; octopaas reconnects.

--pod narrows tail and follow to one pod; without it every pod is read, each
with its own colour.`,
		Example: `  octopaas logs billing --since 1h --level ERROR
  octopaas logs billing --grep invoice --all -o json
  octopaas logs billing --tail 100
  octopaas logs billing -f`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			api, err := a.client()
			if err != nil {
				return err
			}
			id, err := a.deployment(ctx, api, args[0])
			if err != nil {
				return err
			}

			if follow || cmd.Flags().Changed("tail") {
				pods, err := a.podsOf(ctx, api, id, pod)
				if err != nil {
					return err
				}
				if follow {
					if !cmd.Flags().Changed("tail") {
						tail = -1
					}
					return a.follow(ctx, api, id, pods, tail)
				}
				return a.tailPods(ctx, api, id, pods, tail)
			}
			query := client.LogQuery{Level: strings.ToUpper(level), Text: grep, Limit: limit}
			if since > 0 {
				query.From = time.Now().Add(-since)
			}
			return a.history(ctx, api, id, query, all)
		},
	}
	flags := cmd.Flags()
	flags.BoolVarP(&follow, "follow", "f", false, "stream new lines as they are written")
	flags.StringVar(&pod, "pod", "", "only this pod (tail and follow)")
	flags.IntVar(&tail, "tail", 0, "the last N lines from the cluster; with --follow, how many to start with")
	flags.StringVar(&level, "level", "", "only this level: DEBUG, INFO, WARN or ERROR")
	flags.StringVar(&grep, "grep", "", "only messages containing this text")
	flags.DurationVar(&since, "since", 0, "only entries newer than this, such as 30m or 24h")
	flags.IntVar(&limit, "limit", 200, "entries per page (at most 1000)")
	flags.BoolVar(&all, "all", false, "page through every matching entry, not only the newest page")
	return cmd
}

// history prints stored entries oldest first, the way a log reads.
func (a *App) history(ctx context.Context, api *client.Client, id string, q client.LogQuery, all bool) error {

	entries := []map[string]any{}
	err := a.ui.Spin(ui.Ink, "Reading the logbook", func(l *ui.Live) error {
		for {
			page, err := api.DeploymentLogs(ctx, id, q)
			if err != nil {
				return err
			}
			entries = append(entries, page.Items...)
			l.Detail(fmt.Sprintf("%d entries", len(entries)))
			if !all || page.NextBefore == "" {
				return nil
			}
			q.Before = page.NextBefore
		}
	})
	if err != nil {
		return err
	}
	//pages come newest first; we read oldest first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	if a.output != outputTable {
		return a.emit(entries, nil)
	}
	if len(entries) == 0 {
		a.ui.Moment(ui.Searching, "No log entries match.", "Widen --since, or drop --level and --grep.")
		return nil
	}
	for _, e := range entries {
		a.ui.Print("%s", a.entryLine(e))
	}
	return nil
}

func (a *App) entryLine(e map[string]any) string {

	ts, _ := e["ts"].(string)
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		ts = t.Local().Format("Jan 02 15:04:05.000")
	}
	level, _ := e["level"].(string)
	message, _ := e["message"].(string)
	line := fmt.Sprintf("%s %s %s", a.ui.Dim(ts), a.level(level), message)
	if attrs, ok := e["attrs"].(map[string]any); ok && len(attrs) > 0 {
		data, _ := json.Marshal(attrs)
		line += " " + a.ui.Dim(string(data))
	}
	return line
}

// level paints a level by how worried you should be.
func (a *App) level(level string) string {

	padded := fmt.Sprintf("%-5s", strings.ToUpper(level))
	if !a.ui.Fancy {
		return padded
	}
	color := map[string]string{"DEBUG": "#868E96", "INFO": "#3BC9DB", "WARN": "#FFD43B", "ERROR": "#FF6B6B"}[strings.ToUpper(level)]
	if color == "" {
		return padded
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(padded)
}

// podsOf is the pods to read: the one asked for, or every pod the deployment
// has now.
func (a *App) podsOf(ctx context.Context, api *client.Client, id, pod string) ([]string, error) {

	if pod != "" {
		return []string{pod}, nil
	}
	d, err := api.GetDeployment(ctx, id)
	if err != nil {
		return nil, err
	}
	ret := make([]string, 0, len(d.Pods))
	for _, p := range d.Pods {
		ret = append(ret, p.Name)
	}
	if len(ret) == 0 {
		return nil, fmt.Errorf("%s has no pods right now, so there is nothing to tail; its stored history is `octopaas logs %s`", d.Name, d.Name)
	}
	return ret, nil
}

func (a *App) tailPods(ctx context.Context, api *client.Client, id string, pods []string, tail int) error {

	for _, pod := range pods {
		var buf strings.Builder
		if err := api.PodLogs(ctx, id, pod, tail, &buf); err != nil {
			return err
		}
		prefix := a.podPrefix(pod, len(pods))
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			if line != "" {
				a.ui.Print("%s%s", prefix, a.paintLine(line))
			}
		}
	}
	return nil
}

// follow streams every pod at once until Ctrl-C, reconnecting each stream when
// the platform ends it.
func (a *App) follow(ctx context.Context, api *client.Client, id string, pods []string, tail int) error {

	if a.ui.Fancy {
		a.ui.Say("%s", a.ui.Accent(fmt.Sprintf("~(°▿°)~ following %d pod(s); Ctrl-C to stop", len(pods))))
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make([]error, len(pods))
	for i, pod := range pods {
		wg.Add(1)
		go func() {

			defer wg.Done()
			prefix := a.podPrefix(pod, len(pods))
			start := tail
			for ctx.Err() == nil {
				err := api.StreamPodLogs(ctx, id, pod, start, func(ev client.Event) error {
					switch ev.Name {
					case "log":
						mu.Lock()
						a.ui.Print("%s%s", prefix, a.paintLine(ev.Data))
						mu.Unlock()
					case "error":
						var body struct {
							Error string `json:"error"`
						}
						_ = json.Unmarshal([]byte(ev.Data), &body)
						return errors.New(firstOf(body.Error, ev.Data))
					}
					return nil
				})
				//a refusal is final (the pod is gone, or you may not read it); a
				//stream that ended or broke is picked up again from where it is.
				if client.StatusOf(err) >= 400 && client.StatusOf(err) < 500 {
					errs[i] = err
					return
				}
				if err != nil {
					a.ui.Warn("%s: %v; reconnecting", pod, err)
				}
				//a reconnect asks for no history: what came before the cut
				//has been printed already. A line written while we were away
				//is lost, which beats replaying hundreds we have shown.
				start = 0
				select {
				case <-ctx.Done():
				case <-time.After(reconnectAfter):
				}
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

var podColors = []string{"#FF7B54", "#3BC9DB", "#B197FC", "#69DB7C", "#FFD43B", "#F783AC"}

func (a *App) podPrefix(pod string, pods int) string {

	if pods < 2 {
		return ""
	}
	label := pod
	if i := strings.LastIndexByte(pod, '-'); i > 0 {
		label = pod[i+1:]
	}
	if !a.ui.Fancy {
		return label + " | "
	}
	sum := 0
	for _, r := range pod {
		sum += int(r)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(podColors[sum%len(podColors)])).Bold(true).Render(label) + " │ "
}

// paintLine colours a raw pod log line by the level it mentions, whatever shape
// the line has: JSON, key=value, or text.
func (a *App) paintLine(line string) string {

	if !a.ui.Fancy {
		return line
	}
	upper := strings.ToUpper(line)
	for _, lvl := range []string{"ERROR", "WARN", "DEBUG"} {
		for _, marker := range []string{`"LEVEL":"` + lvl, "LEVEL=" + lvl, " " + lvl + " "} {
			if strings.Contains(upper, marker) {
				color := map[string]string{"ERROR": "#FF6B6B", "WARN": "#FFD43B", "DEBUG": "#868E96"}[lvl]
				return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(line)
			}
		}
	}
	return line
}
