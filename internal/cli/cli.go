package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/debuglog"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/provider"
	"github.com/james-see/temper/internal/run"
	"github.com/james-see/temper/internal/tui"
	"github.com/spf13/cobra"
)

const Version = "0.1.4"

func Execute() {
	if err := root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	var (
		plain   bool
		debug   bool
		cfgPath string
		agent   string
		prov    string
		model   string
	)
	cmd := &cobra.Command{
		Use:           "temper",
		Short:         "Adaptive control plane for coding agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			goal := strings.Join(args, " ")
			return doRun(cmd.Context(), goal, config.Flags{
				Plain: plain, Debug: debug, Config: cfgPath, Agent: agent, Provider: prov, Model: model,
			})
		},
	}
	cmd.PersistentFlags().BoolVar(&plain, "plain", false, "log events to stdout (no TUI)")
	cmd.PersistentFlags().BoolVar(&debug, "debug", false, "verbose slog to stderr and .temper/debug.log")
	cmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file")
	cmd.PersistentFlags().StringVar(&agent, "agent", "", "agent id")
	cmd.PersistentFlags().StringVar(&prov, "provider", "", "provider id")
	cmd.PersistentFlags().StringVar(&model, "model", "", "model id")

	cmd.AddCommand(runCmd(&plain, &debug, &cfgPath, &agent, &prov, &model))
	cmd.AddCommand(debugCmd(&plain, &debug, &cfgPath, &agent, &prov, &model))
	cmd.AddCommand(inspectCmd(&plain, &cfgPath))
	cmd.AddCommand(configCmd(&cfgPath))
	cmd.AddCommand(versionCmd())
	return cmd
}

func runCmd(plain, debug *bool, cfgPath, agent, prov, model *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run [goal]",
		Short: "Execute a supervised coding run",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doRun(cmd.Context(), strings.Join(args, " "), config.Flags{
				Plain: *plain, Debug: *debug, Config: *cfgPath, Agent: *agent, Provider: *prov, Model: *model,
			})
		},
	}
}

func debugCmd(plain, debug *bool, cfgPath, agent, prov, model *string) *cobra.Command {
	return &cobra.Command{
		Use:   "debug [goal]",
		Short: "Same as temper, with verbose debug logging",
		Long:  "Enable verbose slog (routing, tools, reflex, judge, state). TUI stays; logs go to .temper/debug.log. With --plain, logs also go to stderr. Same as TEMPER_DEBUG=1 or --debug.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doRun(cmd.Context(), strings.Join(args, " "), config.Flags{
				Plain: *plain, Debug: true, Config: *cfgPath, Agent: *agent, Provider: *prov, Model: *model,
			})
		},
	}
}

func inspectCmd(plain *bool, cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <run-id>",
		Short: "Inspect a run event stream",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load(config.Flags{Plain: *plain, Config: *cfgPath})
			if err != nil {
				return err
			}
			root, err := loaded.Config.WorkspaceRoot()
			if err != nil {
				return err
			}
			mgr, err := run.Open(loaded.Config, root)
			if err != nil {
				return err
			}
			defer mgr.Close()
			r, evs, err := mgr.Load(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			mgr.Hydrate(r, evs)
			if *plain || !isTTY() {
				printEvents(os.Stdout, evs)
				return nil
			}
			return tui.Run(tui.Options{Hub: mgr.Hub, Inspect: true})
		},
	}
}

func configCmd(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Show configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print merged config and sources",
		RunE: func(*cobra.Command, []string) error {
			loaded, err := config.Load(config.Flags{Config: *cfgPath})
			if err != nil {
				return err
			}
			y, err := config.Redact(loaded.Config).YAML()
			if err != nil {
				return err
			}
			fmt.Print(y)
			fmt.Println("# sources")
			for k, v := range loaded.Sources {
				fmt.Printf("# %s: %s\n", k, v)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print loaded config files",
		RunE: func(*cobra.Command, []string) error {
			loaded, err := config.Load(config.Flags{Config: *cfgPath})
			if err != nil {
				return err
			}
			if len(loaded.Files) == 0 {
				fmt.Println("(defaults only)")
				return nil
			}
			for _, f := range loaded.Files {
				abs, err := filepath.Abs(f)
				if err != nil {
					fmt.Println(f)
					continue
				}
				fmt.Println(abs)
			}
			return nil
		},
	})
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(*cobra.Command, []string) error {
			fmt.Println("temper", Version)
			return nil
		},
	}
}

