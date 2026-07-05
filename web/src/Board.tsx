import { useCallback, useEffect, useRef, useState } from "react";
import type { LoopView, FireView, GateSummary, SSEEvent } from "./types";
import { api } from "./api";
import { EventStore } from "./events";

export interface BoardState {
  loops: LoopView[];
  fires: Map<string, FireView[]>;
  gates: GateSummary[];
  connected: boolean;
}

const COLUMNS = [
  { key: "FIRE_SCHEDULED", label: "Scheduled" },
  { key: "FIRE_DRAFTING", label: "Drafting" },
  { key: "FIRE_CONFIRMED", label: "Confirmed" },
  { key: "FIRE_RUNNING", label: "Running" },
  { key: "FIRE_GATED", label: "Human Gate" },
  { key: "FIRE_DONE", label: "Done" },
  { key: "FIRE_REJECTED", label: "Rejected" },
  { key: "FIRE_DEFERRED", label: "Deferred" },
  { key: "FIRE_FAILED", label: "Failed" },
] as const;

const TERMINAL = new Set(["FIRE_DONE", "FIRE_REJECTED", "FIRE_DEFERRED", "FIRE_FAILED"]);

export function useBoard() {
  const [state, setState] = useState<BoardState>({ loops: [], fires: new Map(), gates: [], connected: false });
  const storeRef = useRef<EventStore | null>(null);

  const load = useCallback(async () => {
    const [loops, gates] = await Promise.all([api.listLoops(), api.listOpenGates()]);
    const fires = new Map<string, FireView[]>();
    await Promise.all(
      loops.map(async (l) => {
        try {
          fires.set(l.id, await api.listFires(l.id));
        } catch {
          fires.set(l.id, []);
        }
      }),
    );
    setState((current) => ({ loops, fires, gates, connected: current.connected }));
  }, []);

  useEffect(() => {
    load();
    const store = new EventStore((connected) => {
      setState((current) => ({ ...current, connected }));
    });
    storeRef.current = store;
    store.onEvent((_e: SSEEvent) => {
      // ponytail: re-fetch on any event — simple, correct for phase 1
      load();
    });
    store.connect();
    return () => store.disconnect();
  }, [load]);

  const openGates = state.gates;

  return { state, openGates, reload: load };
}

export function Board({ state, onGateClick }: { state: BoardState; onGateClick: (gate: GateSummary) => void }) {
  const gateByFire = new Map(state.gates.map((g) => [g.fire_id, g]));

  return (
    <div style={{ display: "flex", gap: 12, overflowX: "auto", padding: "0 16px 16px", minHeight: "60vh" }}>
      {COLUMNS.map((col) => {
        const fires: { fire: FireView; loop: LoopView }[] = [];
        for (const loop of state.loops) {
          const loopFires = state.fires.get(loop.id) ?? [];
          for (const f of loopFires) {
            if (col.key === "FIRE_GATED") {
              if (gateByFire.has(f.id)) fires.push({ fire: f, loop });
            } else if (f.status === col.key) {
              fires.push({ fire: f, loop });
            }
          }
        }
        const isTerminal = TERMINAL.has(col.key);
        return (
          <ColumnCard
            key={col.key}
            label={col.label}
            fires={fires}
            isGated={col.key === "FIRE_GATED"}
            isTerminal={isTerminal}
            gateByFire={gateByFire}
            onGateClick={onGateClick}
          />
        );
      })}
    </div>
  );
}

function ColumnCard({
  label,
  fires,
  isGated,
  isTerminal,
  gateByFire,
  onGateClick,
}: {
  label: string;
  fires: { fire: FireView; loop: LoopView }[];
  isGated: boolean;
  isTerminal: boolean;
  gateByFire: Map<string, GateSummary>;
  onGateClick: (gate: GateSummary) => void;
}) {
  return (
    <div
      style={{
        flex: "0 0 240px",
        background: isGated ? "#fff8e1" : isTerminal ? "#f5f5f5" : "#e3f2fd",
        borderRadius: 8,
        padding: 8,
      }}
    >
      <h3 style={{ margin: "0 0 8px", fontSize: 14, fontWeight: 600 }}>
        {label}
        <span style={{ marginLeft: 6, color: "#666", fontWeight: 400 }}>({fires.length})</span>
      </h3>
      <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        {fires.map(({ fire, loop }) => (
          <div
            key={fire.id}
            style={{
              background: "#fff",
              borderRadius: 6,
              padding: "6px 8px",
              fontSize: 12,
              border: isGated ? "2px solid #f57c00" : "1px solid #ddd",
              cursor: isGated ? "pointer" : "default",
            }}
            onClick={() => {
              const g = gateByFire.get(fire.id);
              if (g) onGateClick(g);
            }}
          >
            <div style={{ fontWeight: 600 }}>{fire.id}</div>
            <div style={{ color: "#666" }}>{loop.id}</div>
            {fire.scheduled_at && <div style={{ color: "#999", fontSize: 11 }}>{new Date(fire.scheduled_at).toLocaleString()}</div>}
            {fire.last_error && <div style={{ color: "#d32f2f", fontSize: 11, marginTop: 2 }}>{fire.last_error}</div>}
          </div>
        ))}
        {fires.length === 0 && (
          <div style={{ color: "#999", fontSize: 12, textAlign: "center", padding: 12 }}>—</div>
        )}
      </div>
    </div>
  );
}
