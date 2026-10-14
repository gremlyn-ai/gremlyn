# notes-server

A throwaway Streamable HTTP MCP server for trying the annotation-aware rubric. It keeps three
notes in memory and exposes three annotated tools:

| Tool | Annotations | What it does |
|---|---|---|
| `list_notes` | `readOnlyHint: true` | returns every note as free text |
| `create_note` | `readOnlyHint: false`, `destructiveHint: false`, `idempotentHint: false` | creates a note; two calls make two notes |
| `delete_note` | `readOnlyHint: false`, `destructiveHint: true`, `idempotentHint: true` | deletes a note for good |

It listens on a loopback address only and logs every create and delete on stderr, so you can
see what the agent did to it.

```bash
go run ./examples/notes-server
gremlyn arena ci --config examples/arena-notes.yaml
```

[`examples/arena-notes.yaml`](../arena-notes.yaml) runs the two failures the rubric scores
from annotations: an injected instruction asking for a `delete_note` call, and a timeout on
`create_note`, after which calling it again writes the note twice. Restart the server between
runs to get the three notes back.
