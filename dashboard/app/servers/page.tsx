"use client";

import { useState } from "react";
import { Sidebar } from "@/components/Sidebar";
import { TopBar } from "@/components/TopBar";
import { useShieldServers, mutateServers } from "@/lib/hooks/useShieldData";
import { createServer, deleteServer, updateServer } from "@/lib/api/shield";
import type { ServerInfo } from "@/lib/api/types";

type NewServerForm = {
  name: string;
  mode: "proxy" | "wrap" | "cloud";
  upstream_url: string;
  command: string;
  auth_header: string;
};

const emptyForm: NewServerForm = { name: "", mode: "proxy", upstream_url: "", command: "", auth_header: "" };

function statusColor(status: string) {
  switch (status) {
    case "active": return "text-primary";
    case "inactive": return "text-on-surface-variant";
    case "error": return "text-error";
    default: return "text-on-surface-variant";
  }
}

function statusDot(status: string) {
  switch (status) {
    case "active": return "bg-primary";
    case "inactive": return "bg-on-surface-variant";
    case "error": return "bg-error";
    default: return "bg-on-surface-variant";
  }
}

export default function ServersPage() {
  const { data: servers, error, isLoading } = useShieldServers();
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<NewServerForm>(emptyForm);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editForm, setEditForm] = useState<NewServerForm>(emptyForm);

  async function handleCreate() {
    if (!form.name.trim()) return;
    setSubmitting(true);
    setFormError(null);
    try {
      await createServer({
        name: form.name.trim(),
        mode: form.mode,
        upstream_url: form.mode !== "wrap" ? form.upstream_url : undefined,
        command: form.mode === "wrap" ? form.command : undefined,
        auth_header: form.mode === "cloud" ? form.auth_header : undefined,
      });
      setForm(emptyForm);
      setShowForm(false);
      await mutateServers();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Failed to create server");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleToggleStatus(s: ServerInfo) {
    const newStatus = s.status === "active" ? "inactive" : "active";
    try {
      await updateServer(s.id, { status: newStatus } as Partial<ServerInfo>);
      await mutateServers();
    } catch {
      // ignore
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteServer(id);
      await mutateServers();
    } catch {
      // ignore
    }
  }

  async function handleUpdate(id: string) {
    setSubmitting(true);
    try {
      await updateServer(id, {
        name: editForm.name.trim(),
        mode: editForm.mode,
        upstream_url: editForm.mode !== "wrap" ? editForm.upstream_url : undefined,
        command: editForm.mode === "wrap" ? editForm.command : undefined,
        auth_header: editForm.mode === "cloud" ? editForm.auth_header : undefined,
      } as Partial<ServerInfo>);
      setEditingId(null);
      await mutateServers();
    } catch {
      // ignore
    } finally {
      setSubmitting(false);
    }
  }

  function startEdit(s: ServerInfo) {
    setEditingId(s.id);
    setEditForm({
      name: s.name,
      mode: s.mode,
      upstream_url: s.upstream_url ?? "",
      command: s.command ?? "",
      auth_header: s.auth_header ?? "",
    });
  }

  return (
    <div className="selection-primary">
      <Sidebar activePage="servers" />

      <main className="flex-1 ml-64 bg-background relative min-h-screen">
        <TopBar accent="primary" statusText="Servers_Config" statusLabel="Servers_Config" />

        <div className="p-8 max-w-7xl mx-auto space-y-8 pb-24 relative z-10">
          {/* Header */}
          <div className="flex items-end justify-between border-b border-outline-variant/20 pb-4">
            <div>
              <span className="font-mono text-primary text-xs font-bold tracking-[0.2em]">
                CONFIGURATION
              </span>
              <h3 className="text-4xl font-headline font-bold tracking-tight mt-1">
                MCP SERVERS
              </h3>
            </div>
            <button
              onClick={() => setShowForm(!showForm)}
              className="bg-primary text-on-primary px-6 py-3 font-mono font-bold text-xs tracking-widest hover:shadow-[0_0_10px_rgba(142,255,113,0.3)] transition-all"
            >
              {showForm ? "CANCEL" : "ADD_SERVER"}
            </button>
          </div>

          {/* Create form */}
          {showForm && (
            <div className="bg-surface-container-low p-6 space-y-4 border-l-2 border-primary">
              <h4 className="font-headline text-sm font-bold uppercase tracking-widest text-primary">
                NEW_SERVER
              </h4>

              {formError && (
                <div className="bg-error/10 border border-error/20 p-3">
                  <span className="font-mono text-xs text-error">{formError}</span>
                </div>
              )}

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="font-mono text-[10px] text-on-surface-variant block mb-1">NAME</label>
                  <input
                    type="text"
                    className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 placeholder:text-on-surface-variant/30 focus:ring-1 focus:ring-primary"
                    placeholder="my-mcp-server"
                    value={form.name}
                    onChange={(e) => setForm({ ...form, name: e.target.value })}
                  />
                </div>
                <div>
                  <label className="font-mono text-[10px] text-on-surface-variant block mb-1">MODE</label>
                  <select
                    className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 appearance-none focus:ring-1 focus:ring-primary"
                    value={form.mode}
                    onChange={(e) => setForm({ ...form, mode: e.target.value as NewServerForm["mode"] })}
                  >
                    <option value="proxy">PROXY</option>
                    <option value="wrap">WRAP</option>
                    <option value="cloud">CLOUD</option>
                  </select>
                </div>
              </div>

              {(form.mode === "proxy" || form.mode === "cloud") && (
                <div>
                  <label className="font-mono text-[10px] text-on-surface-variant block mb-1">UPSTREAM_URL</label>
                  <input
                    type="text"
                    className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 placeholder:text-on-surface-variant/30 focus:ring-1 focus:ring-primary"
                    placeholder={form.mode === "cloud" ? "https://api.example.com/mcp" : "http://localhost:3001"}
                    value={form.upstream_url}
                    onChange={(e) => setForm({ ...form, upstream_url: e.target.value })}
                  />
                </div>
              )}

              {form.mode === "cloud" && (
                <div>
                  <label className="font-mono text-[10px] text-on-surface-variant block mb-1">AUTH_HEADER</label>
                  <input
                    type="password"
                    className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 placeholder:text-on-surface-variant/30 focus:ring-1 focus:ring-primary"
                    placeholder="Bearer sk-... or API key"
                    value={form.auth_header}
                    onChange={(e) => setForm({ ...form, auth_header: e.target.value })}
                  />
                  <span className="font-mono text-[9px] text-on-surface-variant/50 mt-1 block">
                    Raw keys are auto-wrapped as Bearer tokens
                  </span>
                </div>
              )}

              {form.mode === "wrap" && (
                <div>
                  <label className="font-mono text-[10px] text-on-surface-variant block mb-1">COMMAND</label>
                  <input
                    type="text"
                    className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 placeholder:text-on-surface-variant/30 focus:ring-1 focus:ring-primary"
                    placeholder="npx -y @modelcontextprotocol/server-filesystem"
                    value={form.command}
                    onChange={(e) => setForm({ ...form, command: e.target.value })}
                  />
                </div>
              )}

              <button
                onClick={handleCreate}
                disabled={!form.name.trim() || submitting}
                className={`px-8 py-3 font-mono font-bold text-xs tracking-widest transition-all ${
                  form.name.trim() && !submitting
                    ? "bg-primary text-on-primary hover:shadow-[0_0_10px_rgba(142,255,113,0.3)]"
                    : "bg-surface-container-highest text-on-surface-variant cursor-not-allowed"
                }`}
              >
                {submitting ? "CREATING..." : "CREATE_SERVER"}
              </button>
            </div>
          )}

          {/* Error */}
          {error && (
            <div className="bg-error/10 border border-error/20 p-4">
              <span className="font-mono text-xs text-error">[ERROR] Failed to load servers — is Shield running?</span>
            </div>
          )}

          {/* Loading */}
          {isLoading && (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="bg-surface-container-low p-6 animate-pulse">
                  <div className="h-4 w-48 bg-surface-container-highest mb-3" />
                  <div className="h-3 w-full bg-surface-container-highest" />
                </div>
              ))}
            </div>
          )}

          {/* Empty */}
          {!isLoading && (!servers || servers.length === 0) && !error && (
            <div className="bg-surface-container-low p-12 text-center">
              <span className="material-symbols-outlined text-4xl text-on-surface-variant mb-4 block">
                dns
              </span>
              <p className="font-mono text-sm text-on-surface-variant">NO_SERVERS_CONFIGURED</p>
              <p className="font-mono text-xs text-on-surface-variant/50 mt-2">
                Add a server via the button above or define servers in gremlyn.yaml
              </p>
            </div>
          )}

          {/* Server list */}
          {servers && servers.length > 0 && (
            <div className="space-y-3">
              {servers.map((s) => (
                <div
                  key={s.id || s.name}
                  className="bg-surface-container-low border-l-2 border-transparent hover:border-primary transition-colors"
                >
                  {editingId === s.id ? (
                    // Edit mode
                    <div className="p-6 space-y-4">
                      <div className="grid grid-cols-2 gap-4">
                        <div>
                          <label className="font-mono text-[10px] text-on-surface-variant block mb-1">NAME</label>
                          <input
                            type="text"
                            className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 focus:ring-1 focus:ring-primary"
                            value={editForm.name}
                            onChange={(e) => setEditForm({ ...editForm, name: e.target.value })}
                          />
                        </div>
                        <div>
                          <label className="font-mono text-[10px] text-on-surface-variant block mb-1">MODE</label>
                          <select
                            className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 appearance-none focus:ring-1 focus:ring-primary"
                            value={editForm.mode}
                            onChange={(e) => setEditForm({ ...editForm, mode: e.target.value as NewServerForm["mode"] })}
                          >
                            <option value="proxy">PROXY</option>
                            <option value="wrap">WRAP</option>
                            <option value="cloud">CLOUD</option>
                          </select>
                        </div>
                      </div>

                      {(editForm.mode === "proxy" || editForm.mode === "cloud") && (
                        <div>
                          <label className="font-mono text-[10px] text-on-surface-variant block mb-1">UPSTREAM_URL</label>
                          <input
                            type="text"
                            className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 focus:ring-1 focus:ring-primary"
                            value={editForm.upstream_url}
                            onChange={(e) => setEditForm({ ...editForm, upstream_url: e.target.value })}
                          />
                        </div>
                      )}

                      {editForm.mode === "cloud" && (
                        <div>
                          <label className="font-mono text-[10px] text-on-surface-variant block mb-1">AUTH_HEADER</label>
                          <input
                            type="password"
                            className="w-full bg-surface-container-highest border-0 text-on-surface font-mono text-xs p-3 focus:ring-1 focus:ring-primary"
                            placeholder="Bearer sk-... or API key"
                            value={editForm.auth_header}
                            onChange={(e) => setEditForm({ ...editForm, auth_header: e.target.value })}
                          />
                        </div>
                      )}

                      <div className="flex gap-2">
                        <button
                          onClick={() => handleUpdate(s.id)}
                          disabled={submitting}
                          className="bg-primary text-on-primary px-6 py-2 font-mono font-bold text-xs"
                        >
                          SAVE
                        </button>
                        <button
                          onClick={() => setEditingId(null)}
                          className="bg-surface-container-highest text-on-surface-variant px-6 py-2 font-mono text-xs"
                        >
                          CANCEL
                        </button>
                      </div>
                    </div>
                  ) : (
                    // Display mode
                    <div className="p-6 flex items-center justify-between">
                      <div className="flex items-center gap-6">
                        <div className="w-10 h-10 bg-surface-container-highest flex items-center justify-center">
                          <span className="material-symbols-outlined text-primary">dns</span>
                        </div>
                        <div>
                          <div className="flex items-center gap-3">
                            <h5 className="font-headline font-bold text-on-surface uppercase">
                              {s.name}
                            </h5>
                            <span className={`flex items-center gap-1.5 font-mono text-[10px] ${statusColor(s.status)}`}>
                              <span className={`w-1.5 h-1.5 rounded-full ${statusDot(s.status)}`} />
                              {s.status.toUpperCase()}
                            </span>
                            <span className="font-mono text-[10px] px-2 py-0.5 bg-surface-container-highest text-on-surface-variant">
                              {s.source.toUpperCase()}
                            </span>
                          </div>
                          <div className="flex items-center gap-4 mt-1 font-mono text-[10px] text-on-surface-variant">
                            <span>MODE: <span className="text-on-surface">{s.mode.toUpperCase()}</span></span>
                            {s.upstream_url && (
                              <span>URL: <span className="text-on-surface">{s.upstream_url}</span></span>
                            )}
                            {s.command && (
                              <span>CMD: <span className="text-on-surface">{s.command}</span></span>
                            )}
                            {s.auth_header && (
                              <span>AUTH: <span className="text-primary">CONFIGURED</span></span>
                            )}
                            {s.id && (
                              <span>ID: <span className="text-on-surface">{s.id.slice(0, 8)}</span></span>
                            )}
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-2">
                        {/* On/Off toggle */}
                        {s.source === "api" && (
                          <button
                            onClick={() => handleToggleStatus(s)}
                            className={`flex items-center gap-1.5 px-3 py-1.5 font-mono text-[10px] font-bold tracking-widest transition-all ${
                              s.status === "active"
                                ? "bg-primary/10 text-primary border border-primary/30 hover:bg-primary/20"
                                : "bg-surface-container-highest text-on-surface-variant border border-outline-variant/20 hover:bg-surface-container-high"
                            }`}
                            title={s.status === "active" ? "Stop server" : "Start server"}
                          >
                            <span className="material-symbols-outlined text-sm">
                              {s.status === "active" ? "stop_circle" : "play_circle"}
                            </span>
                            {s.status === "active" ? "STOP" : "START"}
                          </button>
                        )}

                        {s.source === "api" && (
                          <>
                            <button
                              onClick={() => startEdit(s)}
                              className="p-2 text-on-surface-variant hover:text-primary hover:bg-surface-container-low transition-colors"
                              title="Edit server"
                            >
                              <span className="material-symbols-outlined text-sm">edit</span>
                            </button>
                            <button
                              onClick={() => handleDelete(s.id)}
                              className="p-2 text-on-surface-variant hover:text-error hover:bg-error/10 transition-colors"
                              title="Delete server"
                            >
                              <span className="material-symbols-outlined text-sm">delete</span>
                            </button>
                          </>
                        )}

                        {s.source === "yaml" && (
                          <span className="font-mono text-[10px] text-on-surface-variant px-3 py-1 bg-surface-container-highest">
                            DEFINED_IN_YAML
                          </span>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
