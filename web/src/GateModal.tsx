import { useEffect, useState } from "react";
import type { GateDetail, GateSummary } from "./types";
import { api } from "./api";

interface GateModalProps {
  gate: GateSummary;
  onClose: () => void;
  onResolved: () => void;
}

export function GateModal({ gate, onClose, onResolved }: GateModalProps) {
  const [detail, setDetail] = useState<GateDetail | null>(null);
  const [decision, setDecision] = useState("");
  const [note, setNote] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (gate.id) {
      api.getGate(gate.id).then(setDetail).catch(() => setDetail(null));
    }
  }, [gate.id]);

  const requiresNote = decision === "route" || decision === "reject";

  const handleSubmit = async () => {
    if (!decision) return;
    if (requiresNote && !note.trim()) return;
    setSending(true);
    setError("");
    try {
      await api.resolveGate(gate.id, { decision, note: note.trim() || undefined });
      onResolved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSending(false);
    }
  };

  const d = detail;
  const options = d?.decision_options ?? ["approve", "route", "defer", "reject"];

  return (
    <div
      style={{
        position: "fixed", inset: 0, zIndex: 1000,
        display: "flex", alignItems: "center", justifyContent: "center",
        background: "rgba(0,0,0,0.4)",
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: "#fff", borderRadius: 12, padding: 24, maxWidth: 520, width: "90%",
          maxHeight: "80vh", overflowY: "auto",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 style={{ margin: "0 0 4px", fontSize: 18 }}>Gate: {d?.gate ?? gate.gate}</h2>
        <div style={{ fontSize: 13, color: "#666", marginBottom: 12 }}>
          {d?.phase ?? gate.phase} · {d?.loop_id ?? gate.loop_id} · {d?.fire_id ?? gate.fire_id}
        </div>

        <div style={{ fontSize: 14, marginBottom: 12, padding: 8, background: "#f5f5f5", borderRadius: 6 }}>
          {d?.summary ?? gate.summary}
        </div>

        {d?.body && (
          <pre style={{ fontSize: 12, background: "#fafafa", padding: 8, borderRadius: 6, maxHeight: 120, overflow: "auto", marginBottom: 12 }}>
            {d.body}
          </pre>
        )}

        {d?.threads && d.threads.length > 0 && (
          <div style={{ marginBottom: 12 }}>
            <h4 style={{ margin: "0 0 6px", fontSize: 13 }}>Threads</h4>
            {d.threads.map((t) => (
              <div key={t.id} style={{ fontSize: 12, padding: "4px 0", borderBottom: "1px solid #eee" }}>
                <strong>{t.title}</strong>
                {t.location && <span style={{ color: "#999" }}> — {t.location}</span>}
                <div style={{ color: "#666" }}>{t.summary}</div>
              </div>
            ))}
          </div>
        )}

        <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 12 }}>
          {options.map((opt) => (
            <button
              key={opt}
              onClick={() => setDecision(opt)}
              style={{
                padding: "6px 14px", borderRadius: 6, border: "1px solid #ccc",
                background: decision === opt ? "#1976d2" : "#fff",
                color: decision === opt ? "#fff" : "#333",
                cursor: "pointer", fontSize: 13, fontWeight: 500,
              }}
            >
              {opt}
            </button>
          ))}
        </div>

        {decision && (
          <textarea
            placeholder={requiresNote ? "Note (required for route/reject)" : "Note (optional)"}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            rows={3}
            style={{ width: "100%", boxSizing: "border-box", padding: 8, fontSize: 13, borderRadius: 6, border: "1px solid #ccc", marginBottom: 12, resize: "vertical" }}
          />
        )}

        {error && <div style={{ color: "#d32f2f", fontSize: 13, marginBottom: 8 }}>{error}</div>}

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <button onClick={onClose} style={{ padding: "6px 16px", borderRadius: 6, border: "1px solid #ccc", background: "#fff", cursor: "pointer", fontSize: 13 }}>
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={!decision || (requiresNote && !note.trim()) || sending}
            style={{
              padding: "6px 16px", borderRadius: 6, border: "none",
              background: !decision || (requiresNote && !note.trim()) ? "#ccc" : "#1976d2",
              color: "#fff", cursor: !decision || (requiresNote && !note.trim()) ? "not-allowed" : "pointer",
              fontSize: 13,
            }}
          >
            {sending ? "Sending..." : "Submit"}
          </button>
        </div>
      </div>
    </div>
  );
}
