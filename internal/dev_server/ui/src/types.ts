export interface Environment {
  key: string;
  name: string;
}

export interface SummaryEventPayload {
  kind: 'summary';
  features: object;
  [key: string]: unknown;
}

export interface FeatureEventPayload {
  kind: 'feature';
  creationDate: number;
  key: string;
  version: number;
  context: {
    kind: string;
    key: string;
    anonymous: boolean;
    _meta?: {
      redactedAttributes?: string[];
    };
  };
  variation: number;
  value: boolean | string | number | object;
  default: boolean | string | number | object;
  reason: {
    kind: string;
  };
}

export interface IndexEventPayload {
  kind: 'index';
  user?: object;
  [key: string]: unknown;
}

export interface GenericEventPayload {
  kind: string;
  [key: string]: unknown;
}

export interface EventData {
  id: string;
  timestamp: number;
  data: SummaryEventPayload | FeatureEventPayload | IndexEventPayload | GenericEventPayload;
}

export interface DebugSession {
  key: string;
  written_at: string;
  event_count: number;
}

export interface DebugSessionsPage {
  sessions: DebugSession[];
  total_count: number;
  has_more: boolean;
}

// API Event type that matches the server response
export interface ApiEvent {
  id: number;
  written_at: string;
  kind: string;
  data: unknown; // Raw JSON data from the API
}

// API EventsPage type that matches the server response
export interface ApiEventsPage {
  events: ApiEvent[];
  total_count: number;
  has_more: boolean;
}

// Utility function to convert API event to UI EventData
export function convertApiEventToEventData(apiEvent: ApiEvent): EventData {
  return {
    id: apiEvent.id.toString(),
    timestamp: new Date(apiEvent.written_at).getTime(),
    data: apiEvent.data
  };
}
