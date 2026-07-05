import { useState } from "react";
import { useBoard, Board } from "./Board";
import { GateModal } from "./GateModal";
import type { GateSummary } from "./types";

export function App() {
  const { state, openGates, reload } = useBoard();
  const [selectedGate, setSelectedGate] = useState<GateSummary | null>(null);

  return (
    <div style={{ fontFamily: "system-ui, sans-serif", color: "#333" }}>
      <header
        style={{
          display: "flex", alignItems: "center", gap: 12,
          padding: "8px 16px", borderBottom: "1px solid #ddd",
          background: "#fafafa",
        }}
      >
        <h1 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>pi-ramid</h1>
        <span
          style={{
            display: "inline-block", width: 8, height: 8, borderRadius: "50%",
            background: state.connected ? "#4caf50" : "#9e9e9e",
          }}
          title={state.connected ? "Connected" : "Disconnected"}
        />
        <span style={{ fontSize: 12, color: "#666" }}>{state.connected ? "SSE live" : "SSE reconnecting"}</span>
        <div style={{ flex: 1 }} />
        <button
          onClick={() => {
            const gated = state.gates;
            if (gated.length > 0) setSelectedGate(gated[0]);
          }}
          style={{
            position: "relative", padding: "4px 12px", borderRadius: 12,
            border: "1px solid #f57c00", background: "#fff8e1",
            cursor: "pointer", fontSize: 13, fontWeight: 600, color: "#e65100",
          }}
        >
          Gates
          {openGates.length > 0 && (
            <span
              style={{
                position: "absolute", top: -6, right: -6,
                background: "#d32f2f", color: "#fff", borderRadius: "50%",
                width: 18, height: 18, fontSize: 11, fontWeight: 700,
                display: "flex", alignItems: "center", justifyContent: "center",
              }}
            >
              {openGates.length}
            </span>
          )}
        </button>
      </header>

      <Board state={state} onGateClick={(g) => setSelectedGate(g)} />

      {selectedGate && (
        <GateModal
          gate={selectedGate}
          onClose={() => setSelectedGate(null)}
          onResolved={() => {
            setSelectedGate(null);
            reload();
          }}
        />
      )}
    </div>
  );
}
