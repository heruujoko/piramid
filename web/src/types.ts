export interface LoopView {
  id: string;
  pattern_id: string;
  active: boolean;
  cron: string;
  autonomy: string;
  latest_fire?: FireSummary;
}

export interface FireSummary {
  id: string;
  loop_id: string;
  status: string;
  scheduled_at: string;
}

export interface FireView {
  id: string;
  loop_id: string;
  goal_id?: string;
  status: string;
  scheduled_at: string;
  started_at?: string;
  last_error?: string;
}

export interface GateSummary {
  id: string;
  gate: string;
  phase: string;
  fire_id: string;
  loop_id: string;
  summary: string;
  opened_at: string;
}

export interface GateThreadView {
  id: string;
  title: string;
  location?: string;
  author?: string;
  summary: string;
}

export interface GateDetail {
  id: string;
  gate: string;
  phase: string;
  fire_id: string;
  loop_id: string;
  goal_id?: string;
  task_id?: string;
  attempt_id?: string;
  summary: string;
  opened_at: string;
  decision_options: string[];
  threads?: GateThreadView[];
  body?: string;
}

export interface GateDecisionInput {
  decision: string;
  note?: string;
}

export interface SSEEvent {
  id: number;
  entity_type: string;
  entity_id: string;
  event_type: string;
  payload_json: string;
  created_at: string;
}
