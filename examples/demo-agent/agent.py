import argparse
import json
import subprocess


def rpc(proc, msg):
    proc.stdin.write(json.dumps(msg) + "\n")
    proc.stdin.flush()
    if "id" not in msg:
        return None
    while True:
        line = proc.stdout.readline()
        reply = json.loads(line)
        if reply.get("id") == msg["id"]:
            return reply


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--prompt", required=True)
    ap.add_argument("--mcp-config", required=True)
    args = ap.parse_args()

    servers = json.load(open(args.mcp_config))["mcpServers"]
    server = next(iter(servers.values()))
    proc = subprocess.Popen([server["command"], *server.get("args", [])],
                            stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)

    rpc(proc, {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
        "protocolVersion": "2025-06-18", "capabilities": {},
        "clientInfo": {"name": "demo-agent", "version": "0"}}})
    rpc(proc, {"jsonrpc": "2.0", "method": "notifications/initialized"})

    reply = rpc(proc, {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
                       "params": {"name": "read_graph", "arguments": {}}})
    graph = json.loads(reply["result"]["content"][0]["text"])
    print(f"The knowledge graph contains {len(graph['entities'])} entities.")

    proc.stdin.close()
    proc.wait(timeout=10)


if __name__ == "__main__":
    main()
