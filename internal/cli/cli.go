package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/run"
	"github.com/james-see/temper/internal/tui"
	"github.com/spf13/cobra"
)

const Version = "0.1.0"

func Execute() {
	if err := root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	var (
		plain    bool
		cfgPath  string
		agent    string
		prov     string
		model    string
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
				Plain: plain, Config: cfgPath, Agent: agent, Provider: prov, Model: model,
			})
		},
	}
	cmd.PersistentFlags().BoolVar(&plain, "plain", false, "log events to stdout (no TUI)")
	cmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file")
	cmd.PersistentFlags().StringVar(&agent, "agent", "", "agent id")
	cmd.PersistentFlags().StringVar(&prov, "provider", "", "provider id")
	cmd.PersistentFlags().StringVar(&model, "model", "", "model id")

	cmd.AddCommand(runCmd(&plain, &cfgPath, &agent, &prov, &model))
	cmd.AddCommand(inspectCmd(&plain, &cfgPath))
	cmd.AddCommand(configCmd(&cfgPath))
	cmd.AddCommand(versionCmd())
	return cmd
}

func runCmd(plain *bool, cfgPath, agent, prov, model *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run [goal]",
		Short: "Execute a supervised coding run",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doRun(cmd.Context(), strings.Join(args, " "), config.Flags{
				Plain: *plain, Config: *cfgPath, Agent: *agent, Provider: *prov, Model: *model,
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
			return tui.Run("", mgr.Hub, nil, true, nil)
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
	mgr, err := run.Open(loaded.Config, root)
	if err != nil {
		return err
	}
	defer mgr.Close()

	plain := flags.Plain || !isTTY()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := func(g string) {
		go func() {
			_, _ = mgr.Execute(runCtx, run.Options{
				Goal:     g,
				Root:     root,
				Agent:    flags.Agent,
				Provider: flags.Provider,
				Model:    flags.Model,
				Plain:    plain,
			})
		}()
	}

	if plain {
		if strings.TrimSpace(goal) == "" {
			return fmt.Errorf("goal required with --plain")
		}
		go printLive(runCtx, mgr)
		_, err := mgr.Execute(runCtx, run.Options{
			Goal: goal, Root: root, Agent: flags.Agent, Provider: flags.Provider, Model: flags.Model, Plain: true,
		})
		return err
	}

	return tui.Run(goal, mgr.Hub, cancel, false, start)
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