func doRun(ctx context.Context, goal string, flags config.Flags) error {
	loaded, err := config.Load(flags)
	if err != nil {
		return err
	}
	root, err := loaded.Config.WorkspaceRoot()
	if err != nil {
		return err
	}
	plain := flags.Plain || !isTTY()
	if debuglog.Requested(flags.Debug) {
		log, err := debuglog.Setup(config.DataDir(root), !plain)
		if err != nil {
			return err
		}
		log.Debug("debug enabled", "plain", plain, "log", filepath.Join(config.DataDir(root), "debug.log"))
	}
	catalog := provider.Discover(ctx, loaded.Config)
	if debuglog.Requested(flags.Debug) {
		for _, c := range catalog.Candidates {
			slog.Debug("provider.probe", "id", c.ID, "usable", c.Usable, "reason", c.Reason)
		}
	}
	mgr, err := run.Open(loaded.Config, root)
	if err != nil {
		return err
	}
	defer mgr.Close()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	listModels := func(id string) ([]string, error) {
		cctx, done := context.WithTimeout(context.Background(), 8*time.Second)
		defer done()
		return provider.ModelNames(cctx, loaded.Config, id)
	}

	if plain {
		printDiscovery(os.Stdout, catalog)
		if strings.TrimSpace(goal) == "" {
			return fmt.Errorf("goal required with --plain")
		}
		if flags.Model == "" {
			return fmt.Errorf("model required with --plain (no picker)")
		}
		prov := flags.Provider
		if prov == "" {
			if c, ok := catalog.BestCandidate(); ok {
				prov = c.ID
			}
		}
		if prov == "" {
			if flags.Provider == "" && flags.Model == "" {
				return fmt.Errorf("no usable providers; set OLLAMA_API_KEY, start ollama, or pass --provider and --model")
			}
			return fmt.Errorf("no usable providers; pass --provider")
		}
		go printLive(runCtx, mgr)
		_, err := mgr.Execute(runCtx, run.Options{
			Goal: goal, Root: root, Agent: flags.Agent, Provider: prov, Model: flags.Model, Plain: true,
		})
		return err
	}

	start := func(g, prov, model string) {
		go func() {
			_, _ = mgr.Execute(runCtx, run.Options{
				Goal:     g,
				Root:     root,
				Agent:    flags.Agent,
				Provider: prov,
				Model:    model,
				Plain:    false,
			})
		}()
	}
	follow := func(text string) {
		go func() {
			_, _ = mgr.Followup(runCtx, text)
		}()
	}

	return tui.Run(tui.Options{
		Goal:       goal,
		Provider:   flags.Provider,
		Model:      flags.Model,
		Hub:        mgr.Hub,
		Cancel:     cancel,
		OnStart:    start,
		OnFollow:   follow,
		Catalog:    catalog,
		ListModels: listModels,
	})
}

func printDiscovery(w io.Writer, st provider.Status) {
	fmt.Fprintln(w, "providers:")
	if len(st.Candidates) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	for _, c := range st.Candidates {
		state := "no"
		if c.Usable {
			state = "usable"
		}
		src := c.Reason
		if c.KeySource != "" {
			src = src + " (" + c.KeySource + ")"
		}
		fmt.Fprintf(w, "  %-14s  %-7s  %s\n", c.ID, state, src)
	}
	if st.Best != "" {
		fmt.Fprintf(w, "preferred: %s\n", st.Best)
	}
}

func printLive(ctx context.Context, mgr *run.Manager) {
	seen := 0
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			snap := mgr.Hub.Get()
			if len(snap.Events) > seen {
				printEvents(os.Stdout, snap.Events[seen:])
				seen = len(snap.Events)
			}
			if snap.Done {
				return
			}
		}
	}
}

func printEvents(w io.Writer, evs []event.Event) {
	for _, ev := range evs {
		fmt.Fprintf(w, "%d  %s  %s\n", ev.Sequence, ev.Type, compactJSON(ev.Data))
	}
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func isTTY() bool {
	st, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
