# demo-agent

A deliberately fragile MCP agent. It asks the memory server how many entities its
knowledge graph holds, with one tool call, and never checks what comes back.

```bash
python3 agent.py --prompt "How many entities?" --mcp-config mcp.json
```

Its MCP server is `npx -y @modelcontextprotocol/server-memory`, registered as `memory`.

This is the agent in the plugin demo. Open this folder in Claude Code with the gremlyn plugin
installed and run `/gremlyn:chaos-test`: it writes `.gremlyn/arena.yaml`, finds the two
failures, fixes `agent.py` and re-runs.
