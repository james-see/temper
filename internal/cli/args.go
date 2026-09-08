package cli

import (
	"strings"

	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/config"
)

func splitAgentArgs(args []string, cfg config.Config, flagAgent string) (agentID, goal string, err error) {
	if flagAgent != "" {
		id, typ, ok := agent.Resolve(flagAgent, cfg)
		if !ok {
			id, typ = agent.Normalize(flagAgent), agent.TypeOf(flagAgent)
		}
		if ok && !agent.Implemented(id) && typ != "temper" {
			return id, strings.Join(args, " "), agent.UnimplementedError(id)
		}
		if !ok && !agent.Implemented(flagAgent) && agent.IsKnown(flagAgent) {
			return flagAgent, strings.Join(args, " "), agent.UnimplementedError(flagAgent)
		}
		return agent.Normalize(flagAgent), strings.Join(args, " "), nil
	}
	if len(args) == 0 {
		return "", "", nil
	}
	id, typ, ok := agent.Resolve(args[0], cfg)
	if !ok {
		return "", strings.Join(args, " "), nil
	}
	if !agent.Implemented(id) && typ != "temper" {
		return id, strings.Join(args[1:], " "), agent.UnimplementedError(id)
	}
	return id, strings.Join(args[1:], " "), nil
}
