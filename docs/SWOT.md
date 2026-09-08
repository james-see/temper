# SWOT

## Strengths

- Sits above existing agents instead of replacing them
- Provider-independent and agent-independent
- Local-first support through oMLX/Ollama
- Observable and event-sourced
- Progress/stagnation awareness is a strong differentiator
- Can improve orchestration empirically over time
- More agents/models increase Temper's routing value

## Weaknesses

- Very large integration surface
- Provider and agent CLIs change frequently
- "Progress" is task-dependent and difficult to generalize
- Coding-agent evaluations can be noisy
- Multi-agent workflows can become expensive
- Some external agents expose limited telemetry/control

## Opportunities

- Coding agents are fragmenting into a multi-agent ecosystem
- Teams need model/agent independence
- Local models are improving rapidly
- Enterprise buyers need budgets, auditability and governance
- Agent execution generates valuable telemetry that is currently underused
- MCP/ACP-style protocols can reduce integration cost

## Threats

- Major agent vendors can add native orchestration/recovery
- Open-source peers can copy loop detection and routing
- Protocol standardization may commoditize adapters
- OSS developer-tool monetization is difficult
- Scope creep could prevent a compelling MVP

## Strategic conclusion

Do not win by implementing the most tools or providers.

Win by being the best **supervisor**:
1. detect lack of progress
2. recover intelligently
3. choose the right executor
4. prove the intervention improved outcomes
5. learn from accumulated evidence
