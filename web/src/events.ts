import type { SSEEvent } from "./types";

type Listener = (event: SSEEvent) => void;

type ConnectionListener = (connected: boolean) => void;

export class EventStore {
  private events: SSEEvent[] = [];
  private listeners = new Set<Listener>();
  private lastID = 0;
  private controller: AbortController | null = null;
  private _connected = false;
  private onConnection?: ConnectionListener;

  get connected() {
    return this._connected;
  }

  get all() {
    return this.events;
  }

  constructor(onConnection?: ConnectionListener) {
    this.onConnection = onConnection;
  }

  onEvent(fn: Listener) {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  connect() {
    if (this.controller) return;
    this.controller = new AbortController();
    this._connect();
  }

  disconnect() {
    this.controller?.abort();
    this.controller = null;
    this.setConnected(false);
  }

  private async _connect() {
    const signal = this.controller!.signal;
    try {
      const res = await fetch("/v1/events", {
        headers: this.lastID > 0 ? { "Last-Event-ID": String(this.lastID) } : {},
        signal,
      });
      if (!res.ok || !res.body) {
        this.setConnected(false);
        this._retry();
        return;
      }
      this.setConnected(true);
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split("\n");
        buf = lines.pop() ?? "";

        let id = 0;
        for (const line of lines) {
          if (line.startsWith("id: ")) {
            id = parseInt(line.slice(4), 10);
          } else if (line.startsWith("data: ")) {
            try {
              const event: SSEEvent = JSON.parse(line.slice(6));
              event.id = id;
              this.events.push(event);
              this.lastID = id;
              for (const fn of this.listeners) fn(event);
            } catch {
              // skip malformed events
            }
          }
        }
      }
    } catch (err) {
      if ((err as Error).name === "AbortError") return;
    }
    this.setConnected(false);
    this._retry();
  }

  private setConnected(connected: boolean) {
    this._connected = connected;
    this.onConnection?.(connected);
  }

  private _retry() {
    if (!this.controller) return;
    setTimeout(() => this._connect(), 1000);
  }
}
